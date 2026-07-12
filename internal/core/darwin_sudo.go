//go:build darwin

package core

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	darwinSudoPath = "/usr/bin/sudo"
	darwinShPath   = "/bin/sh"
)

type darwinSudoCache struct {
	mu       sync.RWMutex
	password []byte // macOS login password for sudo; in-memory only.
}

var darwinSudo = &darwinSudoCache{}
var darwinRunSudoCommand = darwinRunSudo

func darwinAdminHasPassword() bool {
	if os.Geteuid() == 0 {
		return true
	}
	darwinSudo.mu.RLock()
	ok := len(darwinSudo.password) > 0
	darwinSudo.mu.RUnlock()
	return ok
}

func darwinAdminAcquire(password string) error {
	// Note: This is the user's macOS login password (for sudo), not the root account password.
	if os.Geteuid() == 0 {
		return nil
	}
	if password == "" {
		return fmt.Errorf("%w: empty password", ErrAdminRequired)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	out, err := darwinRunSudoCommand(ctx, []byte(password+"\n"), "-S", "-k", "-p", "", "-v")
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			return fmt.Errorf("sudo validation failed: %w", err)
		}
		return fmt.Errorf("sudo validation failed: %s", msg)
	}

	pw := []byte(password)
	darwinSudo.mu.Lock()
	zeroBytes(darwinSudo.password)
	darwinSudo.password = pw
	darwinSudo.mu.Unlock()
	return nil
}

func darwinAdminForget() error {
	darwinSudo.mu.Lock()
	zeroBytes(darwinSudo.password)
	darwinSudo.password = nil
	darwinSudo.mu.Unlock()

	// Best-effort: drop sudo timestamp too.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, darwinSudoPath, "-k").Run()
	return nil
}

func darwinAdminRunShLC(ctx context.Context, script string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	script = darwinAdminNormalizeScript(script)

	if os.Geteuid() == 0 {
		cmd := exec.CommandContext(ctx, darwinShPath, "-lc", script)
		output, err := cmd.CombinedOutput()
		return string(output), err
	}

	pw, ok := darwinAdminPasswordCopy()
	if !ok {
		return "", ErrAdminRequired
	}
	defer zeroBytes(pw)

	stdin := make([]byte, 0, len(pw)+1)
	stdin = append(stdin, pw...)
	stdin = append(stdin, '\n')
	defer zeroBytes(stdin)

	// Execute the privileged script exactly once. A failed `sudo -n` attempt cannot
	// be distinguished safely from a script failure, and retrying may apply route or
	// DNS mutations twice.
	return darwinRunSudoCommand(ctx, stdin, "-S", "-p", "", "--", darwinShPath, "-lc", script)
}

func darwinAdminPasswordCopy() ([]byte, bool) {
	darwinSudo.mu.RLock()
	if len(darwinSudo.password) == 0 {
		darwinSudo.mu.RUnlock()
		return nil, false
	}
	pw := append([]byte(nil), darwinSudo.password...)
	darwinSudo.mu.RUnlock()
	return pw, true
}

func darwinRunSudo(ctx context.Context, stdin []byte, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, darwinSudoPath, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func darwinAdminNormalizeScript(script string) string {
	script = strings.TrimSpace(script)
	if script == "" {
		return "true"
	}
	// Ensure system binaries (route/pfctl/networksetup/ifconfig/launchctl...) are resolvable.
	// Don't use `set -e` here; callers decide whether commands are best-effort.
	const pathPrefix = `export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin";`
	if strings.HasPrefix(script, "export PATH=") {
		return script
	}
	return pathPrefix + " " + script
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
