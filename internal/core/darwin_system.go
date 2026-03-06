//go:build darwin

package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

func darwinProcessArgs(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid: %d", pid)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", "-p", fmt.Sprintf("%d", pid), "-o", "args=")
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("ps timeout (pid=%d)", pid)
	}
	return strings.TrimSpace(string(output)), err
}

func darwinNetstatRoutesIPv4() ([]darwinNetstatRoute, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "netstat", "-rn", "-f", "inet")
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, errors.New("netstat -rn -f inet: timeout")
	}
	clean := strings.TrimSpace(string(out))
	if err != nil {
		if clean != "" {
			return nil, fmt.Errorf("netstat -rn -f inet: %w: %s", err, clean)
		}
		return nil, fmt.Errorf("netstat -rn -f inet: %w", err)
	}
	return parseDarwinNetstatRoutes(clean), nil
}

func darwinPrimaryNetworkInfo() (darwinPrimaryRouteInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "scutil", "--nwi")
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return darwinPrimaryRouteInfo{}, errors.New("scutil --nwi: timeout")
	}
	clean := strings.TrimSpace(string(out))
	if err != nil {
		if clean != "" {
			return darwinPrimaryRouteInfo{}, fmt.Errorf("scutil --nwi: %w: %s", err, clean)
		}
		return darwinPrimaryRouteInfo{}, fmt.Errorf("scutil --nwi: %w", err)
	}
	return parseDarwinScutilNWIOutput(clean), nil
}

func darwinListTunInterfaces() map[string]struct{} {
	out := map[string]struct{}{}
	ifs, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, it := range ifs {
		name := strings.TrimSpace(it.Name)
		if strings.HasPrefix(name, "utun") || strings.HasPrefix(name, "tun") {
			out[name] = struct{}{}
		}
	}
	return out
}

func darwinWaitNewTunInterface(before map[string]struct{}, timeout time.Duration) string {
	if before == nil {
		before = map[string]struct{}{}
	}
	deadline := time.Now().Add(timeout)
	for {
		after := darwinListTunInterfaces()
		for name := range after {
			if _, ok := before[name]; !ok {
				return name
			}
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(80 * time.Millisecond)
	}
}

func darwinFindTunInterfaceByIPv4(ip string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil || !addr.Is4() {
		return ""
	}
	ifs, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, it := range ifs {
		name := strings.TrimSpace(it.Name)
		if !(strings.HasPrefix(name, "utun") || strings.HasPrefix(name, "tun")) {
			continue
		}
		addrs, err := it.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			prefix, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			if prefix.Addr() == addr {
				return name
			}
		}
	}
	return ""
}

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
