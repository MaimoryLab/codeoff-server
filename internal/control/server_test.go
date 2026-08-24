package control

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/codex-server/internal/appserver"
	"github.com/MaimoryLab/codex-server/internal/devices"
	"github.com/MaimoryLab/codex-server/internal/diagnostics"
)

type fakeApprovalServer struct {
	id     int64
	result any
	method string
	params any
}

func (s *fakeApprovalServer) Call(_ context.Context, method string, params any) (json.RawMessage, error) {
	s.method, s.params = method, params
	return json.RawMessage(`{}`), nil
}

func (s *fakeApprovalServer) ReleaseThread(_ context.Context, threadID string) (bool, error) {
	s.method, s.params = "thread/release", map[string]string{"threadId": threadID}
	return true, nil
}

func (s *fakeApprovalServer) TakeOverThread(_ context.Context, threadID string) (json.RawMessage, error) {
	s.method, s.params = "thread/takeover", map[string]string{"threadId": threadID}
	return json.RawMessage(`{"thread":{"id":"thread-42"}}`), nil
}

func (s *fakeApprovalServer) Respond(id int64, result any, _ *appserver.RPCError) error {
	s.id, s.result = id, result
	return nil
}

func (s *fakeApprovalServer) Events() <-chan appserver.Event { return make(chan appserver.Event) }

func TestPairExchangeAndAuthenticatedStatus(t *testing.T) {
	store, err := devices.Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.NewPairing()
	if err != nil {
		t.Fatal(err)
	}
	server := New(func(context.Context) diagnostics.Snapshot {
		return diagnostics.Snapshot{Platform: "test", Architecture: "test"}
	}, store)
	httpServer := httptest.NewServer(server.httpServer.Handler)
	t.Cleanup(httpServer.Close)

	body, _ := json.Marshal(map[string]string{"token": pairing.Token, "name": "Phone"})
	response, err := http.Post(httpServer.URL+"/api/v1/pair/exchange", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("pair status = %d", response.StatusCode)
	}
	var exchange struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&exchange); err != nil {
		t.Fatal(err)
	}

	unauthorized, err := http.Get(httpServer.URL + "/api/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.StatusCode)
	}

	request, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/v1/status", nil)
	request.Header.Set("Authorization", "Bearer "+exchange.Token)
	authorized, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer authorized.Body.Close()
	if authorized.StatusCode != http.StatusOK {
		t.Fatalf("authorized status = %d", authorized.StatusCode)
	}
	if _, err := io.ReadAll(authorized.Body); err != nil {
		t.Fatal(err)
	}
}

