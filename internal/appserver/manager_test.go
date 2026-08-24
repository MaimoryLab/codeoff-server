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

func TestManagerReleaseThread(t *testing.T) {
	for _, status := range []string{"idle", "active"} {
		t.Run(status, func(t *testing.T) {
			starts := 0
			manager := newManager(func(context.Context, string, ...string) (*Client, error) {
				starts++
				clientTransport, serverTransport := net.Pipe()
				go serveRelease(serverTransport, status)
				return New(clientTransport), nil
			})
			t.Cleanup(func() { _ = manager.Stop() })
			if _, err := manager.Start(context.Background(), "codex"); err != nil {
				t.Fatal(err)
			}
			released, err := manager.ReleaseThread(context.Background(), "thread-42")
			if err != nil {
				t.Fatal(err)
			}
			if released != (status == "idle") {
				t.Fatalf("released = %t", released)
			}
			expectedStarts := 1
			if released {
				expectedStarts = 2
			}
			if starts != expectedStarts {
				t.Fatalf("starts = %d", starts)
			}
		})
	}
}

func serveRelease(connection net.Conn, status string) {
	defer connection.Close()
	scanner := bufio.NewScanner(connection)
	encoder := json.NewEncoder(connection)
	for scanner.Scan() {
		var request message
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == nil {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"userAgent": "test", "codexHome": "/tmp/codex"}
		case "thread/unsubscribe":
			result = map[string]string{"status": "unsubscribed"}
		case "thread/loaded/list":
			result = map[string]any{"data": []string{"thread-42"}, "nextCursor": nil}
		case "thread/read":
			result = map[string]any{"thread": map[string]any{"status": map[string]string{"type": status}}}
		default:
			result = map[string]any{}
		}
		if encoder.Encode(map[string]any{"id": request.ID, "result": result}) != nil {
			return
		}
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
