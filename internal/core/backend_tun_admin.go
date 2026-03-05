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
	switch runtime.GOOS {
	case "darwin":
		return darwinAdminHasPassword()
	case "linux":
		return linuxAdminHasPassword()
	default:
		return true
	}
}

// TunAcquirePrivileges validates the provided system password for sudo and caches
// it in-memory for subsequent privileged TUN route/DNS operations.
//
// The password is never written to disk.
func (b *Backend) TunAcquirePrivileges(password string) error {
	if b == nil {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		if err := darwinAdminAcquire(password); err != nil {
			return err
		}
	case "linux":
		if err := linuxAdminAcquire(password); err != nil {
			return err
		}
	default:
		return nil
	}
	b.mu.Lock()
	// Clear the "needs admin" indicator if it was set by a previous failed operation.
	b.state.NeedsAdmin = false
	b.emitStateLocked()
	b.mu.Unlock()
	if runtime.GOOS == "linux" {
		b.addLog("info", "tun", "administrator privileges acquired for TUN operations (linux)")
	} else if runtime.GOOS == "darwin" {
		b.addLog("info", "tun", "administrator privileges acquired for TUN operations (macOS)")
	}
	return nil
}

// TunDropPrivileges clears any cached admin credentials used for TUN operations.
func (b *Backend) TunDropPrivileges() error {
	if b == nil {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		if err := darwinAdminForget(); err != nil {
			return fmt.Errorf("drop privileges: %w", err)
		}
		b.addLog("info", "tun", "administrator privileges cleared for TUN operations (macOS)")
	case "linux":
		if err := linuxAdminForget(); err != nil {
			return fmt.Errorf("drop privileges: %w", err)
		}
		b.addLog("info", "tun", "administrator privileges cleared for TUN operations (linux)")
	default:
		return nil
	}
	return nil
}
