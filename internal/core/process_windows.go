//go:build windows

package core

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func applyManagedProcessSysProcAttr(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
}
