package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/MaimoryLab/codex-server/internal/appserver"
	"github.com/MaimoryLab/codex-server/internal/devices"
	"github.com/coder/websocket"
)

type websocketRequest struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type websocketResponse struct {
	ID     int64           `json:"id"`
	Result any             `json:"result,omitempty"`
	Error  *websocketError `json:"error,omitempty"`
}

type websocketError struct {
	Message    string `json:"message"`
	StatusCode int    `json:"statusCode,omitempty"`
}

func authenticateDevice(store *devices.Store, header string) (devices.Device, bool) {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return devices.Device{}, false
	}
	device, ok := store.Authenticate(token)
	return device, ok
}

func websocketHandler(server *Server, store *devices.Store, appServer AppServer, events *eventHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		device, ok := authenticateDevice(store, r.Header.Get("Authorization"))
		if !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		disconnect := store.Connect(device.ID)
		defer disconnect()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		session := &websocketSession{conn: conn, ctx: ctx, server: server, store: store, appServer: appServer}
		var requests sync.WaitGroup
		if events != nil {
			eventStream, unsubscribe := events.subscribe()
			defer unsubscribe()
			go session.writeEvents(eventStream)
		}

		for {
			messageType, data, err := conn.Read(ctx)
			if err != nil {
				break
			}
			if messageType != websocket.MessageText {
				continue
			}
			var request websocketRequest
			if err := json.Unmarshal(data, &request); err != nil || request.ID == 0 {
				_ = session.write(websocketResponse{ID: request.ID, Error: &websocketError{Message: "invalid websocket request", StatusCode: http.StatusBadRequest}})
				continue
			}
			requests.Go(func() { session.handle(request) })
		}
		cancel()
		requests.Wait()
	}
}

type websocketSession struct {
	conn      *websocket.Conn
	ctx       context.Context
	server    *Server
	store     *devices.Store
	appServer AppServer
}

func (s *websocketSession) writeEvents(events <-chan appserver.Event) {
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			value := map[string]any{"method": event.Method, "params": json.RawMessage(event.Params)}
			if event.ID != nil {
				value["id"] = *event.ID
			}
			data, err := json.Marshal(value)
			if err != nil || s.conn.Write(s.ctx, websocket.MessageText, data) != nil {
				return
			}
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *websocketSession) handle(request websocketRequest) {
	result, status, err := s.dispatch(request)
	if err != nil {
		_ = s.write(websocketResponse{ID: request.ID, Error: &websocketError{Message: err.Error(), StatusCode: status}})
		return
	}
	_ = s.write(websocketResponse{ID: request.ID, Result: result})
}

func (s *websocketSession) dispatch(request websocketRequest) (any, int, error) {
	params := map[string]any{}
	if len(request.Params) > 0 && string(request.Params) != "null" {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, http.StatusBadRequest, errors.New("invalid request params")
		}
	}
	switch request.Method {
	case "heartbeat":
		return map[string]bool{"ack": true}, http.StatusOK, nil
	case "status":
		value, err := json.Marshal(s.server.status(s.ctx))
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}
		result := map[string]any{}
		if err := json.Unmarshal(value, &result); err != nil {
			return nil, http.StatusInternalServerError, err
		}
		result["server"] = s.serverInfo()
		return result, http.StatusOK, nil
	case "directories":
		path, _ := params["path"].(string)
		result, err := listDirectoriesValue(path)
		return result, statusFor(err, http.StatusBadRequest), err
	case "upload":
		name, _ := params["name"].(string)
		encoded, _ := params["data"].(string)
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, http.StatusBadRequest, errors.New("invalid upload data")
		}
		result, err := s.server.saveUpload(name, data)
		return result, statusFor(err, http.StatusBadRequest), err
	case "thread/list":
		return s.call("thread/list", params)
	case "thread/read":
		params["includeTurns"] = true
		return s.call("thread/read", params)
	case "thread/start":
		return s.call("thread/start", params)
	case "thread/resume":
		return s.resumeThread(params)
	case "thread/unsubscribe", "thread/archive":
		return s.call(request.Method, params)
	case "thread/release":
		threadID, err := requiredString(params, "threadId")
		if err != nil {
			return nil, http.StatusBadRequest, err
		}
		if s.appServer == nil {
			return nil, http.StatusServiceUnavailable, errors.New("app-server is not running")
		}
		released, err := s.appServer.ReleaseThread(s.ctx, threadID)
		return map[string]bool{"released": released}, statusFor(err, http.StatusBadGateway), err
	case "thread/takeover":
		threadID, err := requiredString(params, "threadId")
		if err != nil {
			return nil, http.StatusBadRequest, err
		}
		return s.takeOverThread(threadID)
	case "thread/name/set":
		name, err := requiredString(params, "name")
		if err != nil {
			return nil, http.StatusBadRequest, err
		}
		params["name"] = strings.TrimSpace(name)
		if params["name"] == "" {
			return nil, http.StatusBadRequest, errors.New("name is required")
		}
		return s.call("thread/name/set", params)
	case "turn/start":
		return s.turn(params, "turn/start")
	case "turn/steer":
		return s.turn(params, "turn/steer")
	case "turn/interrupt":
		return s.call("turn/interrupt", params)
	case "approval/respond":
		return s.approve(params)
	default:
		return nil, http.StatusBadRequest, errors.New("unknown websocket method")
	}
}

