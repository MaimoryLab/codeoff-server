package appserver

import (
	"encoding/json"
	"errors"
)

func requestKey(id json.RawMessage) string {
	var text string
	if json.Unmarshal(id, &text) == nil {
		return "s:" + text
	}
	return "n:" + string(id)
}

func (c *Client) trackApproval(request message) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	switch request.Method {
	case "item/permissions/requestApproval", "execCommandApproval", "applyPatchApproval":
		if request.ID != nil {
			c.approvals[requestKey(request.ID)] = request
		}
	case "serverRequest/resolved":
		var params struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(request.Params, &params) == nil {
			delete(c.approvals, requestKey(params.RequestID))
		}
	}
}

// Translate the mobile allow/deny decision into the result required by the request.
func approvalResult(request message, result any) (any, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var response struct {
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	if request.Method != "item/permissions/requestApproval" {
		switch response.Decision {
		case "accept":
			return map[string]any{"decision": "approved"}, nil
		case "acceptForSession":
			return map[string]any{"decision": "approved_for_session"}, nil
		case "decline":
			return map[string]any{"decision": map[string]any{"denied": map[string]string{"rejection": "User declined"}}}, nil
		case "cancel":
			return map[string]any{"decision": "abort"}, nil
		default:
			return result, nil
		}
	}
	permissions := map[string]json.RawMessage{}
	scope := "turn"
	switch response.Decision {
	case "accept", "acceptForSession":
		var params struct {
			Permissions map[string]json.RawMessage `json:"permissions"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, err
		}
		for key, value := range params.Permissions {
			if string(value) != "null" {
				permissions[key] = value
			}
		}
		if response.Decision == "acceptForSession" {
			scope = "session"
		}
	case "decline", "cancel":
	default:
		return nil, errors.New("invalid permissions approval decision")
	}
	return map[string]any{"permissions": permissions, "scope": scope}, nil
}
