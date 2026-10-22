//go:build !windows

package core

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func applyManagedProcessSysProcAttr(_ *exec.Cmd) {}

// attachChildLifetime is a no-op on unix: orphaned children are reaped by the guard process.
func attachChildLifetime(_ *exec.Cmd) {}

func terminateProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
}

// detachProcessSysProcAttr starts cmd in its own session so it outlives this process and
// ignores the terminal hang-up that follows a closed shell.
func detachProcessSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func killPID(pid int) {
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// processCommand returns the command line of pid ("" if gone), used to make sure a recorded
// pid still belongs to the process we started before signalling it.
func processCommand(pid int) string {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
