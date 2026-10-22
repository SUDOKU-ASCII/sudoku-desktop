//go:build darwin

package core

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

func platformCaptureSystemProxy(ctx context.Context) (*systemProxySnapshot, error) {
	services, err := darwinNetworkServices(ctx)
	if err != nil {
		return nil, err
	}
	snaps := make([]darwinProxySnapshot, len(services))
	var wg sync.WaitGroup
	for i, svc := range services {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snaps[i] = darwinReadProxySnapshot(ctx, svc)
			// A leftover of an earlier crashed run is not "the user's setting".
			for _, p := range []*darwinProxy{&snaps[i].Web, &snaps[i].Secure, &snaps[i].SOCKS} {
				if isLocalProxy(p.Server, p.Port, ProxyPort) {
					*p = darwinProxy{}
				}
			}
		}()
	}
	wg.Wait()
	return &systemProxySnapshot{Darwin: snaps}, nil
}

func platformSetSystemProxy(ctx context.Context, base *systemProxySnapshot, port int, logf func(string)) error {
	if base == nil || len(base.Darwin) == 0 {
		return errors.New("no network service found")
	}
	portStr := strconv.Itoa(port)
	var wg sync.WaitGroup
	errs := make([]error, len(base.Darwin))
	for i, snap := range base.Darwin {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc := snap.Service
			steps := [][]string{
				{"-setautoproxystate", svc, "off"},
				{"-setwebproxy", svc, "127.0.0.1", portStr},
				{"-setsecurewebproxy", svc, "127.0.0.1", portStr},
				{"-setsocksfirewallproxy", svc, "127.0.0.1", portStr},
			}
			merged := uniqueStrings(append(append([]string(nil), snap.BypassDomains...), proxyBypassLocal...))
			steps = append(steps, append([]string{"-setproxybypassdomains", svc}, merged...))
			for _, args := range steps {
				if err := darwinNS(ctx, logf, args...); err != nil {
					errs[i] = err
					return
				}
			}
		}()
	}
	wg.Wait()
	var failed []string
	for i, err := range errs {
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", base.Darwin[i].Service, err))
		}
	}
	if len(failed) == len(errs) {
		return errors.New(strings.Join(failed, "; "))
	}
	if len(failed) > 0 && logf != nil {
		logf("system proxy partially applied: " + strings.Join(failed, "; "))
	}
	return nil
}

func platformRestoreSystemProxy(ctx context.Context, snap *systemProxySnapshot, port int, logf func(string)) error {
	var wg sync.WaitGroup
	for _, s := range snap.Darwin {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cur := darwinReadProxySnapshot(ctx, s.Service)
			ours := func(p darwinProxy) bool { return p.Enabled && isLocalProxy(p.Server, p.Port, port) }
			svc := s.Service
			set := func(on bool) string { return map[bool]string{true: "on", false: "off"}[on] }
			restore := func(kind string, now, orig darwinProxy) {
				if !ours(now) {
					return
				}
				if orig.Server != "" && orig.Port > 0 && orig.Port <= 65535 {
					_ = darwinNS(ctx, logf, "-set"+kind, svc, orig.Server, strconv.Itoa(orig.Port))
				}
				_ = darwinNS(ctx, logf, "-set"+kind+"state", svc, set(orig.Enabled && !isLocalProxy(orig.Server, orig.Port, port)))
			}
			restore("webproxy", cur.Web, s.Web)
			restore("securewebproxy", cur.Secure, s.Secure)
			restore("socksfirewallproxy", cur.SOCKS, s.SOCKS)
			if ours(cur.Web) || ours(cur.Secure) || ours(cur.SOCKS) {
				if s.AutoProxyURL != "" {
					_ = darwinNS(ctx, logf, "-setautoproxyurl", svc, s.AutoProxyURL)
				}
				_ = darwinNS(ctx, logf, "-setautoproxystate", svc, set(s.AutoProxyOn))
				if len(s.BypassDomains) == 0 {
					_ = darwinNS(ctx, logf, "-setproxybypassdomains", svc, "Empty")
				} else {
					_ = darwinNS(ctx, logf, append([]string{"-setproxybypassdomains", svc}, s.BypassDomains...)...)
				}
			}
		}()
	}
	wg.Wait()
	return nil
}

// darwinNetworkServices lists the enabled network services (disabled ones are prefixed "*").
func darwinNetworkServices(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return nil, fmt.Errorf("list network services: %w", err)
	}
	var services []string
	for i, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		if i == 0 && strings.Contains(line, "asterisk") {
			continue
		}
		services = append(services, line)
	}
	return services, nil
}

func darwinNS(ctx context.Context, logf func(string), args ...string) error {
	out, err := exec.CommandContext(ctx, "networksetup", args...).CombinedOutput()
	clean := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Errorf("networksetup %s: %w: %s", strings.Join(args, " "), err, clean)
	}
	return nil
}

func darwinReadProxySnapshot(ctx context.Context, service string) darwinProxySnapshot {
	snap := darwinProxySnapshot{Service: service}
	snap.AutoProxyOn, snap.AutoProxyURL = darwinGetAutoProxy(ctx, service)
	snap.Web = darwinGetProxy(ctx, service, "-getwebproxy")
	snap.Secure = darwinGetProxy(ctx, service, "-getsecurewebproxy")
	snap.SOCKS = darwinGetProxy(ctx, service, "-getsocksfirewallproxy")
	snap.BypassDomains = darwinGetBypassDomains(ctx, service)
	return snap
}

func darwinGetProxy(ctx context.Context, service string, getCmd string) darwinProxy {
	out, err := exec.CommandContext(ctx, "networksetup", getCmd, service).Output()
	if err != nil {
		return darwinProxy{}
	}
	var p darwinProxy
	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "Enabled:"):
			v := strings.TrimSpace(strings.TrimPrefix(line, "Enabled:"))
			p.Enabled = strings.EqualFold(v, "yes") || strings.EqualFold(v, "on") || v == "1"
		case strings.HasPrefix(line, "Server:"):
			p.Server = strings.TrimSpace(strings.TrimPrefix(line, "Server:"))
		case strings.HasPrefix(line, "Port:"):
			p.Port, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Port:")))
		}
	}
	return p
}

func darwinGetAutoProxy(ctx context.Context, service string) (on bool, url string) {
	out, err := exec.CommandContext(ctx, "networksetup", "-getautoproxyurl", service).Output()
	if err != nil {
		return false, ""
	}
	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "URL:"):
			url = strings.TrimSpace(strings.TrimPrefix(line, "URL:"))
			if url == "(null)" {
				url = ""
			}
		case strings.HasPrefix(line, "Enabled:"):
			v := strings.TrimSpace(strings.TrimPrefix(line, "Enabled:"))
			on = strings.EqualFold(v, "yes") || strings.EqualFold(v, "on") || v == "1"
		}
	}
	return on, url
}

func darwinGetBypassDomains(ctx context.Context, service string) []string {
	out, err := exec.CommandContext(ctx, "networksetup", "-getproxybypassdomains", service).Output()
	if err != nil {
		return nil
	}
	s := strings.TrimSpace(string(out))
	low := strings.ToLower(s)
	if s == "" || strings.Contains(low, "there aren't any") || strings.Contains(low, "there are no") {
		return nil
	}
	return uniqueStrings(strings.Split(s, "\n"))
}
