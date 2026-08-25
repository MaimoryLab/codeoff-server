package diagnostics

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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

func TestLoginShellPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS login-shell behavior is platform-specific")
	}
	shell := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'startup noise\\036/custom/bin:/usr/bin\\037trailing noise'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	oldShell := os.Getenv("SHELL")
	t.Setenv("SHELL", shell)
	t.Cleanup(func() { _ = os.Setenv("SHELL", oldShell) })
	if path := loginShellPath(shell, []string{"SHELL=" + shell, "PATH=/usr/bin"}); path != "/custom/bin:/usr/bin" {
		t.Fatalf("login shell PATH = %q", path)
	}
}
