//go:build linux

package awake

import (
	"os"

	"github.com/godbus/dbus/v5"
)

func acquire() (func() error, error) {
	connection, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}
	var fd dbus.UnixFD
	err = connection.Object("org.freedesktop.login1", "/org/freedesktop/login1").
		Call("org.freedesktop.login1.Manager.Inhibit", 0, "sleep", "Codex Remote", "app-server is running", "block").
		Store(&fd)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "logind-inhibitor")
	return file.Close, nil
}
