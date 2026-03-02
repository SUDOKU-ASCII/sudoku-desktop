//go:build darwin

package core

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type darwinProxySnapshot struct {
	Service string

	AutoProxyState bool
	AutoProxyURL   string

	WebEnabled    bool
	WebServer     string
	WebPort       int
	SecureEnabled bool
	SecureServer  string
	SecurePort    int
	SOCKSEnabled  bool
	SOCKSServer   string
	SOCKSPort     int

	BypassDomains []string
}

func platformApplySystemProxy(cfg systemProxyConfig) (func() error, error) {
	_, ifName, err := darwinDefaultRoute()
	if err != nil {
		return nil, err
	}
	ifName = strings.TrimSpace(ifName)
	if ifName == "" {
		return nil, errors.New("default interface not found")
	}
	svc, err := darwinNetworkServiceForDevice(ifName)
	if err != nil {
		return nil, err
	}
	svc = strings.TrimSpace(svc)
	if svc == "" {
		return nil, errors.New("network service not found")
	}

	snap, err := darwinReadProxySnapshot(svc)
	if err != nil {
		return nil, err
	}
	logf := cfg.Logf

	applyErr := func() error {
		if cfg.ProxyMode == "pac" && strings.TrimSpace(cfg.PACURL) != "" {
			if err := darwinNS(logf, "networksetup", "-setautoproxyurl", svc, strings.TrimSpace(cfg.PACURL)); err != nil {
				return err
			}
			_ = darwinNS(logf, "networksetup", "-setautoproxystate", svc, "on")
			_ = darwinNS(logf, "networksetup", "-setwebproxystate", svc, "off")
			_ = darwinNS(logf, "networksetup", "-setsecurewebproxystate", svc, "off")
			_ = darwinNS(logf, "networksetup", "-setsocksfirewallproxystate", svc, "off")
			return nil
		}

		if cfg.LocalPort <= 0 || cfg.LocalPort > 65535 {
			return fmt.Errorf("invalid local port: %d", cfg.LocalPort)
		}

		_ = darwinNS(logf, "networksetup", "-setautoproxystate", svc, "off")

		portStr := strconv.Itoa(cfg.LocalPort)
		_ = darwinNS(logf, "networksetup", "-setwebproxy", svc, "127.0.0.1", portStr)
		_ = darwinNS(logf, "networksetup", "-setsecurewebproxy", svc, "127.0.0.1", portStr)
		_ = darwinNS(logf, "networksetup", "-setsocksfirewallproxy", svc, "127.0.0.1", portStr)
		_ = darwinNS(logf, "networksetup", "-setwebproxystate", svc, "on")
		_ = darwinNS(logf, "networksetup", "-setsecurewebproxystate", svc, "on")
		_ = darwinNS(logf, "networksetup", "-setsocksfirewallproxystate", svc, "on")

		needBypass := []string{"localhost", "127.0.0.1", "::1"}
		merged := uniqueStrings(append(append([]string(nil), snap.BypassDomains...), needBypass...))
		if len(merged) == 0 {
			_ = darwinNS(logf, "networksetup", "-setproxybypassdomains", svc, "Empty")
		} else {
			args := append([]string{"-setproxybypassdomains", svc}, merged...)
			_ = darwinNS(logf, "networksetup", args...)
		}
		return nil
	}()
	if applyErr != nil {
		return nil, applyErr
	}

	return func() error {
		return darwinRestoreProxySnapshot(snap, logf)
	}, nil
}

