package control

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
