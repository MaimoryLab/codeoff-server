package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		config Config
		want   string
	}{
		{name: "mode", config: Config{CFTunnelMode: "named"}, want: "cf tunnel mode"},
		{name: "external url", config: Config{CFTunnelMode: "external"}, want: "URL is required"},
		{name: "url path", config: Config{CFTunnelMode: "external", CFTunnelURL: "https://example.com/app"}, want: "origin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(test.config)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	if err := os.WriteFile(path, []byte(`{"controlAddr":"http://127.0.0.1:11037","adminToken":"token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(path)
	if err != nil || state.ControlAddr == "" || state.AdminToken == "" {
		t.Fatalf("state = %#v, err = %v", state, err)
	}
}
