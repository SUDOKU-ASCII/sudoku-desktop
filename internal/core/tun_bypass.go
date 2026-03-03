package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type tunBypass struct {
	V4Path  string
	V6Path  string
	V4Count int
	V6Count int
}

type chnCIDRYaml struct {
	Payload []string `yaml:"payload"`
}

func prepareTunBypass(ctx context.Context, store *Store, cfg *AppConfig, logf func(string)) (tunBypass, error) {
	return prepareTunBypassWithClient(ctx, store, cfg, nil, logf)
}

func prepareTunBypassWithClient(ctx context.Context, store *Store, cfg *AppConfig, client *http.Client, logf func(string)) (tunBypass, error) {
	if store == nil || cfg == nil {
		return tunBypass{}, errors.New("nil store/cfg")
	}
	if strings.ToLower(strings.TrimSpace(cfg.Routing.ProxyMode)) != "pac" {
		return tunBypass{}, nil
	}

	urls := append([]string(nil), cfg.Routing.RuleURLs...)
	if len(urls) == 0 && (!cfg.Routing.CustomRulesEnabled || strings.TrimSpace(cfg.Routing.CustomRules) == "") {
		urls = defaultPACRuleURLs()
	}
	v4URL, v6URL := findChnCIDRURLs(urls)
	v4Candidates, v6Candidates := chnCIDRURLCandidates(v4URL, v6URL)
	if len(v4Candidates) == 0 && len(v6Candidates) == 0 {
		return tunBypass{}, nil
	}

	cacheDir := filepath.Join(store.RuntimeDir(), "cache")
	bypassDir := filepath.Join(store.RuntimeDir(), "bypass")
	if err := ensureDir(cacheDir); err != nil {
		return tunBypass{}, err
	}
	if err := ensureDir(bypassDir); err != nil {
		return tunBypass{}, err
	}

	const maxCacheAge = 7 * 24 * time.Hour
	out := tunBypass{}
	var errs []string

	if len(v4Candidates) > 0 {
		cidrs, usedURL, err := fetchCIDRsFromAny(ctx, v4Candidates, cacheDir, maxCacheAge, client, false)
		if err != nil {
			errs = append(errs, fmt.Sprintf("ipv4: %v", err))
		} else if len(cidrs) > 0 {
			out.V4Count = len(cidrs)
			out.V4Path = filepath.Join(bypassDir, "cn_ipv4.txt")
			if werr := writeLines(out.V4Path, cidrs); werr != nil {
				return tunBypass{}, werr
			}
			if logf != nil {
				logf(fmt.Sprintf("prepared CN bypass list (ipv4): %d routes (%s)", out.V4Count, usedURL))
			}
		}
	}
	if len(v6Candidates) > 0 {
		cidrs, usedURL, err := fetchCIDRsFromAny(ctx, v6Candidates, cacheDir, maxCacheAge, client, true)
		if err != nil {
			errs = append(errs, fmt.Sprintf("ipv6: %v", err))
		} else if len(cidrs) > 0 {
			out.V6Count = len(cidrs)
			out.V6Path = filepath.Join(bypassDir, "cn_ipv6.txt")
			if werr := writeLines(out.V6Path, cidrs); werr != nil {
				return tunBypass{}, werr
			}
			if logf != nil {
				logf(fmt.Sprintf("prepared CN bypass list (ipv6): %d routes (%s)", out.V6Count, usedURL))
			}
		}
	}
	if out.V4Path == "" && out.V6Path == "" && len(errs) > 0 {
		return tunBypass{}, errors.New(strings.Join(errs, " | "))
	}
	return out, nil
}

func findChnCIDRURLs(urls []string) (v4 string, v6 string) {
	for _, u := range urls {
		s := strings.ToLower(strings.TrimSpace(u))
		if s == "" {
			continue
		}
		if !strings.Contains(s, "chn-cidr-list") {
			continue
		}
		if strings.Contains(s, "ipv4.yaml") && v4 == "" {
			v4 = strings.TrimSpace(u)
		}
		if strings.Contains(s, "ipv6.yaml") && v6 == "" {
			v6 = strings.TrimSpace(u)
		}
	}
	return v4, v6
}

func chnCIDRURLCandidates(v4URL string, v6URL string) (v4 []string, v6 []string) {
	// Prefer the user's configured sources (found from RuleURLs), but allow a few derived mirrors
	// (raw GitHub, jsDelivr, gh-proxy) for resilience. Do not hardcode CN bypass sources when the
	// user didn't configure them.
	if strings.TrimSpace(v4URL) != "" {
		v4 = append(v4, urlMirrorCandidates(strings.TrimSpace(v4URL))...)
	}
	if strings.TrimSpace(v6URL) != "" {
		v6 = append(v6, urlMirrorCandidates(strings.TrimSpace(v6URL))...)
	}
	return uniqueStrings(v4), uniqueStrings(v6)
}

