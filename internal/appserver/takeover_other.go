//go:build !darwin

package appserver

import (
	"context"
	"errors"
)

func terminateThreadOwner(context.Context, string, string) error {
	return errors.New("taking over a thread is currently supported only on macOS")
}
