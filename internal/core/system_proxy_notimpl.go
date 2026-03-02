//go:build !darwin && !windows && !linux

package core

func platformApplySystemProxy(_ systemProxyConfig) (func() error, error) {
	return nil, nil
}
