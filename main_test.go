package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

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
