package main

import (
	"context"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

func TestRunUpdateChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make(chan struct{})
	done := make(chan struct{})
	go func() {
		runUpdateChecks(ctx, time.Millisecond, func(ctx context.Context) {
			select {
			case calls <- struct{}{}:
			case <-ctx.Done():
			}
		})
		close(done)
	}()
	for range 2 {
		select {
		case <-calls:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for update check")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("update checks did not stop after cancellation")
	}
}

func TestOTAAssetMatcher(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "codeoff-server-darwin-arm64.dmg"},
		{Name: "ota-codeoff-server-darwin-arm64.zip"},
	}
	got := otaAssetMatcher(updater.CheckRequest{Platform: "darwin", Arch: "arm64"}, assets)
	if got != 1 {
		t.Fatalf("otaAssetMatcher() = %d, want 1", got)
	}
}
