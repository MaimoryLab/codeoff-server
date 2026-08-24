package control

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/codex-server/internal/devices"
	"github.com/MaimoryLab/codex-server/internal/diagnostics"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

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
	conn, _, err := websocket.Dial(context.Background(), wsURL.String(), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + exchange.Token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	if err := wsjson.Write(context.Background(), conn, map[string]any{
		"id": 1, "method": "GET", "path": "/api/v1/status",
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
}
