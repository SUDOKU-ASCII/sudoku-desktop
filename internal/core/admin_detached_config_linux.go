//go:build linux

package core

import (
	"path/filepath"
	"strings"
)

func configureAdminDetachedProcess(proc adminDetachedProcess, store *Store, cfg *AppConfig) {
	if proc == nil || store == nil || cfg == nil {
		return
	}
	p, ok := proc.(*linuxAdminDetachedProcess)
	if !ok || p == nil {
		return
	}
	p.pidFile = filepath.Join(store.RuntimeDir(), "hev.pid")
	p.logFile = filepath.Join(store.LogDir(), "hev.log")
	p.expectRuntimeDir = store.RuntimeDir()
	if strings.TrimSpace(cfg.Core.HevBinary) != "" {
		p.expectCmdBase = filepath.Base(cfg.Core.HevBinary)
	} else {
		p.expectCmdBase = ""
	}
}
