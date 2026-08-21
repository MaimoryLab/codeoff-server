package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
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
	Events() <-chan appserver.Event
}

func New[T any](status func(context.Context) T, deviceStore *devices.Store, appServers ...AppServer) *Server {
	var appServer AppServer
	if len(appServers) > 0 {
		appServer = appServers[0]
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
	mux.Handle("GET /api/v1/threads", authenticate(deviceStore, callAppServer(appServer, "thread/list", func(*http.Request) any { return map[string]any{} })))
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
	mux.Handle("POST /api/v1/turns/{turnID}/interrupt", authenticate(deviceStore, callAppServer(appServer, "turn/interrupt", func(r *http.Request) any {
		return map[string]string{"threadId": r.URL.Query().Get("threadId"), "turnId": r.PathValue("turnID")}
	})))
	mux.Handle("GET /api/v1/events", authenticate(deviceStore, eventsStream(appServer)))
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

func eventsStream(appServer AppServer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if appServer == nil {
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
		for {
			select {
			case event, ok := <-appServer.Events():
				if !ok {
					return
				}
				payload, _ := json.Marshal(map[string]any{"method": event.Method, "params": json.RawMessage(event.Params)})
				_, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
}

func writeRawJSON(w http.ResponseWriter, data json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	if len(data) == 0 {
		data = []byte("{}")
	}
	_, _ = w.Write(data)
}

func writeAppServerError(w http.ResponseWriter, err error) {
	var rpcError *appserver.RPCError
	if errors.As(err, &rpcError) && rpcError.Code == -32001 {
		http.Error(w, rpcError.Error(), http.StatusTooManyRequests)
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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
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
	return "http://" + s.listener.Addr().String()
}

func (s *Server) Close(ctx context.Context) error {
	if s.listener == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}
