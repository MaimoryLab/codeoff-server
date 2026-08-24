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
		starting  bool
		stopping  bool
		want      string
	}{{want: "未安装"}, {installed: true, want: "停止"}, {installed: true, running: true, want: "运行"}, {installed: true, starting: true, want: "启动中"}, {installed: true, stopping: true, want: "停止中"}} {
		if got := serviceStatus(test.installed, test.running, test.starting, test.stopping); got != test.want {
			t.Fatalf("service status = %q, want %q", got, test.want)
		}
	}
}

func TestToggleLabel(t *testing.T) {
	if toggleLabel(false) != "启动" || toggleLabel(true) != "停止" {
		t.Fatal("unexpected toggle labels")
	}
}
