//go:build darwin

package core

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func darwinProcessArgs(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid: %d", pid)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", "-p", fmt.Sprintf("%d", pid), "-o", "args=")
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("ps timeout (pid=%d)", pid)
	}
	return strings.TrimSpace(string(output)), err
}
