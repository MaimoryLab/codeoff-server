package main

import (
	"path/filepath"
	"testing"
)

func TestListenAddrSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	address, err := loadListenAddr(path)
	if err != nil || address != defaultListenAddr {
		t.Fatalf("default address = %q, %v", address, err)
	}
	if err := saveListenAddr(path, "0.0.0.0:12000"); err != nil {
		t.Fatal(err)
	}
	address, err = loadListenAddr(path)
	if err != nil || address != "0.0.0.0:12000" {
		t.Fatalf("saved address = %q, %v", address, err)
	}
	if _, err := validateListenAddr("localhost:11037"); err == nil {
		t.Fatal("hostname should be rejected")
	}
}