func fetchCIDRsFromAny(ctx context.Context, urls []string, cacheDir string, maxAge time.Duration, client *http.Client, wantV6 bool) (cidrs []string, usedURL string, err error) {
	var errs []string
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		rawPath, ferr := fetchCachedWithClient(ctx, u, cacheDir, maxAge, client)
		if ferr != nil {
			errs = append(errs, u+": "+ferr.Error())
			continue
		}
		parsed, perr := parseCIDRsFromYAMLFile(rawPath, wantV6)
		if perr != nil {
			errs = append(errs, u+": "+perr.Error())
			continue
		}
		if len(parsed) == 0 {
			errs = append(errs, u+": empty cidr list")
			continue
		}
		return parsed, u, nil
	}
	if len(errs) > 0 {
		return nil, "", errors.New(strings.Join(errs, " | "))
	}
	return nil, "", errors.New("no available cidr urls")
}

func urlMirrorCandidates(url string) []string {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil
	}
	const (
		ghProxyPrefix = "https://gh-proxy.org/https://"
		rawPrefix     = "https://raw.githubusercontent.com/"
		jsdPrefix     = "https://fastly.jsdelivr.net/gh/"
	)

	out := []string{url}

	// Unwrap gh-proxy -> raw.
	if strings.HasPrefix(url, ghProxyPrefix+rawPrefix) {
		out = append(out, strings.TrimPrefix(url, ghProxyPrefix))
	}

	// raw -> jsDelivr (+ gh-proxy variant).
	for _, candidate := range []string{url, strings.TrimPrefix(url, ghProxyPrefix)} {
		if strings.HasPrefix(candidate, rawPrefix) {
			parts := strings.Split(strings.TrimPrefix(candidate, rawPrefix), "/")
			// owner/repo/ref/path...
			if len(parts) >= 4 && parts[0] != "" && parts[1] != "" && parts[2] != "" {
				owner, repo, ref := parts[0], parts[1], parts[2]
				path := strings.Join(parts[3:], "/")
				out = append(out,
					jsdPrefix+owner+"/"+repo+"@"+ref+"/"+path,
					ghProxyPrefix+candidate,
				)
			}
		}
	}

	// jsDelivr -> raw (+ gh-proxy variant).
	if strings.HasPrefix(url, jsdPrefix) {
		rest := strings.TrimPrefix(url, jsdPrefix)
		parts := strings.Split(rest, "/")
		// owner/repo@ref/path...
		if len(parts) >= 2 {
			owner := parts[0]
			repoRef := parts[1]
			if owner != "" && strings.Contains(repoRef, "@") {
				repoParts := strings.SplitN(repoRef, "@", 2)
				repo, ref := repoParts[0], repoParts[1]
				path := ""
				if len(parts) > 2 {
					path = strings.Join(parts[2:], "/")
				}
				if repo != "" && ref != "" && path != "" {
					raw := rawPrefix + owner + "/" + repo + "/" + ref + "/" + path
					out = append(out, raw, ghProxyPrefix+raw)
				}
			}
		}
	}

	return uniqueStrings(out)
}

func fetchCached(ctx context.Context, url string, cacheDir string, maxAge time.Duration) (string, error) {
	return fetchCachedWithClient(ctx, url, cacheDir, maxAge, nil)
}

func fetchCachedWithClient(ctx context.Context, url string, cacheDir string, maxAge time.Duration, client *http.Client) (string, error) {
	sum := sha256.Sum256([]byte(url))
	name := hex.EncodeToString(sum[:]) + ".yaml"
	path := filepath.Join(cacheDir, name)

	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < maxAge && st.Size() > 0 {
		return path, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "sudoku-desktop/1.0")
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		// If we have any cache, fall back.
		if _, serr := os.Stat(path); serr == nil {
			return path, nil
		}
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if _, serr := os.Stat(path); serr == nil {
			return path, nil
		}
		return "", fmt.Errorf("fetch %s: http %s", url, resp.Status)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", closeErr
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}

func parseCIDRsFromYAMLFile(path string, wantV6 bool) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var doc chnCIDRYaml
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Payload) == 0 {
		// Fallback: scan for CIDR-like tokens.
		lines := strings.Split(string(raw), "\n")
		doc.Payload = make([]string, 0, len(lines))
		for _, ln := range lines {
			ln = strings.TrimSpace(strings.TrimPrefix(ln, "-"))
			if ln == "" {
				continue
			}
			doc.Payload = append(doc.Payload, ln)
		}
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(doc.Payload))
	for _, item := range doc.Payload {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(item)
		if err != nil || ipnet == nil {
			continue
		}
		isV6 := ipnet.IP.To4() == nil
		if wantV6 != isV6 {
			continue
		}
		s := ipnet.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}

func writeLines(path string, lines []string) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
