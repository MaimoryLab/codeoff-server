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

func TestServiceStatus(t *testing.T) {
	for _, test := range []struct {
		installed bool
		running   bool
		want      string
	}{{false, false, "未安装"}, {true, false, "停止"}, {true, true, "运行"}} {
		if got := serviceStatus(test.installed, test.running); got != test.want {
			t.Fatalf("service status = %q, want %q", got, test.want)
		}
	}
}

func TestToggleLabel(t *testing.T) {
	if toggleLabel(false) != "启动" || toggleLabel(true) != "停止" {
		t.Fatal("unexpected toggle labels")
	}
}
