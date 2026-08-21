package control

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/MaimoryLab/codex-server/internal/devices"
)

type Server struct {
	httpServer *http.Server
	listener   net.Listener
}

func New[T any](status func(context.Context) T, deviceStore *devices.Store) *Server {
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
	return &Server{httpServer: &http.Server{Handler: mux}}
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
