//go:build !darwin

package core

import (
	"context"
	"fmt"
)

func darwinAdminHasPassword() bool { return false }

func darwinAdminAcquire(_ string) error {
	return fmt.Errorf("%w: darwin only", ErrAdminRequired)
}

func darwinAdminForget() error { return nil }

func darwinAdminRunShLC(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("%w: darwin only", ErrAdminRequired)
}
