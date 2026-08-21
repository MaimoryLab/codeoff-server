package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
)

func TestManagerStartAndStop(t *testing.T) {
	manager := newManager(func(context.Context, string, ...string) (*Client, error) {
		clientTransport, serverTransport := net.Pipe()
		go serveInitialize(serverTransport)
		return New(clientTransport), nil
	})

	state, err := manager.Start(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Running || state.CodexHome != "/tmp/codex" {
		t.Fatalf("unexpected state: %+v", state)
	}
	if err := manager.Stop(); err != nil {
		t.Fatal(err)
	}
	if manager.State().Running {
		t.Fatal("manager still running after stop")
	}
}

func serveInitialize(connection net.Conn) {
	defer connection.Close()
	reader := bufio.NewReader(connection)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return
	}
	var request message
	if json.Unmarshal(line, &request) != nil {
		return
	}
	if json.NewEncoder(connection).Encode(map[string]any{
		"id":     request.ID,
		"result": map[string]any{"userAgent": "test", "codexHome": "/tmp/codex"},
	}) != nil {
		return
	}
	_, _ = reader.ReadBytes('\n')
}
