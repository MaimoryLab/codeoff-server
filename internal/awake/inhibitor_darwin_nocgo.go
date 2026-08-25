//go:build darwin && !cgo

package awake

import "errors"

func acquire() (func() error, error) {
	return nil, errors.New("preventing sleep on macOS requires cgo")
}
