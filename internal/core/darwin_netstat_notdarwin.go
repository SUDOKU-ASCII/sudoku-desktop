//go:build !darwin

package core

import "errors"

func darwinNetstatRoutesIPv4() ([]darwinNetstatRoute, error) {
	return nil, errors.New("darwin only")
}
