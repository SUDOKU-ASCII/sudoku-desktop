package core

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type cnRuleSet struct {
	domainExact  map[string]struct{}
	domainSuffix map[string]struct{}
}

type ruleSetYAML struct {
	Payload []string `yaml:"payload"`
}

func prepareCNRules(ctx context.Context, store *Store, cfg *AppConfig, client *http.Client, logf func(string)) (*cnRuleSet, error) {
	if store == nil || cfg == nil {
		return nil, fmt.Errorf("nil store/cfg")
	}
	if strings.ToLower(strings.TrimSpace(cfg.Routing.ProxyMode)) != "pac" {
		return nil, nil
	}
	custom := ""
	if cfg.Routing.CustomRulesEnabled {
		custom = strings.TrimSpace(cfg.Routing.CustomRules)
	}
	urls := append([]string(nil), cfg.Routing.RuleURLs...)
	if len(urls) == 0 && custom == "" {
		urls = defaultPACRuleURLs()
	}
	if len(urls) == 0 && custom == "" {
		return nil, nil
	}

	cacheDir := filepath.Join(store.RuntimeDir(), "cache")
	if err := ensureDir(cacheDir); err != nil {
		return nil, err
	}
	const maxCacheAge = 7 * 24 * time.Hour

	out := &cnRuleSet{
		domainExact:  map[string]struct{}{},
		domainSuffix: map[string]struct{}{},
	}

	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		switch strings.ToLower(u) {
		case "global", "direct":
			continue
		}
		rawPath, err := fetchCachedWithClient(ctx, u, cacheDir, maxCacheAge, client)
		if err != nil {
			// Keep going; partial rules are still useful.
			if logf != nil {
				logf(fmt.Sprintf("download rule list failed: %s: %v", u, err))
			}
			continue
		}
		if err := parseCNRuleFile(rawPath, out); err != nil {
			if logf != nil {
				logf(fmt.Sprintf("parse rule list failed: %s: %v", u, err))
			}
		}
	}
	if custom != "" {
		parseCNRuleText(custom, out)
	}

	if logf != nil {
		logf(fmt.Sprintf("prepared CN domain rules: %d exact, %d suffix", len(out.domainExact), len(out.domainSuffix)))
	}
	return out, nil
}

func parseCNRuleFile(path string, out *cnRuleSet) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var doc ruleSetYAML
	if err := yaml.Unmarshal(raw, &doc); err == nil && len(doc.Payload) > 0 {
		for _, rule := range doc.Payload {
			parseCNRuleLine(rule, out)
		}
		return nil
	}

	buf := bytes.NewBuffer(raw)
	for {
		line, err := buf.ReadString('\n')
		if err != nil && len(line) == 0 {
			break
		}
		parseCNRuleLine(line, out)
		if err != nil {
			break
		}
	}
	return nil
}

func parseCNRuleText(text string, out *cnRuleSet) {
	for _, line := range strings.Split(text, "\n") {
		parseCNRuleLine(line, out)
	}
}

func parseCNRuleLine(line string, out *cnRuleSet) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
		return
	}
	parts := strings.Split(line, ",")
	if len(parts) < 2 {
		return
	}
	typ := strings.ToUpper(strings.TrimSpace(parts[0]))
	val := normalizeRuleDomain(parts[1])
	if val == "" {
		return
	}
	switch typ {
	case "DOMAIN":
		out.domainExact[val] = struct{}{}
	case "DOMAIN-SUFFIX":
		out.domainSuffix[val] = struct{}{}
	}
}

func (r *cnRuleSet) matchDomain(host string) bool {
	if r == nil {
		return false
	}
	host = normalizeLookupHost(host)
	if host == "" {
		return false
	}
	if _, ok := r.domainExact[host]; ok {
		return true
	}
	parts := strings.Split(host, ".")
	for i := 0; i < len(parts); i++ {
		suffix := strings.Join(parts[i:], ".")
		if _, ok := r.domainSuffix[suffix]; ok {
			return true
		}
	}
	return false
}

func normalizeRuleDomain(v string) string {
	v = strings.Trim(v, "'\"")
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimSuffix(v, ".")
	v = strings.TrimPrefix(v, ".")
	return v
}

func normalizeLookupHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if h, _, err := netSplitHostPortLoose(host); err == nil && h != "" {
		host = h
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	host = strings.TrimSuffix(host, ".")
	return strings.ToLower(host)
}

func netSplitHostPortLoose(hostport string) (host, port string, err error) {
	// Keep it local to avoid importing net everywhere this helper is used.
	// Accept raw host without port as a non-error.
	if strings.Count(hostport, ":") == 0 {
		return hostport, "", nil
	}
	// Bracketed IPv6 without port.
	if strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]"), "", nil
	}
	// Delegate to net.SplitHostPort semantics when it looks like host:port.
	lastColon := strings.LastIndex(hostport, ":")
	if lastColon <= 0 || lastColon == len(hostport)-1 {
		return hostport, "", nil
	}
	// If there are multiple colons and no brackets, treat as raw IPv6.
	if strings.Count(hostport, ":") > 1 && !strings.HasPrefix(hostport, "[") {
		return hostport, "", nil
	}
	return hostport[:lastColon], hostport[lastColon+1:], nil
}