func (s *websocketSession) call(method string, params map[string]any) (any, int, error) {
	if s.appServer == nil {
		return nil, http.StatusServiceUnavailable, errors.New("app-server is not running")
	}
	result, err := s.appServer.Call(s.ctx, method, params)
	if err != nil {
		return nil, appServerStatus(err), err
	}
	var value any
	if len(result) > 0 {
		if err := json.Unmarshal(result, &value); err != nil {
			return nil, http.StatusBadGateway, err
		}
	}
	return value, http.StatusOK, nil
}

func (s *websocketSession) resumeThread(params map[string]any) (any, int, error) {
	threadID, err := requiredString(params, "threadId")
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	if s.appServer == nil {
		return nil, http.StatusServiceUnavailable, errors.New("app-server is not running")
	}
	result, err := s.appServer.ResumeThread(s.ctx, threadID)
	if err != nil {
		return nil, appServerStatus(err), err
	}
	var value any
	if len(result) > 0 {
		if err := json.Unmarshal(result, &value); err != nil {
			return nil, http.StatusBadGateway, err
		}
	}
	return value, http.StatusOK, nil
}

func (s *websocketSession) takeOverThread(threadID string) (any, int, error) {
	if s.appServer == nil {
		return nil, http.StatusServiceUnavailable, errors.New("app-server is not running")
	}
	result, err := s.appServer.TakeOverThread(s.ctx, threadID)
	if err != nil {
		return nil, appServerStatus(err), err
	}
	var value any
	if err := json.Unmarshal(result, &value); err != nil {
		return nil, http.StatusBadGateway, err
	}
	return value, http.StatusOK, nil
}

func (s *websocketSession) turn(params map[string]any, method string) (any, int, error) {
	threadID, err := requiredString(params, "threadId")
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	var request turnRequest
	data, _ := json.Marshal(params)
	if err := json.Unmarshal(data, &request); err != nil {
		return nil, http.StatusBadRequest, err
	}
	input, err := s.server.userInput(request)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	callParams := map[string]any{"threadId": threadID, "input": input}
	if method == "turn/steer" {
		turnID, err := requiredString(params, "turnId")
		if err != nil {
			return nil, http.StatusBadRequest, err
		}
		callParams["expectedTurnId"] = turnID
	}
	if err := request.addPermissions(callParams); err != nil {
		return nil, http.StatusBadRequest, err
	}
	return s.call(method, callParams)
}

func (s *websocketSession) approve(params map[string]any) (any, int, error) {
	requestID, ok := params["requestId"].(float64)
	if !ok || requestID <= 0 || requestID != float64(int64(requestID)) {
		return nil, http.StatusBadRequest, errors.New("invalid approval request id")
	}
	decision, ok := params["decision"]
	if !ok {
		return nil, http.StatusBadRequest, errors.New("invalid approval decision")
	}
	raw, err := json.Marshal(decision)
	if err != nil || !validApprovalDecision(raw) {
		return nil, http.StatusBadRequest, errors.New("invalid approval decision")
	}
	if s.appServer == nil {
		return nil, http.StatusServiceUnavailable, errors.New("app-server is not running")
	}
	if err := s.appServer.Respond(int64(requestID), map[string]json.RawMessage{"decision": raw}, nil); err != nil {
		return nil, appServerStatus(err), err
	}
	return nil, http.StatusNoContent, nil
}

func (s *websocketSession) serverInfo() devices.Server { return s.store.Server() }

func (s *websocketSession) write(value websocketResponse) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.conn.Write(s.ctx, websocket.MessageText, data)
}

func requiredString(params map[string]any, key string) (string, error) {
	value, ok := params[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", errors.New(key + " is required")
	}
	return value, nil
}

func statusFor(err error, fallback int) int {
	if err == nil {
		return http.StatusOK
	}
	return fallback
}

func appServerStatus(err error) int {
	if rpcError, ok := errors.AsType[*appserver.RPCError](err); ok {
		if rpcError.Code == -32001 {
			return http.StatusTooManyRequests
		}
		return http.StatusConflict
	}
	return http.StatusBadGateway
}
