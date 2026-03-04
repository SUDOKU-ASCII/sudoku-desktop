//go:build !darwin

package core

import "errors"

func darwinProcessArgs(_ int) (string, error) {
	return "", errors.New("darwin only")
}
