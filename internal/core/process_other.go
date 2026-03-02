//go:build !windows

package core

import "os/exec"

func applyManagedProcessSysProcAttr(_ *exec.Cmd) {}
