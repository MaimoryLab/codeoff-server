package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Keep the mobile thread/read response intact while hydrating paginated history.
func readThread(ctx context.Context, client *Client, params any) (json.RawMessage, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	var request struct {
		ThreadID     string `json:"threadId"`
		IncludeTurns bool   `json:"includeTurns"`
	}
	if err := json.Unmarshal(data, &request); err != nil {
		return nil, err
	}
	var result json.RawMessage
	if !request.IncludeTurns {
		err := client.Call(ctx, "thread/read", params, &result)
		return result, err
	}
	var response map[string]json.RawMessage
	if err := client.Call(ctx, "thread/read", map[string]any{"threadId": request.ThreadID, "includeTurns": false}, &response); err != nil {
		return nil, err
	}
	var thread map[string]json.RawMessage
	if err := json.Unmarshal(response["thread"], &thread); err != nil {
		return nil, err
	}
	if string(thread["historyMode"]) != `"paginated"` {
		err := client.Call(ctx, "thread/read", params, &result)
		if isUnmaterializedThread(err) {
			return json.Marshal(response)
		}
		return result, err
	}
	// ponytail: mobile expects all turns; add UI pagination if responses grow too large.
	turns := []json.RawMessage{}
	args := map[string]any{"threadId": request.ThreadID, "sortDirection": "asc", "itemsView": "full", "limit": 100}
	seen := map[string]bool{}
	for {
		var page struct {
			Data       []json.RawMessage `json:"data"`
			NextCursor string            `json:"nextCursor"`
		}
		if err := client.Call(ctx, "thread/turns/list", args, &page); err != nil {
			if len(turns) == 0 && isUnmaterializedThread(err) {
				return json.Marshal(response)
			}
			return nil, fmt.Errorf("read paginated thread history: %w", err)
		}
		turns = append(turns, page.Data...)
		if page.NextCursor == "" {
			break
		}
		if seen[page.NextCursor] {
			return nil, fmt.Errorf("repeated thread history cursor %q", page.NextCursor)
		}
		seen[page.NextCursor] = true
		args["cursor"] = page.NextCursor
	}
	thread["turns"], err = json.Marshal(turns)
	if err != nil {
		return nil, err
	}
	response["thread"], err = json.Marshal(thread)
	if err != nil {
		return nil, err
	}
	return json.Marshal(response)
}

func isUnmaterializedThread(err error) bool {
	rpcError, ok := errors.AsType[*RPCError](err)
	return ok && rpcError.Code == -32600 && strings.Contains(rpcError.Message, "not materialized yet")
}
