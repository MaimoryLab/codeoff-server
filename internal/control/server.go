package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/MaimoryLab/codeoff-server/internal/appserver"
	"github.com/MaimoryLab/codeoff-server/internal/devices"
)

const maxUploadSize = 25 << 20

type Server struct {
	httpServer *http.Server
	listener   net.Listener
	uploadMu   sync.RWMutex
	uploads    map[string]bool
	status     func(context.Context) any
}

type turnRequest struct {
	Input             string         `json:"input"`
	ApprovalPolicy    string         `json:"approvalPolicy"`
	ApprovalsReviewer string         `json:"approvalsReviewer"`
	SandboxPolicy     *sandboxPolicy `json:"sandboxPolicy"`
	Attachments       []struct {
		Name string `json:"name"`
		Path string `json:"path"`
	} `json:"attachments"`
}

type sandboxPolicy struct {
	Type string `json:"type"`
}

type AppServer interface {
	Call(context.Context, string, any) (json.RawMessage, error)
	ResumeThread(context.Context, string) (json.RawMessage, error)
	ReleaseThread(context.Context, string) (bool, error)
	TakeOverThread(context.Context, string) (json.RawMessage, error)
	Respond(int64, any, *appserver.RPCError) error
	Events() <-chan appserver.Event
}

// AdminController is the local daemon control plane. Its endpoints are
// protected by the admin bearer token passed to NewWithOptions.
type AdminController interface {
	Status(context.Context) any
	NewPairing() (devices.Pairing, error)
	Devices() []devices.Device
	RevokeDevice(string) error
	RestartAppServer() error
	RestartTunnel() error
	Shutdown() error
}

type Options struct {
	AppServers []AppServer
	Admin      AdminController
	AdminToken string
}

func New[T any](status func(context.Context) T, deviceStore *devices.Store, appServers ...AppServer) *Server {
	return NewWithOptions(status, deviceStore, Options{AppServers: appServers})
}

func NewWithOptions[T any](status func(context.Context) T, deviceStore *devices.Store, options Options) *Server {
	server := &Server{
		uploads: make(map[string]bool),
		status:  func(ctx context.Context) any { return status(ctx) },
	}
	var appServer AppServer
	if len(options.AppServers) > 0 {
		appServer = options.AppServers[0]
	}
	var events *eventHub
	if appServer != nil {
		events = newEventHub(appServer.Events())
	}
	mux := http.NewServeMux()
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
		log.Printf("device paired: id=%s name=%q", device.ID, device.Name)
		writeJSON(w, struct {
			Device devices.Device `json:"device"`
			Server devices.Server `json:"server"`
			Token  string         `json:"token"`
		}{device, deviceStore.Server(), token})
	})
	mux.HandleFunc("POST /api/v1/upload", uploadHandler(server, deviceStore))
	mux.HandleFunc("GET /api/v1/ws", websocketHandler(server, deviceStore, appServer, events))
	if options.Admin != nil && options.AdminToken != "" {
		server.addAdminRoutes(mux, options.Admin, options.AdminToken)
	}
	server.httpServer = &http.Server{Handler: mux}
	return server
}

func (s *Server) addAdminRoutes(mux *http.ServeMux, admin AdminController, token string) {
	authorized := func(r *http.Request) bool {
		return r.Header.Get("Authorization") == "Bearer "+token
	}
	guard := func(w http.ResponseWriter, r *http.Request) bool {
		if authorized(r) {
			return true
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	mux.HandleFunc("GET /api/v1/admin/status", func(w http.ResponseWriter, r *http.Request) {
		if !guard(w, r) {
			return
		}
		writeJSON(w, admin.Status(r.Context()))
	})
	mux.HandleFunc("GET /api/v1/admin/devices", func(w http.ResponseWriter, r *http.Request) {
		if !guard(w, r) {
			return
		}
		writeJSON(w, admin.Devices())
	})
	mux.HandleFunc("POST /api/v1/admin/pair", func(w http.ResponseWriter, r *http.Request) {
		if !guard(w, r) {
			return
		}
		pairing, err := admin.NewPairing()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, pairing)
	})
	mux.HandleFunc("POST /api/v1/admin/revoke", func(w http.ResponseWriter, r *http.Request) {
		if !guard(w, r) {
			return
		}
		var request struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&request); err != nil || strings.TrimSpace(request.ID) == "" {
			http.Error(w, "device id is required", http.StatusBadRequest)
			return
		}
		if err := admin.RevokeDevice(request.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	for path, action := range map[string]func() error{
		"/api/v1/admin/restart/appserver": admin.RestartAppServer,
		"/api/v1/admin/restart/tunnel":    admin.RestartTunnel,
		"/api/v1/admin/shutdown":          admin.Shutdown,
	} {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			if !guard(w, r) {
				return
			}
			if path == "/api/v1/admin/shutdown" {
				go action()
				writeJSON(w, map[string]bool{"ok": true})
				return
			}
			if err := action(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]bool{"ok": true})
		})
	}
}

func uploadHandler(server *Server, store *devices.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authenticateDevice(store, r.Header.Get("Authorization")); !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body := http.MaxBytesReader(w, r.Body, maxUploadSize+1)
		result, err := server.saveUpload(r.URL.Query().Get("name"), body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, result)
	}
}

