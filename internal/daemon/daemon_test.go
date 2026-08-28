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

func TestLoadTunnelURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if got, err := LoadTunnelURL(path); err != nil || got != "" {
		t.Fatalf("missing tunnel URL = %q, %v", got, err)
	}
	if err := os.WriteFile(path, []byte(`{"tunnelUrl":"https://remote.example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTunnelURL(path)
	if err != nil || got != "https://remote.example.com" {
		t.Fatalf("tunnel URL = %q, %v", got, err)
	}
}

func TestMigrateConfigDir(t *testing.T) {
	t.Run("renames legacy directory", func(t *testing.T) {
		root := t.TempDir()
		oldPath, newPath := filepath.Join(root, "codex-server"), filepath.Join(root, "codeoff")
		if err := os.Mkdir(oldPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(oldPath, "settings.json"), []byte("legacy"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := migrateConfigDir(oldPath, newPath); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Fatalf("legacy directory still exists: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(newPath, "settings.json"))
		if err != nil || string(data) != "legacy" {
			t.Fatalf("migrated settings = %q, %v", data, err)
		}
	})

	t.Run("merges into existing directory", func(t *testing.T) {
		root := t.TempDir()
		oldPath, newPath := filepath.Join(root, "codex-remote"), filepath.Join(root, "codeoff")
		if err := os.MkdirAll(oldPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(newPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(oldPath, "daemon.json"), []byte("legacy"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(oldPath, "settings.json"), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(newPath, "settings.json"), []byte("new"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := migrateConfigDir(oldPath, newPath); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Fatalf("legacy directory still exists: %v", err)
		}
		for name, want := range map[string]string{"daemon.json": "legacy", "settings.json": "new"} {
			data, err := os.ReadFile(filepath.Join(newPath, name))
			if err != nil || string(data) != want {
				t.Fatalf("%s = %q, %v", name, data, err)
			}
		}
	})
}
