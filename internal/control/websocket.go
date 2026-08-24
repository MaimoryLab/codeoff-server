package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"

	"github.com/MaimoryLab/codex-server/internal/appserver"
	"github.com/MaimoryLab/codex-server/internal/devices"
	"github.com/coder/websocket"
)

type authenticatedDeviceKey struct{}

type websocketRequest struct {
	ID          int64             `json:"id"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Query       map[string]string `json:"query,omitempty"`
	Body        json.RawMessage   `json:"body,omitempty"`
	Binary      string            `json:"binary,omitempty"`
	ContentType string            `json:"contentType,omitempty"`
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

func websocketHandler(store *devices.Store, mux http.Handler, events *eventHub) http.HandlerFunc {
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
		session := &websocketSession{conn: conn, mux: mux, ctx: ctx}
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
			requests.Go(func() {
				session.handle(request, device)
			})
		}
		cancel()
		requests.Wait()
	}
}

type websocketSession struct {
	conn *websocket.Conn
	mux  http.Handler
	ctx  context.Context
}

func (s *websocketSession) writeEvents(events <-chan appserver.Event) {
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			value := map[string]any{
				"method": event.Method,
				"params": json.RawMessage(event.Params),
			}
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

func (s *websocketSession) handle(request websocketRequest, device devices.Device) {
	result, status, err := s.dispatch(request, device)
	if err != nil {
		_ = s.write(websocketResponse{ID: request.ID, Error: &websocketError{Message: err.Error(), StatusCode: status}})
		return
	}
	_ = s.write(websocketResponse{ID: request.ID, Result: result})
}

func (s *websocketSession) dispatch(request websocketRequest, device devices.Device) (any, int, error) {
	if request.Method == "" || request.Path == "" || !strings.HasPrefix(request.Path, "/api/v1/") {
		return nil, http.StatusBadRequest, errors.New("invalid websocket request")
	}
	var body []byte
	contentType := request.ContentType
	if request.Binary != "" {
		var err error
		body, err = base64.StdEncoding.DecodeString(request.Binary)
		if err != nil {
			return nil, http.StatusBadRequest, errors.New("invalid binary payload")
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
	} else if len(request.Body) > 0 && string(request.Body) != "null" {
		body = request.Body
		if contentType == "" {
			contentType = "application/json"
		}
	}
	query := url.Values{}
	for key, value := range request.Query {
		query.Set(key, value)
	}
	path := request.Path
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	httpRequest, err := http.NewRequestWithContext(s.ctx, request.Method, path, strings.NewReader(string(body)))
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	httpRequest.Header.Set("Authorization", "Bearer websocket-session")
	httpRequest.Header.Set("Content-Type", contentType)
	httpRequest = httpRequest.WithContext(context.WithValue(httpRequest.Context(), authenticatedDeviceKey{}, device))
	recorder := httptest.NewRecorder()
	s.mux.ServeHTTP(recorder, httpRequest)
	if recorder.Code < 200 || recorder.Code >= 300 {
		message := strings.TrimSpace(recorder.Body.String())
		if message == "" {
			message = http.StatusText(recorder.Code)
		}
		return nil, recorder.Code, errors.New(message)
	}
	if recorder.Code == http.StatusNoContent || recorder.Body.Len() == 0 {
		return nil, recorder.Code, nil
	}
	var result any
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		return nil, http.StatusBadGateway, err
	}
	return result, recorder.Code, nil
}

func (s *websocketSession) write(value websocketResponse) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.conn.Write(s.ctx, websocket.MessageText, data)
}
