package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"
)

func TestManagerStartAndStop(t *testing.T) {
	manager := newManager(func(context.Context, string, ...string) (*Client, error) {
		clientTransport, serverTransport := net.Pipe()
		go serveInitialize(serverTransport)
		return New(clientTransport), nil
	})

	state, err := manager.Toggle(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Running || state.CodexHome != "/tmp/codex" {
		t.Fatalf("unexpected state: %+v", state)
	}
	state, err = manager.Toggle(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if state.Running {
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

func TestManagerTakeOverThread(t *testing.T) {
	var terminated atomic.Bool
	manager := newManager(func(context.Context, string, ...string) (*Client, error) {
		clientTransport, serverTransport := net.Pipe()
		go serveTakeover(serverTransport, &terminated)
		return New(clientTransport), nil
	})
	manager.terminate = func(_ context.Context, codexHome, threadID string) error {
		if codexHome != "/tmp/codex" || threadID != "thread-42" {
			t.Fatalf("terminate(%q, %q)", codexHome, threadID)
		}
		terminated.Store(true)
		return nil
	}
	t.Cleanup(func() { _ = manager.Stop() })
	if _, err := manager.Start(context.Background(), "codex"); err != nil {
		t.Fatal(err)
	}

	result, err := manager.TakeOverThread(context.Background(), "thread-42")
	if err != nil {
		t.Fatal(err)
	}
	if !terminated.Load() || string(result) != `{"thread":{"id":"thread-42"}}` {
		t.Fatalf("terminated = %t, result = %s", terminated.Load(), result)
	}
}

func TestManagerTakeOverThreadReleasesOwnLock(t *testing.T) {
	var unsubscribed atomic.Bool
	var terminated atomic.Bool
	manager := newManager(func(context.Context, string, ...string) (*Client, error) {
		clientTransport, serverTransport := net.Pipe()
		go serveOwnTakeover(serverTransport, &unsubscribed)
		return New(clientTransport), nil
	})
	manager.terminate = func(context.Context, string, string) error {
		terminated.Store(true)
		return nil
	}
	t.Cleanup(func() { _ = manager.Stop() })
	if _, err := manager.Start(context.Background(), "codex"); err != nil {
		t.Fatal(err)
	}

	result, err := manager.TakeOverThread(context.Background(), "thread-42")
	if err != nil {
		t.Fatal(err)
	}
	if !unsubscribed.Load() || terminated.Load() || string(result) != `{"thread":{"id":"thread-42"}}` {
		t.Fatalf("unsubscribed = %t, terminated = %t, result = %s", unsubscribed.Load(), terminated.Load(), result)
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

func serveTakeover(connection net.Conn, terminated *atomic.Bool) {
	defer connection.Close()
	scanner := bufio.NewScanner(connection)
	encoder := json.NewEncoder(connection)
	for scanner.Scan() {
		var request message
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == nil {
			continue
		}
		response := map[string]any{"id": request.ID}
		switch request.Method {
		case "initialize":
			response["result"] = map[string]any{"userAgent": "test", "codexHome": "/tmp/codex"}
		case "thread/resume":
			if !terminated.Load() {
				response["error"] = map[string]any{"code": -32600, "message": "active writer"}
			} else {
				response["result"] = map[string]any{"thread": map[string]string{"id": "thread-42"}}
			}
		default:
			response["result"] = map[string]any{}
		}
		if encoder.Encode(response) != nil {
			return
		}
	}
}

func serveOwnTakeover(connection net.Conn, unsubscribed *atomic.Bool) {
	defer connection.Close()
	scanner := bufio.NewScanner(connection)
	encoder := json.NewEncoder(connection)
	for scanner.Scan() {
		var request message
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == nil {
			continue
		}
		response := map[string]any{"id": request.ID, "result": map[string]any{}}
		switch request.Method {
		case "initialize":
			response["result"] = map[string]any{"userAgent": "test", "codexHome": "/tmp/codex"}
		case "thread/resume":
			if !unsubscribed.Load() {
				response["error"] = map[string]any{"code": -32600, "message": "active writer"}
			} else {
				response["result"] = map[string]any{"thread": map[string]string{"id": "thread-42"}}
			}
		case "thread/unsubscribe":
			unsubscribed.Store(true)
		}
		if encoder.Encode(response) != nil {
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
