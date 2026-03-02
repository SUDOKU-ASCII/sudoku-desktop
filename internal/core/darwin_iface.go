//go:build darwin

package core

import (
	"net"
	"strings"
)

func darwinInterfaceIPv4(ifName string) string {
	ifName = strings.TrimSpace(ifName)
	if ifName == "" {
		return ""
	}
	ifi, err := net.InterfaceByName(ifName)
	if err != nil || ifi == nil {
		return ""
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet == nil || ipNet.IP == nil {
			continue
		}
		ip4 := ipNet.IP.To4()
		if ip4 == nil || ip4.IsLoopback() {
			continue
		}
		return ip4.String()
	}
	return ""
}