func TestDirectoriesListsOnlyDirectories(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "project")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := devices.Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.NewPairing()
	if err != nil {
		t.Fatal(err)
	}
	server := New(func(context.Context) diagnostics.Snapshot { return diagnostics.Snapshot{} }, store)
	httpServer := httptest.NewServer(server.httpServer.Handler)
	t.Cleanup(httpServer.Close)

	pairBody, _ := json.Marshal(map[string]string{"token": pairing.Token, "name": "Phone"})
	pairResponse, err := http.Post(httpServer.URL+"/api/v1/pair/exchange", "application/json", bytes.NewReader(pairBody))
	if err != nil {
		t.Fatal(err)
	}
	defer pairResponse.Body.Close()
	var exchange struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(pairResponse.Body).Decode(&exchange); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/v1/directories?path="+url.QueryEscape(root), nil)
	request.Header.Set("Authorization", "Bearer "+exchange.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var result struct {
		Path        string           `json:"path"`
		Directories []directoryEntry `json:"directories"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Path != root || len(result.Directories) != 1 || result.Directories[0].Name != "project" {
		t.Fatalf("directories = %#v", result)
	}
}

func TestApprovalResponseRequiresAuthAndForwardsDecision(t *testing.T) {
	store, err := devices.Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.NewPairing()
	if err != nil {
		t.Fatal(err)
	}
	fake := new(fakeApprovalServer)
	server := New(func(context.Context) diagnostics.Snapshot { return diagnostics.Snapshot{} }, store, fake)
	httpServer := httptest.NewServer(server.httpServer.Handler)
	t.Cleanup(httpServer.Close)

	pairBody, _ := json.Marshal(map[string]string{"token": pairing.Token, "name": "Phone"})
	pairResponse, err := http.Post(httpServer.URL+"/api/v1/pair/exchange", "application/json", bytes.NewReader(pairBody))
	if err != nil {
		t.Fatal(err)
	}
	defer pairResponse.Body.Close()
	var exchange struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(pairResponse.Body).Decode(&exchange); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/approvals/42", bytes.NewReader([]byte(`{"decision":"accept"}`)))
	request.Header.Set("Authorization", "Bearer "+exchange.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("approval status = %d", response.StatusCode)
	}
	if fake.id != 42 {
		t.Fatalf("approval id = %d", fake.id)
	}
	result, ok := fake.result.(map[string]json.RawMessage)
	if !ok || string(result["decision"]) != `"accept"` {
		t.Fatalf("approval result = %#v", fake.result)
	}
}

func TestThreadActionsForwardThreadID(t *testing.T) {
	store, err := devices.Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.NewPairing()
	if err != nil {
		t.Fatal(err)
	}
	fake := new(fakeApprovalServer)
	server := New(func(context.Context) diagnostics.Snapshot { return diagnostics.Snapshot{} }, store, fake)
	httpServer := httptest.NewServer(server.httpServer.Handler)
	t.Cleanup(httpServer.Close)

	pairBody, _ := json.Marshal(map[string]string{"token": pairing.Token, "name": "Phone"})
	pairResponse, err := http.Post(httpServer.URL+"/api/v1/pair/exchange", "application/json", bytes.NewReader(pairBody))
	if err != nil {
		t.Fatal(err)
	}
	defer pairResponse.Body.Close()
	var exchange struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(pairResponse.Body).Decode(&exchange); err != nil {
		t.Fatal(err)
	}

	for _, action := range []string{"resume", "unsubscribe", "archive"} {
		t.Run(action, func(t *testing.T) {
			request, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/threads/thread-42/"+action, bytes.NewReader([]byte(`{}`)))
			request.Header.Set("Authorization", "Bearer "+exchange.Token)
			request.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", response.StatusCode)
			}
			if fake.method != "thread/"+action {
				t.Fatalf("method = %q", fake.method)
			}
			params, ok := fake.params.(map[string]string)
			if !ok || params["threadId"] != "thread-42" {
				t.Fatalf("params = %#v", fake.params)
			}
		})
	}
	t.Run("release", func(t *testing.T) {
		request, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/threads/thread-42/release", bytes.NewReader([]byte(`{}`)))
		request.Header.Set("Authorization", "Bearer "+exchange.Token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK || fake.method != "thread/release" {
			t.Fatalf("status = %d, method = %q", response.StatusCode, fake.method)
		}
	})
	t.Run("takeover", func(t *testing.T) {
		request, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/threads/thread-42/takeover", bytes.NewReader([]byte(`{}`)))
		request.Header.Set("Authorization", "Bearer "+exchange.Token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK || fake.method != "thread/takeover" {
			t.Fatalf("status = %d, method = %q", response.StatusCode, fake.method)
		}
	})
	t.Run("name", func(t *testing.T) {
		request, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/threads/thread-42/name", bytes.NewReader([]byte(`{"name":"Renamed"}`)))
		request.Header.Set("Authorization", "Bearer "+exchange.Token)
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", response.StatusCode)
		}
		params, ok := fake.params.(map[string]string)
		if fake.method != "thread/name/set" || !ok || params["threadId"] != "thread-42" || params["name"] != "Renamed" {
			t.Fatalf("method = %q, params = %#v", fake.method, fake.params)
		}
	})
}

func TestSteerTurnForwardsActiveTurn(t *testing.T) {
	store, err := devices.Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.NewPairing()
	if err != nil {
		t.Fatal(err)
	}
	fake := new(fakeApprovalServer)
	server := New(func(context.Context) diagnostics.Snapshot { return diagnostics.Snapshot{} }, store, fake)
	httpServer := httptest.NewServer(server.httpServer.Handler)
	t.Cleanup(httpServer.Close)

	pairBody, _ := json.Marshal(map[string]string{"token": pairing.Token, "name": "Phone"})
	pairResponse, err := http.Post(httpServer.URL+"/api/v1/pair/exchange", "application/json", bytes.NewReader(pairBody))
	if err != nil {
		t.Fatal(err)
	}
	defer pairResponse.Body.Close()
	var exchange struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(pairResponse.Body).Decode(&exchange); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/turns/turn-7/steer?threadId=thread-42", bytes.NewReader([]byte(`{"input":"continue"}`)))
	request.Header.Set("Authorization", "Bearer "+exchange.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("steer status = %d", response.StatusCode)
	}
	params, ok := fake.params.(map[string]any)
	input, inputOK := params["input"].([]map[string]string)
	if fake.method != "turn/steer" || !ok || !inputOK || params["threadId"] != "thread-42" || params["expectedTurnId"] != "turn-7" || len(input) != 1 || input[0]["text"] != "continue" {
		t.Fatalf("method = %q, params = %#v", fake.method, fake.params)
	}
}

func TestUploadAndTurnForwardAttachments(t *testing.T) {
	store, err := devices.Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.NewPairing()
	if err != nil {
		t.Fatal(err)
	}
	fake := new(fakeApprovalServer)
	server := New(func(context.Context) diagnostics.Snapshot { return diagnostics.Snapshot{} }, store, fake)
	t.Cleanup(server.cleanupUploads)
	httpServer := httptest.NewServer(server.httpServer.Handler)
	t.Cleanup(httpServer.Close)

	pairBody, _ := json.Marshal(map[string]string{"token": pairing.Token, "name": "Phone"})
	pairResponse, err := http.Post(httpServer.URL+"/api/v1/pair/exchange", "application/json", bytes.NewReader(pairBody))
	if err != nil {
		t.Fatal(err)
	}
	defer pairResponse.Body.Close()
	var exchange struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(pairResponse.Body).Decode(&exchange); err != nil {
		t.Fatal(err)
	}

	png := []byte("\x89PNG\r\n\x1a\nmobile image")
	upload, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/files?name=photo.png", bytes.NewReader(png))
	upload.Header.Set("Authorization", "Bearer "+exchange.Token)
	uploadResponse, err := http.DefaultClient.Do(upload)
	if err != nil {
		t.Fatal(err)
	}
	defer uploadResponse.Body.Close()
	if uploadResponse.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d", uploadResponse.StatusCode)
	}
	var uploaded struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(uploadResponse.Body).Decode(&uploaded); err != nil {
		t.Fatal(err)
	}
	textUpload, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/files?name=notes.txt", strings.NewReader("plain text"))
	textUpload.Header.Set("Authorization", "Bearer "+exchange.Token)
	textUploadResponse, err := http.DefaultClient.Do(textUpload)
	if err != nil {
		t.Fatal(err)
	}
	defer textUploadResponse.Body.Close()
	var uploadedText struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(textUploadResponse.Body).Decode(&uploadedText); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"input": "inspect this",
		"attachments": []map[string]string{
			{"name": "photo.png", "path": uploaded.Path},
			{"name": "notes.txt", "path": uploadedText.Path},
		},
	})
	request, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/threads/thread-42/turns", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+exchange.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("turn status = %d", response.StatusCode)
	}
	params, ok := fake.params.(map[string]any)
	input, inputOK := params["input"].([]map[string]string)
	if fake.method != "turn/start" || !ok || !inputOK || len(input) != 2 ||
		!strings.Contains(input[0]["text"], "photo.png: "+uploaded.Path) ||
		!strings.Contains(input[0]["text"], "notes.txt: "+uploadedText.Path) ||
		input[1]["type"] != "localImage" || input[1]["path"] != uploaded.Path {
		t.Fatalf("method = %q, params = %#v", fake.method, fake.params)
	}
}

func TestAppServerRPCErrorIsConflict(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeAppServerError(recorder, &appserver.RPCError{Code: -32600, Message: "turn already active"})
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestReadThreadIncludesTurns(t *testing.T) {
	store, err := devices.Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.NewPairing()
	if err != nil {
		t.Fatal(err)
	}
	fake := new(fakeApprovalServer)
	server := New(func(context.Context) diagnostics.Snapshot { return diagnostics.Snapshot{} }, store, fake)
	httpServer := httptest.NewServer(server.httpServer.Handler)
	t.Cleanup(httpServer.Close)

	pairBody, _ := json.Marshal(map[string]string{"token": pairing.Token, "name": "Phone"})
	pairResponse, err := http.Post(httpServer.URL+"/api/v1/pair/exchange", "application/json", bytes.NewReader(pairBody))
	if err != nil {
		t.Fatal(err)
	}
	defer pairResponse.Body.Close()
	var exchange struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(pairResponse.Body).Decode(&exchange); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/v1/threads/thread-42", nil)
	request.Header.Set("Authorization", "Bearer "+exchange.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("read status = %d", response.StatusCode)
	}
	params, ok := fake.params.(map[string]any)
	if !ok || params["threadId"] != "thread-42" || params["includeTurns"] != true {
		t.Fatalf("params = %#v", fake.params)
	}
}

func TestListThreadsForwardsPagination(t *testing.T) {
	store, err := devices.Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.NewPairing()
	if err != nil {
		t.Fatal(err)
	}
	fake := new(fakeApprovalServer)
	server := New(func(context.Context) diagnostics.Snapshot { return diagnostics.Snapshot{} }, store, fake)
	httpServer := httptest.NewServer(server.httpServer.Handler)
	t.Cleanup(httpServer.Close)

	pairBody, _ := json.Marshal(map[string]string{"token": pairing.Token, "name": "Phone"})
	pairResponse, err := http.Post(httpServer.URL+"/api/v1/pair/exchange", "application/json", bytes.NewReader(pairBody))
	if err != nil {
		t.Fatal(err)
	}
	defer pairResponse.Body.Close()
	var exchange struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(pairResponse.Body).Decode(&exchange); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/v1/threads?cursor=next-token&limit=100", nil)
	request.Header.Set("Authorization", "Bearer "+exchange.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", response.StatusCode)
	}
	params, ok := fake.params.(map[string]any)
	if !ok || params["cursor"] != "next-token" || params["limit"] != 100 {
		t.Fatalf("params = %#v", fake.params)
	}
}

func TestStartBindsAllIPv4Interfaces(t *testing.T) {
	store, err := devices.Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := New(func(context.Context) diagnostics.Snapshot { return diagnostics.Snapshot{} }, store)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close(context.Background()) })
	host, _, err := net.SplitHostPort(server.ListenAddr())
	if err != nil {
		t.Fatal(err)
	}
	if host != "0.0.0.0" {
		t.Fatalf("listen host = %q", host)
	}
	if server.Addr() == "" {
		t.Fatal("local control address is empty")
	}
}
