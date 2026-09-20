package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run with CODEOFF_CODEX_TEST=1 go test ./internal/appserver -run TestCodexProtocol -v.
// The isolated Codex process uses only a local failing provider, with no model calls.
func TestCodexProtocol(t *testing.T) {
	if os.Getenv("CODEOFF_CODEX_TEST") != "1" {
		t.Skip("set CODEOFF_CODEX_TEST=1 to test the installed Codex binary")
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	version, err := exec.Command(binary, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(strings.TrimSpace(string(version)))
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"offline protocol test"}}`, http.StatusUnauthorized)
	}))
	defer provider.Close()
	for _, mode := range []string{"legacy", "paginated"} {
		t.Run(mode, func(t *testing.T) {
			taskDir := t.TempDir()
			config := fmt.Sprintf("model = \"protocol-test\"\nmodel_provider = \"protocol_test\"\n[model_providers.protocol_test]\nname = \"Protocol test\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\n", provider.URL)
			if err := os.WriteFile(filepath.Join(taskDir, "config.toml"), []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "app-server", "--stdio")
			command.Env = append(command.Environ(), "CODEX_HOME="+taskDir, "OPENAI_API_KEY=", "CODEX_API_KEY=")
			stdin, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			client := New(&stdioTransport{Reader: stdout, Writer: stdin})
			defer func() {
				_ = stdin.Close()
				_ = command.Wait()
				_ = client.Close()
			}()
			if err := client.Call(ctx, "initialize", map[string]any{
				"clientInfo":   ClientInfo{Name: "codeoff_protocol_test", Version: "1"},
				"capabilities": map[string]bool{"experimentalApi": true},
			}, nil); err != nil {
				t.Fatal(err)
			}
			if err := client.Notify("initialized", map[string]any{}); err != nil {
				t.Fatal(err)
			}
			var created struct {
				Thread struct {
					ID          string
					HistoryMode string
				}
			}
			if err := client.Call(ctx, "thread/start", map[string]string{"cwd": taskDir, "historyMode": mode}, &created); err != nil {
				t.Fatal(err)
			}
			if created.Thread.HistoryMode != mode {
				t.Fatalf("history mode = %q", created.Thread.HistoryMode)
			}
			params := map[string]any{"threadId": created.Thread.ID, "includeTurns": true}
			_, emptyErr := readThread(ctx, client, params)
			if emptyErr != nil {
				t.Logf("unmaterialized thread: %v", emptyErr)
			}
			for _, text := range []string{"protocol check one", "protocol check two"} {
				if err := client.Call(ctx, "turn/start", map[string]any{"threadId": created.Thread.ID,
					"input": []map[string]any{{"type": "text", "text": text, "text_elements": []any{}}},
				}, nil); err != nil {
					t.Fatal(err)
				}
				completed := false
				for !completed {
					select {
					case event, ok := <-client.Events():
						if !ok {
							t.Fatal(client.Err())
						}
						completed = event.Method == "turn/completed"
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
			}
			data, err := readThread(ctx, client, params)
			if err != nil {
				t.Fatal(err)
			}
			var history struct {
				Thread struct{ Turns []json.RawMessage }
			}
			if err := json.Unmarshal(data, &history); err != nil {
				t.Fatal(err)
			}
			if len(history.Thread.Turns) != 2 || !strings.Contains(string(history.Thread.Turns[0]), "protocol check one") || !strings.Contains(string(history.Thread.Turns[1]), "protocol check two") {
				t.Fatalf("unexpected history: %s", data)
			}
			if _, err := resumeThread(ctx, client, map[string]string{"threadId": created.Thread.ID}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
