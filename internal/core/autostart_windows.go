//go:build windows

package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	windowsRunKeyPath       = `Software\Microsoft\Windows\CurrentVersion\Run`
	windowsRunValueName     = "sudoku4x4"
	windowsAutostartArgLine = "--autostart"
)

func setLaunchAtLogin(enabled bool) error {
	exe, err := currentExecutablePath()
	if err != nil {
		return err
	}
	exe = filepath.Clean(exe)

	cmd := fmt.Sprintf("\"%s\" %s", exe, windowsAutostartArgLine)

	if enabled {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsRunKeyPath, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("open run key: %w", err)
		}
		defer key.Close()
		if err := key.SetStringValue(windowsRunValueName, cmd); err != nil {
			return fmt.Errorf("set autostart value: %w", err)
		}
		return nil
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, windowsRunKeyPath, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open run key: %w", err)
	}
	defer key.Close()
	if err := key.DeleteValue(windowsRunValueName); err != nil {
		// Be tolerant: some systems return a generic error string when the value is missing.
		if errors.Is(err, registry.ErrNotExist) || strings.Contains(strings.ToLower(err.Error()), "not found") {
			return nil
		}
		return fmt.Errorf("delete autostart value: %w", err)
	}
	return nil
}
