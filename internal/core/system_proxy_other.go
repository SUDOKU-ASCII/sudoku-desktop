//go:build !darwin && !linux && !windows

package core

import (
	"context"
	"errors"
)

var errSystemProxyUnsupported = errors.New("system proxy is not supported on this platform")

func platformCaptureSystemProxy(context.Context) (*systemProxySnapshot, error) {
	return nil, errSystemProxyUnsupported
}
func platformSetSystemProxy(context.Context, *systemProxySnapshot, int, func(string)) error {
	return errSystemProxyUnsupported
}
func platformRestoreSystemProxy(context.Context, *systemProxySnapshot, int, func(string)) error {
	return nil
}
func platformSystemProxyIsOurs(context.Context, int) bool { return false }
