package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/MaimoryLab/codex-server/internal/diagnostics"
)

func TestServerStatusEndpoint(t *testing.T) {
	server := New(func(context.Context) diagnostics.Snapshot {
		return diagnostics.Snapshot{Platform: "test", Architecture: "test"}
	})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close(context.Background()) })

	response, err := http.Get(server.Addr() + "/api/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot diagnostics.Snapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Platform != "test" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}
