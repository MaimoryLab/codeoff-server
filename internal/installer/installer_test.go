package installer

import "testing"

func TestNewInstaller(t *testing.T) {
	if New(nil) == nil {
		t.Fatal("expected installer")
	}
}
