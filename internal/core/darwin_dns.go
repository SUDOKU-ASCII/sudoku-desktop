//go:build darwin

package core

import (
	"errors"
	"os/exec"
	"regexp"
	"strings"
)

func darwinNetworkServiceForDevice(device string) (string, error) {
	device = strings.TrimSpace(device)
	if device == "" {
		return "", errors.New("empty device")
	}
	out, err := exec.Command("networksetup", "-listnetworkserviceorder").CombinedOutput()
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(out), "\n")
	serviceRe := regexp.MustCompile(`^\(\d+\)\s+(.+)\s*$`)
	deviceRe := regexp.MustCompile(`Device:\s*([^,)]+)`)

	var currentService string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if m := serviceRe.FindStringSubmatch(line); len(m) == 2 {
			currentService = strings.TrimSpace(m[1])
			continue
		}
		if currentService == "" {
			continue
		}
		if strings.Contains(line, "Device:") {
			m := deviceRe.FindStringSubmatch(line)
			if len(m) != 2 {
				continue
			}
			dev := strings.TrimSpace(m[1])
			if dev == device {
				return currentService, nil
			}
		}
	}
	return "", errors.New("network service not found for device " + device)
}

func darwinGetDNSServers(service string) (servers []string, wasAutomatic bool, err error) {
	service = strings.TrimSpace(service)
	if service == "" {
		return nil, false, errors.New("empty service")
	}
	out, err := exec.Command("networksetup", "-getdnsservers", service).CombinedOutput()
	if err != nil {
		return nil, false, err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil, false, errors.New("empty dns output")
	}
	if strings.Contains(s, "There aren't any DNS Servers set") || strings.Contains(s, "There are no DNS Servers set") {
		return nil, true, nil
	}
	lines := strings.Split(s, "\n")
	servers = make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		servers = append(servers, l)
	}
	if len(servers) == 0 {
		return nil, true, nil
	}
	return servers, false, nil
}
