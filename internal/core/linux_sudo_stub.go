//go:build !linux

package core

import (
	"context"
	"fmt"
)

func linuxAdminHasPassword() bool { return false }

func linuxAdminAcquire(_ string) error {
	return fmt.Errorf("%w: linux only", ErrAdminRequired)
}

func linuxAdminForget() error { return nil }

func linuxAdminRunShLC(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("%w: linux only", ErrAdminRequired)
}
