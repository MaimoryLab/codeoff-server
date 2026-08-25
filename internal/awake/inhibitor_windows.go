//go:build windows

package awake

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	powerRequestContextVersion      = 0
	powerRequestContextSimpleString = 1
	powerRequestSystemRequired      = 0
)

var (
	kernel32           = windows.NewLazySystemDLL("kernel32.dll")
	powerCreateRequest = kernel32.NewProc("PowerCreateRequest")
	powerSetRequest    = kernel32.NewProc("PowerSetRequest")
	powerClearRequest  = kernel32.NewProc("PowerClearRequest")
)

type reasonContext struct {
	Version uint32
	Flags   uint32
	Reason  uintptr
}

func acquire() (func() error, error) {
	reason, err := windows.UTF16PtrFromString("Codex Remote app-server is running")
	if err != nil {
		return nil, err
	}
	context := reasonContext{Version: powerRequestContextVersion, Flags: powerRequestContextSimpleString, Reason: uintptr(unsafe.Pointer(reason))}
	handle, _, callErr := powerCreateRequest.Call(uintptr(unsafe.Pointer(&context)))
	runtime.KeepAlive(reason)
	if handle == 0 || windows.Handle(handle) == windows.InvalidHandle {
		return nil, callError(callErr)
	}
	if result, _, callErr := powerSetRequest.Call(handle, powerRequestSystemRequired); result == 0 {
		_ = windows.CloseHandle(windows.Handle(handle))
		return nil, callError(callErr)
	}
	return func() error {
		result, _, callErr := powerClearRequest.Call(handle, powerRequestSystemRequired)
		closeErr := windows.CloseHandle(windows.Handle(handle))
		if result == 0 {
			return errors.Join(callError(callErr), closeErr)
		}
		return closeErr
	}, nil
}

func callError(err error) error {
	if err == windows.ERROR_SUCCESS {
		return errors.New("Windows power request failed")
	}
	return err
}
