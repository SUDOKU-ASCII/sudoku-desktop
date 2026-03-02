//go:build darwin

package core

import (
	"bufio"
	"errors"
	"os/exec"
	"strings"
)

func darwinDefaultRouteIPv6() (gateway string, iface string, err error) {
	cmd := exec.Command("route", "-n", "get", "-inet6", "default")
	output, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	s := bufio.NewScanner(strings.NewReader(string(output)))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "gateway:") {
			gateway = strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))
		}
		if strings.HasPrefix(line, "interface:") {
			iface = strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		}
	}
	if gateway == "" {
		return "", "", errors.New("ipv6 gateway not found")
	}
	return gateway, iface, nil
}
