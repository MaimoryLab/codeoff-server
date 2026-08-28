package main

import (
	"net"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	loaded, err := loadSettings(path)
	if err != nil || loaded.ListenAddr != defaultListenAddr {
		t.Fatalf("default settings = %#v, %v", loaded, err)
	}
	want := settings{ListenAddr: "0.0.0.0:12000", AppServerEnabled: true, TunnelEnabled: true, PreventSleep: true, CodexEnvironment: []string{"CODEX_HOME=/tmp/codex"}}
	if err := saveSettings(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("saved settings = %#v, %v", got, err)
	}
	if _, err := validateListenAddr("localhost:11037"); err == nil {
		t.Fatal("hostname should be rejected")
	}
	if got, err := validateTunnelURL(" https://remote.example.com/ "); err != nil || got != "https://remote.example.com" {
		t.Fatalf("tunnel URL = %q, %v", got, err)
	}
	if _, err := validateTunnelURL("https://remote.example.com/app"); err == nil {
		t.Fatal("tunnel URL path should be rejected")
	}
}

func TestValidateEnvironment(t *testing.T) {
	if err := validateEnvironment([]string{"CODEX_HOME=/tmp/codex", "EMPTY="}); err != nil {
		t.Fatal(err)
	}
	for _, environment := range [][]string{{"INVALID"}, {"1INVALID=value"}, {"DUP=1", "DUP=2"}} {
		if err := validateEnvironment(environment); err == nil {
			t.Fatalf("environment %q should be rejected", environment)
		}
	}
}

func TestControlAddresses(t *testing.T) {
	if got := controlAddresses("127.0.0.1:11037"); !slices.Equal(got, []string{"http://127.0.0.1:11037"}) {
		t.Fatalf("control addresses = %v", got)
	}

	want := make([]string, 0)
	for _, address := range must(net.InterfaceAddrs()) {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil {
			want = append(want, "http://"+net.JoinHostPort(ip.String(), "11037"))
		}
	}
	slices.Sort(want)
	want = slices.Compact(want)
	if got := controlAddresses("0.0.0.0:11037"); !slices.Equal(got, want) {
		t.Fatalf("control addresses = %v, want %v", got, want)
	}
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
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
