//go:build !darwin && !linux && !windows

package awake

import "errors"

func acquire() (func() error, error) { return nil, errors.New("preventing sleep is unsupported") }
