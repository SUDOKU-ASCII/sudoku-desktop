//go:build !darwin && !windows && !linux

package core

import (
	"fmt"
	"runtime"
)

func setLaunchAtLogin(enabled bool) error {
	_ = enabled
	return fmt.Errorf("launch at login is not supported on %s", runtime.GOOS)
}
