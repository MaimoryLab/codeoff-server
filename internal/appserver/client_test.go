package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestInitializeAndReceiveEvent(t *testing.T) {
	clientTransport, serverTransport := net.Pipe()
	client := New(clientTransport)
	t.Cleanup(func() { _ = client.Close() })

	serverDone := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(serverTransport)
		line, err := reader.ReadBytes('\n')
		if err != nil {
			serverDone <- err
			return
		}
		var request message
		if err := json.Unmarshal(line, &request); err != nil {
			serverDone <- err
			return
		}
		encoder := json.NewEncoder(serverTransport)
		if err := encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"userAgent": "test", "codexHome": "/tmp/codex"}}); err != nil {
			serverDone <- err
			return
		}
		if _, err := reader.ReadBytes('\n'); err != nil {
			serverDone <- err
			return
		}
		serverDone <- encoder.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread-1"}})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := client.Initialize(ctx, ClientInfo{Name: "test", Title: "Test", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.UserAgent != "test" {
		t.Fatalf("unexpected initialize response: %+v", result)
	}
	select {
	case event := <-client.Events():
		if event.Method != "turn/completed" {
			t.Fatalf("unexpected event: %+v", event)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestCallReturnsRPCError(t *testing.T) {
	clientTransport, serverTransport := net.Pipe()
	client := New(clientTransport)
	t.Cleanup(func() { _ = client.Close() })
	go func() {
		reader := bufio.NewReader(serverTransport)
		line, _ := reader.ReadBytes('\n')
		var request message
		_ = json.Unmarshal(line, &request)
		_ = json.NewEncoder(serverTransport).Encode(map[string]any{
			"id":    request.ID,
			"error": map[string]any{"code": -32001, "message": "Server overloaded; retry later."},
		})
	}()

	err := client.Call(context.Background(), "thread/list", map[string]any{}, nil)
	if err == nil || err.Error() != "app-server error -32001: Server overloaded; retry later." {
		t.Fatalf("unexpected error: %v", err)
	}
}
