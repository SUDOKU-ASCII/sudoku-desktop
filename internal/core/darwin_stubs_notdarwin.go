//go:build !darwin

package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func configureAdminDetachedProcess(_ adminDetachedProcess, _ *Store, _ *AppConfig) {}

func newAdminDetachedProcess() adminDetachedProcess {
	return nil
}

func darwinNetworkServiceForDevice(_ string) (string, error) { return "", nil }

func darwinGetDNSServers(_ string) ([]string, bool, error) { return nil, false, nil }

func darwinInterfaceIPv4(_ string) string {
	return ""
}

func darwinListTunInterfaces() map[string]struct{} {
	return map[string]struct{}{}
}

func darwinWaitNewTunInterface(_ map[string]struct{}, _ time.Duration) string {
	return ""
}

func darwinFindTunInterfaceByIPv4(_ string) string {
	return ""
}

func darwinNetstatRoutesIPv4() ([]darwinNetstatRoute, error) {
	return nil, errors.New("darwin only")
}

func darwinPrimaryNetworkInfo() (darwinPrimaryRouteInfo, error) {
	return darwinPrimaryRouteInfo{}, errors.New("darwinPrimaryNetworkInfo is only supported on macOS")
}

func darwinProcessArgs(_ int) (string, error) {
	return "", errors.New("darwin only")
}

func darwinDefaultRouteIPv6() (gateway string, iface string, err error) {
	return "", "", nil
}

func darwinAdminHasPassword() bool { return false }

func darwinAdminAcquire(_ string) error {
	return fmt.Errorf("%w: darwin only", ErrAdminRequired)
}

func darwinAdminForget() error { return nil }

func darwinAdminRunShLC(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("%w: darwin only", ErrAdminRequired)
}

func darwinBuildPFSetCmd(anchor string, tunIfExpr string, defaultIf string, gw4 string, gw6 string, tunIPv4 string, bypassV4File string, bypassV6File string, blockQUIC bool, dnsProxyPort int) string {
	return ""
}

func darwinBuildPFRestoreCmd(anchor string) string {
	return ""
}

func darwinIsTunLikeInterface(_ string) bool { return false }

func darwinPickPhysicalDefaultRouteIPv4(_ []darwinNetstatRoute) (gateway string, iface string) {
	return "", ""
}

func darwinPickPhysicalDefaultInterface(_ []darwinNetstatRoute) string {
	return ""
}

func darwinHasUnscopedDefaultRouteIPv4(_ []darwinNetstatRoute, _ string) bool {
	return false
}

func darwinHasDefaultRouteNotOnInterface(_ []darwinNetstatRoute, _ string) bool {
	return false
}

func darwinHasDefaultRouteOnInterface(_ []darwinNetstatRoute, _ string) bool {
	return false
}

func darwinHasDefaultRouteOnTunLikeInterface(_ []darwinNetstatRoute) bool {
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
