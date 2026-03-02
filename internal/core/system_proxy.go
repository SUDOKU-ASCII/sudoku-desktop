package core

import "strings"

type systemProxyConfig struct {
	ProxyMode string // "pac"|"global"|"direct"
	LocalPort int
	PACURL    string
	Logf      func(string)
}

// applySystemProxy applies OS system proxy settings for "no-TUN" mode.
// It returns a restore function which should be called when the proxy stops.
func applySystemProxy(cfg systemProxyConfig) (restore func() error, err error) {
	cfg.ProxyMode = strings.ToLower(strings.TrimSpace(cfg.ProxyMode))
	return platformApplySystemProxy(cfg)
}
