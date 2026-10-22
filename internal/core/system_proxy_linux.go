//go:build linux

package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func platformCaptureSystemProxy(ctx context.Context) (*systemProxySnapshot, error) {
	snap := &linuxProxySnapshot{}
	if _, err := exec.LookPath("gsettings"); err == nil {
		if g, err := gsettingsReadSnapshot(ctx); err == nil {
			if isLocalProxy(g.HTTPHost, g.HTTPPort, ProxyPort) {
				g.Mode = "'none'"
			}
			snap.Gnome = &g
		}
	}
	if tool := kdeWriteTool(); tool != "" {
		k := kdeReadSnapshot(ctx, tool)
		if strings.Contains(k.HTTPProxy, "127.0.0.1:"+strconv.Itoa(ProxyPort)) {
			k.ProxyType = "0"
		}
		snap.KDE = &k
	}
	if snap.Gnome == nil && snap.KDE == nil {
		return nil, errors.New("no supported desktop proxy settings found (gsettings/kwriteconfig)")
	}
	return &systemProxySnapshot{Linux: snap}, nil
}

func platformSetSystemProxy(ctx context.Context, base *systemProxySnapshot, port int, logf func(string)) error {
	if base == nil || base.Linux == nil {
		return errors.New("no supported desktop proxy settings found")
	}
	portStr := strconv.Itoa(port)
	if g := base.Linux.Gnome; g != nil {
		if err := gsettingsSet(ctx, "org.gnome.system.proxy", "mode", "'manual'"); err != nil {
			return err
		}
		for _, s := range []string{"http", "https", "socks"} {
			_ = gsettingsSet(ctx, "org.gnome.system.proxy."+s, "host", "'127.0.0.1'")
			_ = gsettingsSet(ctx, "org.gnome.system.proxy."+s, "port", portStr)
		}
		merged := uniqueStrings(append(append([]string(nil), g.IgnoreHosts...), proxyBypassLocal...))
		_ = gsettingsSet(ctx, "org.gnome.system.proxy", "ignore-hosts", gsettingsStringArray(merged))
	}
	if k := base.Linux.KDE; k != nil {
		addr := "127.0.0.1 " + portStr
		kdeWrite(ctx, k.Tool, "ProxyType", "1")
		kdeWrite(ctx, k.Tool, "httpProxy", "http://"+addr)
		kdeWrite(ctx, k.Tool, "httpsProxy", "http://"+addr)
		kdeWrite(ctx, k.Tool, "socksProxy", "socks://"+addr)
		kdeWrite(ctx, k.Tool, "NoProxyFor", strings.Join(uniqueStrings(append(strings.Split(k.NoProxyFor, ","), proxyBypassLocal...)), ","))
		kdeNotify(ctx)
	}
	return nil
}

func platformRestoreSystemProxy(ctx context.Context, snap *systemProxySnapshot, port int, logf func(string)) error {
	if snap.Linux == nil {
		return nil
	}
	if g := snap.Linux.Gnome; g != nil {
		if cur, err := gsettingsReadSnapshot(ctx); err == nil && isLocalProxy(cur.HTTPHost, cur.HTTPPort, port) {
			_ = gsettingsSet(ctx, "org.gnome.system.proxy", "mode", g.Mode)
			if g.AutoConfigURL != "" {
				_ = gsettingsSet(ctx, "org.gnome.system.proxy", "autoconfig-url", g.AutoConfigURL)
			}
			for _, e := range []struct {
				name string
				host string
				port int
			}{{"http", g.HTTPHost, g.HTTPPort}, {"https", g.HTTPSHost, g.HTTPSPort}, {"socks", g.SOCKSHost, g.SOCKSPort}} {
				if isLocalProxy(e.host, e.port, port) {
					e.host, e.port = "''", 0
				}
				_ = gsettingsSet(ctx, "org.gnome.system.proxy."+e.name, "host", e.host)
				_ = gsettingsSet(ctx, "org.gnome.system.proxy."+e.name, "port", strconv.Itoa(e.port))
			}
			_ = gsettingsSet(ctx, "org.gnome.system.proxy", "ignore-hosts", gsettingsStringArray(g.IgnoreHosts))
		}
	}
	if k := snap.Linux.KDE; k != nil {
		cur := kdeReadSnapshot(ctx, k.Tool)
		if strings.Contains(cur.HTTPProxy, "127.0.0.1 "+strconv.Itoa(port)) || strings.Contains(cur.HTTPProxy, "127.0.0.1:"+strconv.Itoa(port)) {
			kdeWrite(ctx, k.Tool, "ProxyType", k.ProxyType)
			kdeWrite(ctx, k.Tool, "httpProxy", k.HTTPProxy)
			kdeWrite(ctx, k.Tool, "httpsProxy", k.HTTPSProxy)
			kdeWrite(ctx, k.Tool, "socksProxy", k.SOCKSProxy)
			kdeWrite(ctx, k.Tool, "NoProxyFor", k.NoProxyFor)
			kdeNotify(ctx)
		}
	}
	return nil
}

