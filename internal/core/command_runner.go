package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	linuxResolvConfBackupPath = "/run/sudoku4x4-resolv.conf.bak"
	darwinPFAnchor            = "com.apple/sudoku4x4.tun"
	windowsDNSBackupFile      = "sudoku4x4-dns.json"
)

// runShellBatch runs shell snippets as one `sh` invocation. Callers are already privileged
// (the TUN helper runs as root), so there is no elevation layer here.
func runShellBatch(logf func(string), cmdlines ...string) error {
	if len(cmdlines) == 0 {
		return nil
	}
	script := "set -e; PATH=/usr/sbin:/sbin:/usr/bin:/bin:$PATH; " + strings.Join(cmdlines, "; ")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	output, err := cmd.CombinedOutput()
	clean := strings.TrimSpace(string(output))
	if logf != nil {
		if clean == "" {
			logf("[route] batch ok")
		} else {
			logf("[route] batch => " + clean)
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("route batch: timeout")
	}
	if err != nil {
		if clean == "" {
			return fmt.Errorf("route batch: %w", err)
		}
		return fmt.Errorf("route batch: %w: %s", err, clean)
	}
	return nil
}

// runPowerShellScript runs a script file under Windows PowerShell (already elevated).
func runPowerShellScript(parent context.Context, logf func(string), body string, timeout time.Duration) error {
	f, err := os.CreateTemp("", "sudoku-route-*.ps1")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.WriteString("$ErrorActionPreference = 'Stop'\r\n" + body + "\r\n"); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if parent == nil {
		parent = context.Background()
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", path)
	applyManagedProcessSysProcAttr(cmd)
	output, err := cmd.CombinedOutput()
	clean := strings.TrimSpace(string(output))
	if logf != nil {
		if clean == "" {
			logf("[route] powershell ok")
		} else {
			logf("[route] powershell => " + clean)
		}
	}
	if ctx.Err() != nil {
		return fmt.Errorf("powershell: %w", ctx.Err())
	}
	if err != nil {
		if clean == "" {
			return fmt.Errorf("powershell: %w", err)
		}
		return fmt.Errorf("powershell: %w: %s", err, clean)
	}
	return nil
}

func shellJoin(args ...string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$&;|<>*?()[]{}!") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
