package diagnostics

import (
	"context"
	"testing"
)

func TestCheckReportsPlatform(t *testing.T) {
	snapshot := Check(context.Background())
	if snapshot.Platform == "" || snapshot.Architecture == "" {
		t.Fatalf("missing platform information: %+v", snapshot)
	}
	if snapshot.CheckedAt.IsZero() {
		t.Fatal("expected a check timestamp")
	}
}
