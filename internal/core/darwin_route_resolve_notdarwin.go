//go:build !darwin

package core

import (
	"errors"
	"time"
)

func darwinIsTunLikeInterface(_ string) bool { return false }

func darwinPickPhysicalDefaultRouteIPv4(_ []darwinNetstatRoute) (gateway string, iface string) {
	return "", ""
}

func darwinHasUnscopedDefaultRouteIPv4(_ []darwinNetstatRoute, _ string) bool {
	return false
}

func darwinHasDefaultRouteNotOnInterface(_ []darwinNetstatRoute, _ string) bool {
	return false
}

func darwinDHCPRouterForInterface(_ string) (string, error) {
	return "", errors.New("darwin only")
}

func darwinResolveOutboundBypassInterface(_ time.Duration) (string, error) {
	return "", errors.New("darwin only")
}

func darwinResolveRestoreGatewayIPv4(_ *routeContext, _ string) (string, error) {
	return "", errors.New("darwin only")
}

func darwinWaitDefaultRouteNotOnTun(_ string, _ time.Duration) error {
	return errors.New("darwin only")
}
