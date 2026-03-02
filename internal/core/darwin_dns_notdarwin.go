//go:build !darwin

package core

func darwinNetworkServiceForDevice(_ string) (string, error) { return "", nil }

func darwinGetDNSServers(_ string) ([]string, bool, error) { return nil, false, nil }
