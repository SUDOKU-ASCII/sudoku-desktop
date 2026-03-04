//go:build darwin

package core

import (
	"path/filepath"
)

func configureAdminDetachedProcess(proc adminDetachedProcess, store *Store, cfg *AppConfig) {
	if proc == nil || store == nil || cfg == nil {
		return
	}
	// darwinAdminDetachedProcess is darwin-only (file suffix _darwin.go).
	// Keep the type assertion out of cross-platform files to avoid CI build failures.
	p, ok := proc.(*darwinAdminDetachedProcess)
	if !ok || p == nil {
		return
	}
	p.pidFile = filepath.Join(store.RuntimeDir(), "hev.pid")
	p.logFile = filepath.Join(store.LogDir(), "hev.log")
	p.expectRuntimeDir = store.RuntimeDir()
	p.expectCmdBase = filepath.Base(cfg.Core.HevBinary)
}
