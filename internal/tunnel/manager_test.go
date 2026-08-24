package tunnel

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestFindURL(t *testing.T) {
	if got := quickTunnelURL.FindString("created https://demo-123.trycloudflare.com"); got != "https://demo-123.trycloudflare.com" {
		t.Fatalf("unexpected URL: %q", got)
	}
}

func writeExecutable(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func TestManagerQuickTunnelProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test uses a POSIX script")
	}
	script := t.TempDir() + "/cloudflared"
	if err := writeExecutable(script, "#!/bin/sh\nfor arg in \"$@\"; do\n  if [ \"$arg\" = \"--output\" ]; then\n    echo '{\"message\":\"https://demo.trycloudflare.com\"}' >&2\n  fi\ndone\nsleep 30\n"); err != nil {
		t.Fatal(err)
	}
	manager := NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	state, err := manager.Toggle(ctx, script, "http://127.0.0.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Running || state.URL != "https://demo.trycloudflare.com" {
		t.Fatalf("unexpected state: %+v", state)
	}
	state, err = manager.Toggle(ctx, script, "http://127.0.0.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	if state.Running {
		t.Fatal("manager still running after toggle")
	}
}
