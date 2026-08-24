package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/MaimoryLab/codex-server/internal/appserver"
	"github.com/MaimoryLab/codex-server/internal/devices"
)

type Server struct {
	httpServer *http.Server
	listener   net.Listener
}

type AppServer interface {
	Call(context.Context, string, any) (json.RawMessage, error)
	Respond(int64, any, *appserver.RPCError) error
	Events() <-chan appserver.Event
}

func New[T any](status func(context.Context) T, deviceStore *devices.Store, appServers ...AppServer) *Server {
	var appServer AppServer
	if len(appServers) > 0 {
		appServer = appServers[0]
	}
	var events *eventHub
	if appServer != nil {
		events = newEventHub(appServer.Events())
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("POST /api/v1/pair/exchange", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Token string `json:"token"`
			Name  string `json:"name"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid pairing request", http.StatusBadRequest)
			return
		}
		device, token, err := deviceStore.Exchange(request.Token, request.Name)
		if err != nil {
			http.Error(w, "pairing denied", http.StatusUnauthorized)
			return
		}
		writeJSON(w, struct {
			Device devices.Device `json:"device"`
			Token  string         `json:"token"`
		}{device, token})
	})
	mux.Handle("GET /api/v1/status", authenticate(deviceStore, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(status(r.Context())); err != nil {
			http.Error(w, "encode status: "+err.Error(), http.StatusInternalServerError)
		}
	})))
	mux.Handle("GET /api/v1/threads", authenticate(deviceStore, callAppServer(appServer, "thread/list", func(r *http.Request) any {
		params := map[string]any{}
		query := r.URL.Query()
		if cursor := query.Get("cursor"); cursor != "" {
			params["cursor"] = cursor
		}
		if limit, err := strconv.Atoi(query.Get("limit")); err == nil && limit > 0 {
			params["limit"] = limit
		}
		return params
	})))
	mux.Handle("GET /api/v1/threads/{threadID}", authenticate(deviceStore, callAppServer(appServer, "thread/read", func(r *http.Request) any {
		return map[string]any{"threadId": r.PathValue("threadID"), "includeTurns": true}
	})))
	mux.Handle("POST /api/v1/threads", authenticate(deviceStore, callAppServer(appServer, "thread/start", func(r *http.Request) any {
		var request struct {
			CWD string `json:"cwd"`
		}
		if err := decodeBody(r, &request); err != nil {
			return requestError{err}
		}
		params := map[string]any{}
		if request.CWD != "" {
			params["cwd"] = request.CWD
		}
		return params
	})))
	mux.Handle("POST /api/v1/threads/{threadID}/resume", authenticate(deviceStore, callAppServer(appServer, "thread/resume", func(r *http.Request) any {
		return map[string]string{"threadId": r.PathValue("threadID")}
	})))
	mux.Handle("POST /api/v1/threads/{threadID}/unsubscribe", authenticate(deviceStore, callAppServer(appServer, "thread/unsubscribe", func(r *http.Request) any {
		return map[string]string{"threadId": r.PathValue("threadID")}
	})))
	mux.Handle("POST /api/v1/threads/{threadID}/turns", authenticate(deviceStore, callAppServer(appServer, "turn/start", func(r *http.Request) any {
		var request struct {
			Input string `json:"input"`
		}
		if err := decodeBody(r, &request); err != nil {
			return requestError{err}
		}
		if strings.TrimSpace(request.Input) == "" {
			return requestError{errors.New("input is required")}
		}
		return map[string]any{
			"threadId": r.PathValue("threadID"),
			"input":    []map[string]string{{"type": "text", "text": request.Input}},
		}
	})))
	mux.Handle("POST /api/v1/turns/{turnID}/steer", authenticate(deviceStore, callAppServer(appServer, "turn/steer", func(r *http.Request) any {
		var request struct {
			Input string `json:"input"`
		}
		if err := decodeBody(r, &request); err != nil {
			return requestError{err}
		}
		threadID := r.URL.Query().Get("threadId")
		if threadID == "" || strings.TrimSpace(request.Input) == "" {
			return requestError{errors.New("threadId and input are required")}
		}
		return map[string]any{
			"threadId":       threadID,
			"expectedTurnId": r.PathValue("turnID"),
			"input":          []map[string]string{{"type": "text", "text": request.Input}},
		}
	})))
	mux.Handle("POST /api/v1/turns/{turnID}/interrupt", authenticate(deviceStore, callAppServer(appServer, "turn/interrupt", func(r *http.Request) any {
		return map[string]string{"threadId": r.URL.Query().Get("threadId"), "turnId": r.PathValue("turnID")}
	})))
	mux.Handle("POST /api/v1/approvals/{requestID}", authenticate(deviceStore, respondApproval(appServer)))
	mux.Handle("GET /api/v1/events", authenticate(deviceStore, eventsStream(appServer, events)))
	return &Server{httpServer: &http.Server{Handler: mux}}
}

type requestError struct{ err error }

func decodeBody(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func callAppServer(appServer AppServer, method string, params func(*http.Request) any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if appServer == nil {
			http.Error(w, "app-server is not running", http.StatusServiceUnavailable)
			return
		}
		value := params(r)
		if request, ok := value.(requestError); ok {
			http.Error(w, request.err.Error(), http.StatusBadRequest)
			return
		}
		result, err := appServer.Call(r.Context(), method, value)
		if err != nil {
			writeAppServerError(w, err)
			return
		}
		writeRawJSON(w, result)
	})
}

func eventsStream(appServer AppServer, hub *eventHub) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if appServer == nil || hub == nil {
			http.Error(w, "app-server is not running", http.StatusServiceUnavailable)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		events, unsubscribe := hub.subscribe()
		defer unsubscribe()
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return
				}
				payloadValue := map[string]any{"method": event.Method, "params": json.RawMessage(event.Params)}
				if event.ID != nil {
					payloadValue["id"] = *event.ID
				}
				payload, _ := json.Marshal(payloadValue)
				_, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
}

func respondApproval(appServer AppServer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if appServer == nil {
			http.Error(w, "app-server is not running", http.StatusServiceUnavailable)
			return
		}
		requestID, err := strconv.ParseInt(r.PathValue("requestID"), 10, 64)
		if err != nil || requestID < 1 {
			http.Error(w, "invalid approval request id", http.StatusBadRequest)
			return
		}
		var request struct {
			Decision json.RawMessage `json:"decision"`
		}
		if err := decodeBody(r, &request); err != nil || !validApprovalDecision(request.Decision) {
			http.Error(w, "invalid approval decision", http.StatusBadRequest)
			return
		}
		if err := appServer.Respond(requestID, map[string]json.RawMessage{"decision": request.Decision}, nil); err != nil {
			writeAppServerError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func validApprovalDecision(raw json.RawMessage) bool {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || bytesEqual(raw, []byte("null")) {
		return false
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		switch value {
		case "accept", "acceptForSession", "decline", "cancel", "approved", "approved_for_session", "approved_mcp_policy_amendment", "timed_out", "abort":
			return true
		default:
			return false
		}
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) != 1 {
		return false
	}
	for key := range object {
		switch key {
		case "acceptWithExecpolicyAmendment", "applyNetworkPolicyAmendment", "approved_execpolicy_amendment", "network_policy_amendment", "denied", "permissions":
			return true
		}
	}
	return false
}

func bytesEqual(left, right []byte) bool { return string(left) == string(right) }

func writeRawJSON(w http.ResponseWriter, data json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	if len(data) == 0 {
		data = []byte("{}")
	}
	_, _ = w.Write(data)
}

func writeAppServerError(w http.ResponseWriter, err error) {
	if rpcError, ok := errors.AsType[*appserver.RPCError](err); ok {
		status := http.StatusConflict
		if rpcError.Code == -32001 {
			status = http.StatusTooManyRequests
		}
		http.Error(w, rpcError.Error(), status)
		return
	}
	http.Error(w, err.Error(), http.StatusBadGateway)
}

func authenticate(store *devices.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
		if !found || !strings.EqualFold(scheme, "Bearer") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if _, ok := store.Authenticate(token); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "encode response: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) Start() error {
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		return err
	}
	s.listener = listener
	go func() {
		_ = s.httpServer.Serve(listener)
	}()
	return nil
}

func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	_, port, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		return ""
	}
	return "http://127.0.0.1:" + port
}

func (s *Server) ListenAddr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *Server) LANAddr() string {
	if s.listener == nil {
		return ""
	}
	_, port, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		return ""
	}
	host := "127.0.0.1"
	if addresses, err := net.InterfaceAddrs(); err == nil {
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.To4() != nil && !ip.IsLoopback() {
				host = ip.String()
				break
			}
		}
	}
	return "http://" + net.JoinHostPort(host, port)
}

func (s *Server) Close(ctx context.Context) error {
	if s.listener == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}
