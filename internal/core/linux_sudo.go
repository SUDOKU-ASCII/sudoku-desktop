//go:build linux

package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	linuxShPath = "/bin/sh"
)

type linuxSudoCache struct {
	mu       sync.RWMutex
	password []byte // sudo password; in-memory only.
}

var linuxSudo = &linuxSudoCache{}

func linuxAdminHasPassword() bool {
	if os.Geteuid() == 0 {
		return true
	}
	linuxSudo.mu.RLock()
	ok := len(linuxSudo.password) > 0
	linuxSudo.mu.RUnlock()
	return ok
}

func linuxAdminAcquire(password string) error {
	if os.Geteuid() == 0 {
		return nil
	}
	if password == "" {
		return fmt.Errorf("%w: empty password", ErrAdminRequired)
	}
	sudo, err := linuxSudoBin()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	out, err := linuxRunSudo(ctx, sudo, []byte(password+"\n"), "-S", "-k", "-p", "", "-v")
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			return fmt.Errorf("sudo validation failed: %w", err)
		}
		return fmt.Errorf("sudo validation failed: %s", msg)
	}

	pw := []byte(password)
	linuxSudo.mu.Lock()
	zeroBytes(linuxSudo.password)
	linuxSudo.password = pw
	linuxSudo.mu.Unlock()
	return nil
}

func linuxAdminForget() error {
	linuxSudo.mu.Lock()
	zeroBytes(linuxSudo.password)
	linuxSudo.password = nil
	linuxSudo.mu.Unlock()

	// Best-effort: drop sudo timestamp too.
	sudo, err := linuxSudoBin()
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, sudo, "-k").Run()
	return nil
}

func linuxAdminRunShLC(ctx context.Context, script string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	script = linuxAdminNormalizeScript(script)

	if os.Geteuid() == 0 {
		cmd := exec.CommandContext(ctx, linuxShPath, "-lc", script)
		output, err := cmd.CombinedOutput()
		return string(output), err
	}

	sudo, err := linuxSudoBin()
	if err != nil {
		return "", err
	}

	pw, ok := linuxAdminPasswordCopy()
	if !ok {
		return "", ErrAdminRequired
	}

	// Try sudo without prompting first (NOPASSWD / existing sudo timestamp).
	output, runErr := linuxRunSudo(ctx, sudo, nil, "-n", "--", linuxShPath, "-lc", script)
	if runErr == nil {
		return output, nil
	}

	stdin := make([]byte, 0, len(pw)+1)
	stdin = append(stdin, pw...)
	stdin = append(stdin, '\n')
	output2, err2 := linuxRunSudo(ctx, sudo, stdin, "-S", "-p", "", "--", linuxShPath, "-lc", script)
	if err2 == nil {
		return output2, nil
	}
	if strings.TrimSpace(output2) != "" {
		return output2, err2
	}
	return output, err2
}

func linuxAdminPasswordCopy() ([]byte, bool) {
	linuxSudo.mu.RLock()
	if len(linuxSudo.password) == 0 {
		linuxSudo.mu.RUnlock()
		return nil, false
	}
	pw := append([]byte(nil), linuxSudo.password...)
	linuxSudo.mu.RUnlock()
	return pw, true
}

func linuxSudoBin() (string, error) {
	// Prefer a stable absolute path; fall back to PATH resolution.
	if st, err := os.Stat("/usr/bin/sudo"); err == nil && st.Mode().IsRegular() {
		return "/usr/bin/sudo", nil
	}
	if p, err := exec.LookPath("sudo"); err == nil && strings.TrimSpace(p) != "" {
		return p, nil
	}
	return "", errors.New("sudo not found")
}

func linuxRunSudo(ctx context.Context, sudo string, stdin []byte, args ...string) (string, error) {
	if strings.TrimSpace(sudo) == "" {
		return "", errors.New("sudo not found")
	}
	cmd := exec.CommandContext(ctx, sudo, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func linuxAdminNormalizeScript(script string) string {
	script = strings.TrimSpace(script)
	if script == "" {
		return "true"
	}
	// Ensure system binaries are resolvable regardless of the desktop environment.
	// Don't force `set -e` here; callers decide whether commands are best-effort.
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
