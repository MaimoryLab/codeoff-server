package control

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/codeoff-server/internal/appserver"
	"github.com/MaimoryLab/codeoff-server/internal/devices"
	"github.com/MaimoryLab/codeoff-server/internal/diagnostics"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type fakeRemoteAppServer struct {
	events chan appserver.Event
}

func (s *fakeRemoteAppServer) Call(context.Context, string, any) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (s *fakeRemoteAppServer) ResumeThread(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (s *fakeRemoteAppServer) ReleaseThread(context.Context, string) (bool, error) { return true, nil }
func (s *fakeRemoteAppServer) TakeOverThread(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (s *fakeRemoteAppServer) Respond(int64, any, *appserver.RPCError) error { return nil }
func (s *fakeRemoteAppServer) Events() <-chan appserver.Event                { return s.events }

func TestStartUsesConfiguredAddress(t *testing.T) {
	store, err := devices.Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := New(func(context.Context) diagnostics.Snapshot { return diagnostics.Snapshot{} }, store)
	if err := server.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close(context.Background()) })
	host, port, err := net.SplitHostPort(server.ListenAddr())
	if err != nil || host != "127.0.0.1" || port == "0" {
		t.Fatalf("listen address = %q", server.ListenAddr())
	}
}

func TestPairExchangeAndWebSocketStatus(t *testing.T) {
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
	response, err := http.Post(httpServer.URL+"/api/v1/pair/exchange", "application/json", strings.NewReader(string(body)))
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

	wsURL, err := url.Parse(httpServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	wsURL.Scheme = "ws"
	wsURL.Path = "/api/v1/ws"
	conn, response, err := websocket.Dial(context.Background(), wsURL.String(), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + exchange.Token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Header.Get(serverVersionHeader) != serverVersion {
		t.Fatalf("server version = %q", response.Header.Get(serverVersionHeader))
	}
	if response.Header.Get(minClientHeader) != minClientVersion {
		t.Fatalf("minimum client version = %q", response.Header.Get(minClientHeader))
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	if err := wsjson.Write(context.Background(), conn, map[string]any{
		"id": 1, "method": "status",
	}); err != nil {
		t.Fatal(err)
	}
	var result struct {
		ID     int            `json:"id"`
		Result map[string]any `json:"result"`
	}
	if err := wsjson.Read(context.Background(), conn, &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != 1 || result.Result["server"] == nil {
		t.Fatalf("websocket status = %#v", result)
	}
	if err := wsjson.Write(context.Background(), conn, map[string]any{
		"id": 2, "method": "heartbeat",
	}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(context.Background(), conn, &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != 2 || result.Result["ack"] != true {
		t.Fatalf("heartbeat response = %#v", result)
	}
}

func TestHTTPUpload(t *testing.T) {
	store, err := devices.Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.NewPairing()
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Exchange(pairing.Token, "Phone")
	if err != nil {
		t.Fatal(err)
	}
	server := New(func(context.Context) diagnostics.Snapshot { return diagnostics.Snapshot{} }, store)
	t.Cleanup(server.cleanupUploads)
	httpServer := httptest.NewServer(server.httpServer.Handler)
	t.Cleanup(httpServer.Close)

	request, err := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/upload?name=photo.jpg", strings.NewReader("image data"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d", response.StatusCode)
	}
	var result struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil || string(data) != "image data" {
		t.Fatalf("uploaded data = %q, %v", data, err)
	}
}

func TestWebSocketPushesEventsWithIDs(t *testing.T) {
	store, err := devices.Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.NewPairing()
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeRemoteAppServer{events: make(chan appserver.Event, 1)}
	server := New(func(context.Context) diagnostics.Snapshot { return diagnostics.Snapshot{} }, store, fake)
	httpServer := httptest.NewServer(server.httpServer.Handler)
	t.Cleanup(httpServer.Close)
	body, _ := json.Marshal(map[string]string{"token": pairing.Token, "name": "Phone"})
	response, err := http.Post(httpServer.URL+"/api/v1/pair/exchange", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	var exchange struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&exchange); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	wsURL, _ := url.Parse(httpServer.URL)
	wsURL.Scheme, wsURL.Path = "ws", "/api/v1/ws"
	conn, _, err := websocket.Dial(context.Background(), wsURL.String(), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + exchange.Token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	id := int64(7)
	fake.events <- appserver.Event{ID: &id, Method: "approval/requested", Params: json.RawMessage(`{"request":"ok"}`)}
	var event map[string]any
	if err := wsjson.Read(context.Background(), conn, &event); err != nil {
		t.Fatal(err)
	}
	if event["method"] != "approval/requested" || event["id"] != float64(7) {
		t.Fatalf("event = %#v", event)
	}
}
