package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// systemProxySnapshot is everything needed to put the OS proxy configuration back the way
// the user had it. It is JSON-serializable on purpose: it lives in the crash journal so
// a guard process (or the next launch) can restore it after a crash or kill -9.
type systemProxySnapshot struct {
	Darwin  []darwinProxySnapshot `json:"darwin,omitempty"`
	Linux   *linuxProxySnapshot   `json:"linux,omitempty"`
	Windows *windowsProxySnapshot `json:"windows,omitempty"`
}

type darwinProxy struct {
	Enabled bool
	Server  string
	Port    int
}

type darwinProxySnapshot struct {
	Service       string
	AutoProxyOn   bool
	AutoProxyURL  string
	Web           darwinProxy
	Secure        darwinProxy
	SOCKS         darwinProxy
	BypassDomains []string
}

type linuxProxySnapshot struct {
	Gnome *gsettingsSnapshot `json:"gnome,omitempty"`
	KDE   *kdeProxySnapshot  `json:"kde,omitempty"`
}

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

type kdeProxySnapshot struct {
	Tool        string // kwriteconfig5 | kwriteconfig6
	ProxyType   string
	HTTPProxy   string
	HTTPSProxy  string
	SOCKSProxy  string
	NoProxyFor  string
	AutoConfURL string
}

type windowsProxySnapshot struct {
	ProxyEnable   uint32
	AutoDetect    uint32
	ProxyServer   string
	AutoConfigURL string
	ProxyOverride string
	// WinHTTP is the raw machine-wide WinHttpSettings value (services, PowerShell, winget…),
	// nil when unset. It is only touched when the app runs elevated.
	WinHTTP []byte `json:",omitempty"`
}

var proxyBypassLocal = []string{"localhost", "127.0.0.1", "::1"}

// captureSystemProxy reads the current configuration without changing anything.
func captureSystemProxy(ctx context.Context) (*systemProxySnapshot, error) {
	return platformCaptureSystemProxy(ctx)
}

// setSystemProxy points the OS at the local mixed listener. base is the snapshot taken
// before, so user bypass entries are preserved.
func setSystemProxy(ctx context.Context, base *systemProxySnapshot, port int, logf func(string)) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid proxy port %d", port)
	}
	return platformSetSystemProxy(ctx, base, port, logf)
}

// restoreSystemProxy puts snap back, but only where the OS still points at us: if the
// user (or another VPN tool) changed the setting meanwhile, their choice wins.
func restoreSystemProxy(snap *systemProxySnapshot, port int, logf func(string)) error {
	if snap == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return platformRestoreSystemProxy(ctx, snap, port, logf)
}

// isLocalProxy reports whether host:port is the loopback proxy this app installs.
func isLocalProxy(host string, port, want int) bool {
	if port != want {
		return false
	}
	switch strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]")) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}
