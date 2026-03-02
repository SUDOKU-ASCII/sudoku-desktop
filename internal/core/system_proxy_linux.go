//go:build linux

package core

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type gsettingsSnapshot struct {
	Mode          string
	AutoConfigURL string
	HTTPHost      string
	HTTPPort      int
	HTTPSHost     string
	HTTPSPort     int
	SOCKSHost     string
	SOCKSPort     int
	IgnoreHosts   []string
}

func platformApplySystemProxy(cfg systemProxyConfig) (func() error, error) {
	if _, err := exec.LookPath("gsettings"); err != nil {
		if cfg.Logf != nil {
			cfg.Logf("gsettings not found; skip setting system proxy on linux")
		}
		return nil, nil
	}
	snap, err := gsettingsReadSnapshot()
	if err != nil {
		if cfg.Logf != nil {
			cfg.Logf(fmt.Sprintf("read gsettings proxy failed; skip: %v", err))
		}
		return nil, nil
	}

	applyErr := func() error {
		if cfg.ProxyMode == "pac" && strings.TrimSpace(cfg.PACURL) != "" {
			if err := gsettingsSet("org.gnome.system.proxy", "mode", "'auto'"); err != nil {
				return err
			}
			_ = gsettingsSet("org.gnome.system.proxy", "autoconfig-url", quoteGSettingsString(strings.TrimSpace(cfg.PACURL)))
			return nil
		}

		if cfg.LocalPort <= 0 || cfg.LocalPort > 65535 {
			return errors.New("invalid local port")
		}
		portStr := strconv.Itoa(cfg.LocalPort)

		if err := gsettingsSet("org.gnome.system.proxy", "mode", "'manual'"); err != nil {
			return err
		}
		_ = gsettingsSet("org.gnome.system.proxy.http", "host", "'127.0.0.1'")
		_ = gsettingsSet("org.gnome.system.proxy.http", "port", portStr)
		_ = gsettingsSet("org.gnome.system.proxy.https", "host", "'127.0.0.1'")
		_ = gsettingsSet("org.gnome.system.proxy.https", "port", portStr)
		_ = gsettingsSet("org.gnome.system.proxy.socks", "host", "'127.0.0.1'")
		_ = gsettingsSet("org.gnome.system.proxy.socks", "port", portStr)

		merged := uniqueStrings(append(append([]string(nil), snap.IgnoreHosts...), "localhost", "127.0.0.1", "::1"))
		_ = gsettingsSet("org.gnome.system.proxy", "ignore-hosts", gsettingsStringArray(merged))
		return nil
	}()
	if applyErr != nil {
		return nil, applyErr
	}
	return func() error {
		return gsettingsRestoreSnapshot(snap)
	}, nil
}

func gsettingsReadSnapshot() (gsettingsSnapshot, error) {
	mode, err := gsettingsGet("org.gnome.system.proxy", "mode")
	if err != nil {
		return gsettingsSnapshot{}, err
	}
	autoURL, _ := gsettingsGet("org.gnome.system.proxy", "autoconfig-url")
	httpHost, _ := gsettingsGet("org.gnome.system.proxy.http", "host")
	httpPort, _ := gsettingsGet("org.gnome.system.proxy.http", "port")
	httpsHost, _ := gsettingsGet("org.gnome.system.proxy.https", "host")
	httpsPort, _ := gsettingsGet("org.gnome.system.proxy.https", "port")
	socksHost, _ := gsettingsGet("org.gnome.system.proxy.socks", "host")
	socksPort, _ := gsettingsGet("org.gnome.system.proxy.socks", "port")
	ignore, _ := gsettingsGet("org.gnome.system.proxy", "ignore-hosts")

	return gsettingsSnapshot{
		Mode:          strings.TrimSpace(mode),
		AutoConfigURL: strings.TrimSpace(autoURL),
		HTTPHost:      strings.TrimSpace(httpHost),
		HTTPPort:      atoiLoose(httpPort),
		HTTPSHost:     strings.TrimSpace(httpsHost),
		HTTPSPort:     atoiLoose(httpsPort),
		SOCKSHost:     strings.TrimSpace(socksHost),
		SOCKSPort:     atoiLoose(socksPort),
		IgnoreHosts:   parseGSettingsStringArray(ignore),
	}, nil
}

func gsettingsRestoreSnapshot(snap gsettingsSnapshot) error {
	_ = gsettingsSet("org.gnome.system.proxy", "mode", snap.Mode)
	if strings.TrimSpace(snap.AutoConfigURL) != "" {
		_ = gsettingsSet("org.gnome.system.proxy", "autoconfig-url", snap.AutoConfigURL)
	}
	_ = gsettingsSet("org.gnome.system.proxy.http", "host", snap.HTTPHost)
	_ = gsettingsSet("org.gnome.system.proxy.http", "port", strconv.Itoa(snap.HTTPPort))
	_ = gsettingsSet("org.gnome.system.proxy.https", "host", snap.HTTPSHost)
	_ = gsettingsSet("org.gnome.system.proxy.https", "port", strconv.Itoa(snap.HTTPSPort))
	_ = gsettingsSet("org.gnome.system.proxy.socks", "host", snap.SOCKSHost)
	_ = gsettingsSet("org.gnome.system.proxy.socks", "port", strconv.Itoa(snap.SOCKSPort))
	_ = gsettingsSet("org.gnome.system.proxy", "ignore-hosts", gsettingsStringArray(snap.IgnoreHosts))
	return nil
}

func gsettingsGet(schema string, key string) (string, error) {
	out, err := exec.Command("gsettings", "get", schema, key).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gsettings get %s %s: %w: %s", schema, key, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func gsettingsSet(schema string, key string, value string) error {
	out, err := exec.Command("gsettings", "set", schema, key, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("gsettings set %s %s %s: %w: %s", schema, key, value, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func quoteGSettingsString(s string) string {
	s = strings.ReplaceAll(s, "'", "\\'")
	return "'" + s + "'"
}

func gsettingsStringArray(items []string) string {
	items = uniqueStrings(items)
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, quoteGSettingsString(it))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func parseGSettingsStringArray(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "@" {
		return nil
	}
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "'\"")
		if p != "" {
			out = append(out, p)
		}
	}
	return uniqueStrings(out)
}

func atoiLoose(s string) int {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "'\"")
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return 0
}
