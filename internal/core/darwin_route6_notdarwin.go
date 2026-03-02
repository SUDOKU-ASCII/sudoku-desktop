//go:build !darwin

package core

func darwinDefaultRouteIPv6() (gateway string, iface string, err error) {
	return "", "", nil
}