func (r turnRequest) addPermissions(params map[string]any) error {
	if r.ApprovalPolicy == "" && r.SandboxPolicy == nil {
		return nil
	}
	if r.ApprovalPolicy == "" || r.SandboxPolicy == nil {
		return errors.New("approvalPolicy and sandboxPolicy are required together")
	}
	if r.ApprovalPolicy != "on-request" && r.ApprovalPolicy != "never" {
		return errors.New("invalid approvalPolicy")
	}
	if r.SandboxPolicy.Type != "workspaceWrite" && r.SandboxPolicy.Type != "dangerFullAccess" {
		return errors.New("invalid sandboxPolicy")
	}
	if r.ApprovalsReviewer != "" && r.ApprovalsReviewer != "user" && r.ApprovalsReviewer != "auto_review" && r.ApprovalsReviewer != "guardian_subagent" {
		return errors.New("invalid approvalsReviewer")
	}
	params["approvalPolicy"] = r.ApprovalPolicy
	if r.ApprovalsReviewer != "" {
		params["approvalsReviewer"] = r.ApprovalsReviewer
	}
	params["sandboxPolicy"] = r.SandboxPolicy
	return nil
}

type directoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func listDirectoriesValue(rawPath string) (map[string]any, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		var err error
		path, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
	}
	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, errors.New("invalid directory path")
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil, fmt.Errorf("access directory: %w", err)
		}
		return nil, errors.New("directory not found")
	}
	if !info.IsDir() {
		return nil, errors.New("directory not found")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}
	directories := make([]directoryEntry, 0, len(entries))
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		childInfo, err := os.Stat(child)
		if err == nil && childInfo.IsDir() {
			directories = append(directories, directoryEntry{Name: entry.Name(), Path: child})
		}
	}
	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}
	return map[string]any{"path": path, "parent": parent, "directories": directories}, nil
}

func createDirectoryValue(rawPath, rawName string) (map[string]any, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return nil, errors.New("path is required")
	}
	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, errors.New("invalid directory path")
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil, fmt.Errorf("access directory: %w", err)
		}
		return nil, errors.New("directory not found")
	}
	if !info.IsDir() {
		return nil, errors.New("directory not found")
	}
	name := strings.TrimSpace(rawName)
	if name == "" {
		return nil, errors.New("name is required")
	}
	if name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return nil, errors.New("invalid directory name")
	}
	created := filepath.Join(path, name)
	if err := os.Mkdir(created, 0o755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}
	return map[string]any{"name": name, "path": created}, nil
}

func (s *Server) saveUpload(name string, data io.Reader) (map[string]any, error) {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" {
		return nil, errors.New("name is required")
	}
	extension := filepath.Ext(name)
	if len(extension) > 16 {
		extension = ""
	}
	file, err := os.CreateTemp("", "codex-remote-*"+extension)
	if err != nil {
		return nil, fmt.Errorf("create upload: %w", err)
	}
	path := file.Name()
	limited := io.LimitReader(data, maxUploadSize+1)
	header, err := io.ReadAll(io.LimitReader(limited, 512))
	if err == nil {
		_, err = file.Write(header)
	}
	var written int64
	if err == nil {
		written, err = io.Copy(file, limited)
	}
	if err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("save upload: %w", err)
	}
	if len(header) == 0 || int64(len(header))+written > maxUploadSize {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, errors.New("invalid file size")
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("save upload: %w", err)
	}
	image := strings.HasPrefix(http.DetectContentType(header), "image/") ||
		strings.Contains(" .heic .heif .webp ", " "+strings.ToLower(extension)+" ")
	s.uploadMu.Lock()
	s.uploads[path] = image
	s.uploadMu.Unlock()
	return map[string]any{"path": path, "image": image}, nil
}

func (s *Server) userInput(request turnRequest) ([]map[string]string, error) {
	text := strings.TrimSpace(request.Input)
	if text == "" && len(request.Attachments) == 0 {
		return nil, errors.New("input or attachments are required")
	}
	input := make([]map[string]string, 0, len(request.Attachments)+1)
	var files strings.Builder
	for _, attachment := range request.Attachments {
		s.uploadMu.RLock()
		image, ok := s.uploads[attachment.Path]
		s.uploadMu.RUnlock()
		if !ok {
			return nil, errors.New("invalid attachment path")
		}
		name := filepath.Base(strings.TrimSpace(attachment.Name))
		if name == "." || name == "" {
			name = filepath.Base(attachment.Path)
		}
		name = strings.NewReplacer("\r", " ", "\n", " ").Replace(name)
		fmt.Fprintf(&files, "\n## %s: %s\n", name, attachment.Path)
		if image {
			input = append(input, map[string]string{"type": "localImage", "path": attachment.Path})
		}
	}
	if files.Len() > 0 {
		if text != "" {
			text += "\n\n"
		}
		text += "# Files mentioned by the user:\n" + files.String()
	}
	return append([]map[string]string{{"type": "text", "text": text}}, input...), nil
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

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "encode response: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) Start(address string) error {
	listener, err := net.Listen("tcp4", address)
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
	host, port, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		return ""
	}
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
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
	host, port, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		return ""
	}
	if host != "0.0.0.0" {
		return "http://" + net.JoinHostPort(host, port)
	}
	host = "127.0.0.1"
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
	defer s.cleanupUploads()
	if s.listener == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) cleanupUploads() {
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	for path := range s.uploads {
		_ = os.Remove(path)
		delete(s.uploads, path)
	}
}
