//go:build linux

package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const linuxAutostartDesktopFileName = "sudoku4x4.desktop"

func setLaunchAtLogin(enabled bool) error {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("resolve user config dir: %w", err)
	}
	autostartDir := filepath.Join(cfgDir, "autostart")
	desktopPath := filepath.Join(autostartDir, linuxAutostartDesktopFileName)

	if !enabled {
		if err := os.Remove(desktopPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove linux autostart entry: %w", err)
		}
		return nil
	}

	exe, err := currentExecutablePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(autostartDir, 0o755); err != nil {
		return fmt.Errorf("create linux autostart dir: %w", err)
	}

	// Desktop Entry spec supports double-quoted executable paths.
	execLine := desktopQuoteArg(exe) + " --autostart"
	content := strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Version=1.0",
		"Name=4x4 sudoku",
		"Comment=Start 4x4 sudoku on login",
		"Terminal=false",
		"X-GNOME-Autostart-enabled=true",
		"Exec=" + execLine,
		"",
	}, "\n")
	if err := os.WriteFile(desktopPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write linux autostart entry: %w", err)
	}
	return nil
}

func desktopQuoteArg(v string) string {
	// Quote for .desktop Exec= field. This is not a shell string; keep it simple and reversible.
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	return `"` + v + `"`
}
