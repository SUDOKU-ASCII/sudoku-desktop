package core

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// effectiveRuntimeConfig returns the per-start config that should actually be
// written to runtime files. We keep user config intact and only enforce runtime
// constraints that the TUN dataplane depends on.
func effectiveRuntimeConfig(cfg *AppConfig, withTun bool) (*AppConfig, []string, error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("nil app config")
	}

	effective := *cfg
	effective.Routing = cfg.Routing
	effective.Tun = cfg.Tun
	effective.Core = cfg.Core
	effective.ReverseClient = cfg.ReverseClient
	effective.ReverseForward = cfg.ReverseForward
	effective.PortForwards = append([]PortForwardRule(nil), cfg.PortForwards...)
	effective.Nodes = append([]NodeConfig(nil), cfg.Nodes...)

	if !withTun {
		return &effective, nil, nil
	}

	proxyMode := strings.ToLower(strings.TrimSpace(effective.Routing.ProxyMode))
	if proxyMode != "pac" {
		return &effective, nil, nil
	}

	mapDNSAddr := strings.TrimSpace(effective.Tun.MapDNSAddress)
	if mapDNSAddr == "" || effective.Tun.MapDNSPort <= 0 {
		return nil, nil, fmt.Errorf("tun pac mode requires a valid MapDNS address and port")
	}
	if effective.Tun.MapDNSEnabled {
		return &effective, nil, nil
	}

	effective.Tun.MapDNSEnabled = true
	warnings := []string{
		fmt.Sprintf(
			"TUN PAC mode requires HEV MapDNS for correct domain routing; auto-enabled %s",
			net.JoinHostPort(mapDNSAddr, strconv.Itoa(effective.Tun.MapDNSPort)),
		),
	}
	return &effective, warnings, nil
}
