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

func darwinPrimaryNetworkInfo() (darwinPrimaryRouteInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "scutil", "--nwi")
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return darwinPrimaryRouteInfo{}, errors.New("scutil --nwi: timeout")
	}
	clean := strings.TrimSpace(string(out))
	if err != nil {
		if clean != "" {
			return darwinPrimaryRouteInfo{}, fmt.Errorf("scutil --nwi: %w: %s", err, clean)
		}
		return darwinPrimaryRouteInfo{}, fmt.Errorf("scutil --nwi: %w", err)
	}
	return parseDarwinScutilNWIOutput(clean), nil
}
