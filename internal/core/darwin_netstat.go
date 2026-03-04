//go:build darwin

package core

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func darwinNetstatRoutesIPv4() ([]darwinNetstatRoute, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "netstat", "-rn", "-f", "inet")
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, errors.New("netstat -rn -f inet: timeout")
	}
	clean := strings.TrimSpace(string(out))
	if err != nil {
		if clean != "" {
			return nil, fmt.Errorf("netstat -rn -f inet: %w: %s", err, clean)
		}
		return nil, fmt.Errorf("netstat -rn -f inet: %w", err)
	}
	return parseDarwinNetstatRoutes(clean), nil
}
