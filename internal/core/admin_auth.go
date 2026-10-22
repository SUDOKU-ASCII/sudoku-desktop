package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// adminAuth holds the user's login password for sudo (macOS / Linux) so the elevated TUN
// helper can be launched from an in-app prompt instead of a system authorization dialog.
// The password lives in memory only and is zeroed when forgotten. Windows elevates through
// UAC and never uses it.
type adminAuth struct {
	mu       sync.RWMutex
	password []byte
}

var admin = &adminAuth{}

const sudoPath = "/usr/bin/sudo"

// adminUsesPassword reports whether elevation on this platform goes through sudo -S.
func adminUsesPassword() bool {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return false
	}
	_, err := os.Stat(sudoPath)
	return err == nil
}

// Has reports whether elevation can proceed without asking the user.
func (a *adminAuth) Has() bool {
	if !adminUsesPassword() {
		return true
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.password) > 0
}

// Acquire validates password with sudo and keeps it on success.
func (a *adminAuth) Acquire(password string) error {
	if !adminUsesPassword() {
		return nil
	}
	if password == "" {
		return fmt.Errorf("%w: empty password", ErrAdminRequired)
	}
	pw := []byte(password)
	stdin := append(append([]byte(nil), pw...), '\n')
	defer zeroBytes(stdin)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sudoPath, "-S", "-k", "-p", "", "-v")
	cmd.Stdin = bytes.NewReader(stdin)
	if out, err := cmd.CombinedOutput(); err != nil {
		zeroBytes(pw)
		if ctx.Err() != nil {
			return errAdminTimeout
		}
		msg := strings.ToLower(string(out))
		if strings.Contains(msg, "not in the sudoers") || strings.Contains(msg, "not allowed") {
			return errAdminNotSudoer
		}
		return errAdminBadPassword
	}
	a.mu.Lock()
	zeroBytes(a.password)
	a.password = pw
	a.mu.Unlock()
	return nil
}

// Forget drops the password and sudo's own timestamp.
func (a *adminAuth) Forget() {
	a.mu.Lock()
	zeroBytes(a.password)
	a.password = nil
	a.mu.Unlock()
	if adminUsesPassword() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, sudoPath, "-k").Run()
	}
}

// stdin returns "password\n" for sudo -S; the caller must zero it.
func (a *adminAuth) stdin() ([]byte, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.password) == 0 {
		return nil, false
	}
	return append(append(make([]byte, 0, len(a.password)+1), a.password...), '\n'), true
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// adminRequest is an action waiting for the in-app password prompt.
type adminRequest struct {
	start  *StartRequest // start, or switch the tunnel on in a running session
	repair bool          // repair network
	err    error         // why the previous attempt failed (wrong password), if it did
}

// requireAdmin parks req until TunAcquirePrivileges and raises NeedsAdmin so the UI asks
// for the password. It returns ErrAdminRequired for the caller to pass on.
func (b *Backend) requireAdmin(req adminRequest) error {
	b.adminMu.Lock()
	b.adminPending = &req
	b.adminMu.Unlock()
	msg := ""
	if errors.Is(req.err, errAdminBadPassword) {
		msg = req.err.Error()
	}
	b.update(func(s *RuntimeState) { s.NeedsAdmin, s.AdminError = true, msg })
	return ErrAdminRequired
}

func (b *Backend) takeAdminPending() *adminRequest {
	b.adminMu.Lock()
	defer b.adminMu.Unlock()
	p := b.adminPending
	b.adminPending = nil
	return p
}

// TunHasPrivileges reports whether the tunnel can be started without asking for the password.
func (b *Backend) TunHasPrivileges() bool { return admin.Has() }

// TunAcquirePrivileges checks the password with sudo, keeps it in memory and resumes the
// action that was waiting for it.
func (b *Backend) TunAcquirePrivileges(password string) error {
	if err := admin.Acquire(password); err != nil {
		return err
	}
	b.update(func(s *RuntimeState) { s.NeedsAdmin, s.AdminError = false, "" })
	p := b.takeAdminPending()
	if p == nil {
		return nil
	}
	go func() {
		var err error
		switch {
		case p.repair:
			err = b.RepairNetwork()
		case p.start != nil:
			err = b.StartProxy(*p.start)
		}
		if err != nil && !errors.Is(err, ErrAdminRequired) && !errors.Is(err, ErrStartCancelled) {
			b.addLog("error", "app", err.Error())
		}
	}()
	return nil
}

// TunCancelPrivileges dismisses a pending password prompt. A tunnel that was waiting for it
// is switched off; nothing else starts half-way.
func (b *Backend) TunCancelPrivileges() {
	p := b.takeAdminPending()
	b.update(func(s *RuntimeState) { s.NeedsAdmin, s.AdminError = false, "" })
	if p != nil && p.start != nil && p.start.WithTun {
		b.setTunEnabled(false)
	}
}

// TunDropPrivileges forgets the password.
func (b *Backend) TunDropPrivileges() { admin.Forget() }
