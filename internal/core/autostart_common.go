package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func currentExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return "", errors.New("resolve executable path: empty")
	}

	// Best-effort cleanup. We don't hard-fail on these steps as they can be OS/filesystem dependent.
	if abs, aerr := filepath.Abs(exe); aerr == nil && strings.TrimSpace(abs) != "" {
		exe = abs
	}
	if real, rerr := filepath.EvalSymlinks(exe); rerr == nil && strings.TrimSpace(real) != "" {
		exe = real
	}
	return exe, nil
}