func platformSystemProxyIsOurs(ctx context.Context, port int) bool {
	if _, err := exec.LookPath("gsettings"); err == nil {
		if g, err := gsettingsReadSnapshot(ctx); err == nil && strings.Contains(g.Mode, "manual") && isLocalProxy(g.HTTPHost, g.HTTPPort, port) {
			return true
		}
	}
	if tool := kdeWriteTool(); tool != "" {
		k := kdeReadSnapshot(ctx, tool)
		return k.ProxyType == "1" && strings.Contains(k.HTTPProxy, "127.0.0.1") && strings.Contains(k.HTTPProxy, strconv.Itoa(port))
	}
	return false
}

func gsettingsReadSnapshot(ctx context.Context) (gsettingsSnapshot, error) {
	mode, err := gsettingsGet(ctx, "org.gnome.system.proxy", "mode")
	if err != nil {
		return gsettingsSnapshot{}, err
	}
	get := func(schema, key string) string { v, _ := gsettingsGet(ctx, schema, key); return v }
	return gsettingsSnapshot{
		Mode:          mode,
		AutoConfigURL: get("org.gnome.system.proxy", "autoconfig-url"),
		HTTPHost:      get("org.gnome.system.proxy.http", "host"),
		HTTPPort:      atoiLoose(get("org.gnome.system.proxy.http", "port")),
		HTTPSHost:     get("org.gnome.system.proxy.https", "host"),
		HTTPSPort:     atoiLoose(get("org.gnome.system.proxy.https", "port")),
		SOCKSHost:     get("org.gnome.system.proxy.socks", "host"),
		SOCKSPort:     atoiLoose(get("org.gnome.system.proxy.socks", "port")),
		IgnoreHosts:   parseGSettingsStringArray(get("org.gnome.system.proxy", "ignore-hosts")),
	}, nil
}

func gsettingsGet(ctx context.Context, schema string, key string) (string, error) {
	out, err := exec.CommandContext(ctx, "gsettings", "get", schema, key).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gsettings get %s %s: %w: %s", schema, key, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func gsettingsSet(ctx context.Context, schema string, key string, value string) error {
	out, err := exec.CommandContext(ctx, "gsettings", "set", schema, key, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("gsettings set %s %s: %w: %s", schema, key, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func gsettingsStringArray(items []string) string {
	items = uniqueStrings(items)
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, "'"+strings.ReplaceAll(it, "'", "\\'")+"'")
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func parseGSettingsStringArray(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]")
	if strings.TrimSpace(raw) == "" || strings.HasPrefix(raw, "@") {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.Trim(strings.TrimSpace(p), "'\""); p != "" {
			out = append(out, p)
		}
	}
	return uniqueStrings(out)
}

func atoiLoose(s string) int {
	n, _ := strconv.Atoi(strings.Trim(strings.TrimSpace(s), "'\""))
	return n
}

func kdeWriteTool() string {
	if os.Getenv("XDG_CURRENT_DESKTOP") == "" || !strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "KDE") {
		return ""
	}
	for _, t := range []string{"kwriteconfig6", "kwriteconfig5"} {
		if _, err := exec.LookPath(t); err == nil {
			return t
		}
	}
	return ""
}

func kdeReadTool(writeTool string) string {
	return strings.Replace(writeTool, "kwriteconfig", "kreadconfig", 1)
}

func kdeReadSnapshot(ctx context.Context, tool string) kdeProxySnapshot {
	get := func(key string) string {
		out, _ := exec.CommandContext(ctx, kdeReadTool(tool), "--file", "kioslaverc", "--group", "Proxy Settings", "--key", key).Output()
		return strings.TrimSpace(string(out))
	}
	return kdeProxySnapshot{
		Tool: tool, ProxyType: get("ProxyType"), HTTPProxy: get("httpProxy"), HTTPSProxy: get("httpsProxy"),
		SOCKSProxy: get("socksProxy"), NoProxyFor: get("NoProxyFor"), AutoConfURL: get("Proxy Config Script"),
	}
}

func kdeWrite(ctx context.Context, tool, key, value string) {
	_ = exec.CommandContext(ctx, tool, "--file", "kioslaverc", "--group", "Proxy Settings", "--key", key, value).Run()
}

func kdeNotify(ctx context.Context) {
	if _, err := exec.LookPath("dbus-send"); err == nil {
		_ = exec.CommandContext(ctx, "dbus-send", "--type=signal", "/KIO/Scheduler", "org.kde.KIO.Scheduler.reparseSlaveConfiguration", "string:").Run()
	}
}
