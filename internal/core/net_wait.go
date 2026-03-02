package core

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

func waitForTCPReady(ctx context.Context, addr string, timeout time.Duration) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return fmt.Errorf("empty address")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := &net.Dialer{Timeout: 250 * time.Millisecond}
	var lastErr error
	for {
		conn, err := dialer.DialContext(waitCtx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		if waitCtx.Err() != nil {
			if lastErr != nil {
				return lastErr
			}
			return waitCtx.Err()
		}
		time.Sleep(60 * time.Millisecond)
	}
}
