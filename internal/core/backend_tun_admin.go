package core

import (
	"fmt"
	"runtime"
)

// TunHasPrivileges reports whether the app can perform privileged TUN operations
// silently on the current platform.
func (b *Backend) TunHasPrivileges() bool {
	if b == nil {
		return false
	}
	if runtime.GOOS != "darwin" {
		// Other platforms use different escalation mechanisms.
		return true
	}
	return darwinAdminHasPassword()
}

// TunAcquirePrivileges validates the provided macOS login password for sudo and
// caches it in-memory for subsequent TUN route/DNS/PF operations.
//
// The password is never written to disk.
func (b *Backend) TunAcquirePrivileges(password string) error {
	if b == nil {
		return nil
	}
	if runtime.GOOS != "darwin" {
		return nil
	}
	if err := darwinAdminAcquire(password); err != nil {
		return err
	}
	b.mu.Lock()
	// Clear the "needs admin" indicator if it was set by a previous failed operation.
	b.state.NeedsAdmin = false
	b.emitStateLocked()
	b.mu.Unlock()
	b.addLog("info", "tun", "administrator privileges acquired for TUN operations (macOS)")
	return nil
}

// TunDropPrivileges clears any cached admin credentials used for TUN operations.
func (b *Backend) TunDropPrivileges() error {
	if b == nil {
		return nil
	}
	if runtime.GOOS != "darwin" {
		return nil
	}
	if err := darwinAdminForget(); err != nil {
		return fmt.Errorf("drop privileges: %w", err)
	}
	b.addLog("info", "tun", "administrator privileges cleared for TUN operations (macOS)")
	return nil
}
