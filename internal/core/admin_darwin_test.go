//go:build darwin

package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDarwinAdminDetachedProcessUsesIndependentBackgroundProcess(t *testing.T) {
	oldRunner := darwinRunSudoCommand
	oldPassword, hadPassword := darwinAdminPasswordCopy()
	defer restoreDarwinSudoTestState(oldRunner, oldPassword, hadPassword)

	darwinSudo.mu.Lock()
	zeroBytes(darwinSudo.password)
	darwinSudo.password = []byte("test-password")
	darwinSudo.mu.Unlock()

	var scripts []string
	darwinRunSudoCommand = func(ctx context.Context, _ []byte, args ...string) (string, error) {
		script := args[len(args)-1]
		scripts = append(scripts, script)
		cmd := exec.CommandContext(ctx, darwinShPath, "-lc", script)
		output, err := cmd.CombinedOutput()
		return string(output), err
	}

	runtimeDir := t.TempDir()
	command := filepath.Join(runtimeDir, "fake-hev")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatalf("write fake HEV: %v", err)
	}

	proc := &darwinAdminDetachedProcess{}
	pidFile := filepath.Join(runtimeDir, "hev.pid")
	logFile := filepath.Join(runtimeDir, "hev.log")
	pid, err := proc.Start(command, nil, runtimeDir, pidFile, logFile)
	if err != nil {
		t.Fatalf("start detached process: %v", err)
	}
	defer func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}()

	if pid <= 0 || !pidLooksAlive(pid) {
		t.Fatalf("detached process is not alive: pid=%d", pid)
	}
	if len(scripts) != 1 {
		t.Fatalf("start executed %d privileged scripts, want 1", len(scripts))
	}
	if strings.Contains(scripts[0], "launchctl submit") {
		t.Fatalf("start script still uses launchctl submit:\n%s", scripts[0])
	}
	for _, required := range []string{"/usr/bin/nohup", "</dev/null", "2>&1 &"} {
		if !strings.Contains(scripts[0], required) {
			t.Fatalf("start script missing %q:\n%s", required, scripts[0])
		}
	}

	if err := proc.Stop(3 * time.Second); err != nil {
		t.Fatalf("stop detached process: %v", err)
	}
}
