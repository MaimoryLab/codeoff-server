package appserver

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestReadThreadHistory(t *testing.T) {
	for _, mode := range []string{"legacy", "paginated", "unsupported", "repeated"} {
		t.Run(mode, func(t *testing.T) {
			left, right := net.Pipe()
			client := New(left)
			t.Cleanup(func() { _ = client.Close(); _ = right.Close() })
			go func() {
				decoder, encoder := json.NewDecoder(right), json.NewEncoder(right)
				for {
					var request message
					if decoder.Decode(&request) != nil {
						return
					}
					var params map[string]any
					_ = json.Unmarshal(request.Params, &params)
					reply := message{ID: request.ID}
					switch request.Method {
					case "thread/read":
						if mode == "legacy" {
							reply.Result = json.RawMessage(`{"thread":{"id":"t","historyMode":"legacy","turns":[{"id":"old","items":[]}]}}`)
						} else {
							if params["includeTurns"] != false {
								t.Error("requested full history before inspecting historyMode")
							}
							reply.Result = json.RawMessage(`{"thread":{"id":"t","historyMode":"paginated","status":{"type":"active"},"turns":[]}}`)
						}
					case "thread/turns/list":
						if params["itemsView"] != "full" || params["sortDirection"] != "asc" {
							t.Errorf("pagination params = %s", request.Params)
						}
						if mode == "unsupported" {
							reply.Error = &RPCError{Code: -32601, Message: "list_turns is not supported yet"}
						} else if params["cursor"] == nil || mode == "repeated" {
							reply.Result = json.RawMessage(`{"data":[{"id":"old","itemsView":"full","items":[{"id":"message","type":"agentMessage","text":"hello"}]}],"nextCursor":"next"}`)
						} else {
							reply.Result = json.RawMessage(`{"data":[{"id":"new","itemsView":"full","status":"inProgress","items":[]}],"nextCursor":null}`)
						}
					default:
						t.Errorf("unexpected method %s", request.Method)
					}
					if encoder.Encode(reply) != nil {
						return
					}
				}
			}()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			data, err := readThread(ctx, client, map[string]any{"threadId": "t", "includeTurns": true})
			if mode == "unsupported" || mode == "repeated" {
				if err == nil || !strings.Contains(err.Error(), map[string]string{"unsupported": "not supported", "repeated": "repeated"}[mode]) {
					t.Fatalf("expected history error, got %s: %v", data, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Thread struct {
					Turns []struct {
						ID    string
						Items []json.RawMessage
					}
				}
			}
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			if result.Thread.Turns[0].ID != "old" {
				t.Fatalf("history = %s", data)
			}
			if mode == "paginated" && (len(result.Thread.Turns) != 2 || result.Thread.Turns[1].ID != "new" || len(result.Thread.Turns[0].Items) != 1) {
				t.Fatalf("incomplete history = %s", data)
			}
		})
	}
}
