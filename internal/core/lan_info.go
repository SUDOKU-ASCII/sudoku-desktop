package core

import (
	"net"
	"sort"
	"strings"
)

func localLANIPv4s() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return []string{}
	}

	seen := make(map[string]struct{}, 8)
	ips := make([]string, 0, 4)
	fallback := make([]string, 0, 2)

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(iface.Name))
		if strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "utun") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			default:
				continue
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			text := ip.String()
			if _, ok := seen[text]; ok {
				continue
			}
			seen[text] = struct{}{}
			if ip.IsPrivate() {
				ips = append(ips, text)
				continue
			}
			// Keep non-private IPv4 as fallback (some LANs use uncommon address plans).
			fallback = append(fallback, text)
		}
	}

	sort.Strings(ips)
	if len(ips) > 0 {
		return ips
	}
	sort.Strings(fallback)
	return fallback
}