func darwinNS(logf func(string), name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	clean := strings.TrimSpace(string(out))
	if logf != nil {
		if clean != "" {
			logf(fmt.Sprintf("[system-proxy] %s %s => %s", name, strings.Join(args, " "), clean))
		} else {
			logf(fmt.Sprintf("[system-proxy] %s %s", name, strings.Join(args, " ")))
		}
	}
	if err != nil {
		if clean != "" {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, clean)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func darwinReadProxySnapshot(service string) (darwinProxySnapshot, error) {
	service = strings.TrimSpace(service)
	if service == "" {
		return darwinProxySnapshot{}, errors.New("empty service")
	}
	snap := darwinProxySnapshot{Service: service}

	if st, url, err := darwinGetAutoProxy(service); err == nil {
		snap.AutoProxyState = st
		snap.AutoProxyURL = url
	}
	if p, err := darwinGetProxy(service, "-getwebproxy"); err == nil {
		snap.WebEnabled, snap.WebServer, snap.WebPort = p.Enabled, p.Server, p.Port
	}
	if p, err := darwinGetProxy(service, "-getsecurewebproxy"); err == nil {
		snap.SecureEnabled, snap.SecureServer, snap.SecurePort = p.Enabled, p.Server, p.Port
	}
	if p, err := darwinGetProxy(service, "-getsocksfirewallproxy"); err == nil {
		snap.SOCKSEnabled, snap.SOCKSServer, snap.SOCKSPort = p.Enabled, p.Server, p.Port
	}
	if domains, err := darwinGetBypassDomains(service); err == nil {
		snap.BypassDomains = domains
	}
	return snap, nil
}

type darwinProxy struct {
	Enabled bool
	Server  string
	Port    int
}

func darwinGetProxy(service string, getCmd string) (darwinProxy, error) {
	out, err := exec.Command("networksetup", getCmd, service).CombinedOutput()
	if err != nil {
		return darwinProxy{}, err
	}
	lines := strings.Split(string(out), "\n")
	p := darwinProxy{}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "Enabled:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "Enabled:"))
			p.Enabled = strings.EqualFold(v, "yes") || strings.EqualFold(v, "on") || v == "1"
		}
		if strings.HasPrefix(line, "Server:") {
			p.Server = strings.TrimSpace(strings.TrimPrefix(line, "Server:"))
		}
		if strings.HasPrefix(line, "Port:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "Port:"))
			if n, _ := strconv.Atoi(v); n > 0 {
				p.Port = n
			}
		}
	}
	return p, nil
}

func darwinGetAutoProxy(service string) (state bool, url string, err error) {
	out, err := exec.Command("networksetup", "-getautoproxystate", service).CombinedOutput()
	if err != nil {
		return false, "", err
	}
	s := strings.TrimSpace(string(out))
	state = strings.Contains(strings.ToLower(s), "on")

	out2, err := exec.Command("networksetup", "-getautoproxyurl", service).CombinedOutput()
	if err != nil {
		return state, "", nil
	}
	lines := strings.Split(string(out2), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "URL:") {
			url = strings.TrimSpace(strings.TrimPrefix(line, "URL:"))
		}
		if strings.HasPrefix(line, "Enabled:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "Enabled:"))
			state = state && (strings.EqualFold(v, "yes") || strings.EqualFold(v, "on") || v == "1")
		}
	}
	return state, url, nil
}

func darwinGetBypassDomains(service string) ([]string, error) {
	out, err := exec.Command("networksetup", "-getproxybypassdomains", service).CombinedOutput()
	if err != nil {
		return nil, err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil, nil
	}
	if strings.Contains(strings.ToLower(s), "there aren't any") || strings.Contains(strings.ToLower(s), "there are no") {
		return nil, nil
	}
	lines := strings.Split(s, "\n")
	outDomains := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		outDomains = append(outDomains, ln)
	}
	return uniqueStrings(outDomains), nil
}

func darwinRestoreProxySnapshot(snap darwinProxySnapshot, logf func(string)) error {
	svc := strings.TrimSpace(snap.Service)
	if svc == "" {
		return nil
	}

	// Restore auto proxy first.
	if strings.TrimSpace(snap.AutoProxyURL) != "" {
		_ = darwinNS(logf, "networksetup", "-setautoproxyurl", svc, strings.TrimSpace(snap.AutoProxyURL))
	}
	_ = darwinNS(logf, "networksetup", "-setautoproxystate", svc, map[bool]string{true: "on", false: "off"}[snap.AutoProxyState])

	// Restore manual proxies.
	restoreOne := func(kind string, enabled bool, server string, port int) {
		server = strings.TrimSpace(server)
		if server != "" && port > 0 && port <= 65535 {
			_ = darwinNS(logf, "networksetup", "-set"+kind, svc, server, strconv.Itoa(port))
		}
		_ = darwinNS(logf, "networksetup", "-set"+kind+"state", svc, map[bool]string{true: "on", false: "off"}[enabled])
	}
	restoreOne("webproxy", snap.WebEnabled, snap.WebServer, snap.WebPort)
	restoreOne("securewebproxy", snap.SecureEnabled, snap.SecureServer, snap.SecurePort)
	restoreOne("socksfirewallproxy", snap.SOCKSEnabled, snap.SOCKSServer, snap.SOCKSPort)

	// Restore bypass domains.
	if len(snap.BypassDomains) == 0 {
		_ = darwinNS(logf, "networksetup", "-setproxybypassdomains", svc, "Empty")
	} else {
		args := append([]string{"-setproxybypassdomains", svc}, snap.BypassDomains...)
		_ = darwinNS(logf, "networksetup", args...)
	}

	return nil
}
