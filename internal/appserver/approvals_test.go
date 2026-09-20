package appserver

import (
	"encoding/json"
	"testing"
)

func TestApprovalResult(t *testing.T) {
	amendment := json.RawMessage(`{"decision":{"approved_execpolicy_amendment":{"proposed_execpolicy_amendment":["git","status"]}}}`)
	result, err := approvalResult(message{Method: "execCommandApproval"}, amendment)
	data, _ := json.Marshal(result)
	if err != nil || string(data) != string(amendment) {
		t.Fatalf("changed legacy amendment: %s: %v", data, err)
	}
	for _, test := range []struct{ method, decision, want string }{
		{"item/permissions/requestApproval", "accept", `{"permissions":{"network":{"enabled":true}},"scope":"turn"}`},
		{"item/permissions/requestApproval", "acceptForSession", `{"permissions":{"network":{"enabled":true}},"scope":"session"}`},
		{"item/permissions/requestApproval", "decline", `{"permissions":{},"scope":"turn"}`},
		{"item/permissions/requestApproval", "cancel", `{"permissions":{},"scope":"turn"}`},
		{"execCommandApproval", "accept", `{"decision":"approved"}`},
		{"applyPatchApproval", "decline", `{"decision":{"denied":{"rejection":"User declined"}}}`},
	} {
		t.Run(test.method+"/"+test.decision, func(t *testing.T) {
			result, err := approvalResult(message{Method: test.method,
				Params: json.RawMessage(`{"permissions":{"network":{"enabled":true},"fileSystem":null}}`)},
				map[string]string{"decision": test.decision})
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(result)
			if err != nil || string(data) != test.want {
				t.Fatalf("result = %s, want %s: %v", data, test.want, err)
			}
		})
	}
}

func TestApprovalResolution(t *testing.T) {
	client := &Client{approvals: make(map[string]message)}
	client.trackApproval(message{ID: json.RawMessage(`"\u0031"`), Method: "item/permissions/requestApproval"})
	client.trackApproval(message{ID: json.RawMessage(`1`), Method: "execCommandApproval"})
	client.trackApproval(message{Method: "serverRequest/resolved", Params: json.RawMessage(`{"requestId":"1"}`)})
	if len(client.approvals) != 1 || client.approvals[requestKey(json.RawMessage(`1`))].Method != "execCommandApproval" {
		t.Fatalf("pending approvals = %#v", client.approvals)
	}
}
