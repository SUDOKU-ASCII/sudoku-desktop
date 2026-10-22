package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// detectIP asks public geo-IP endpoints which address the world sees, either directly or
// through the local mixed listener. dialDirect, when non-nil, replaces the system dialer for
// the direct probe (used to bypass the TUN so the real uplink address is reported).
func detectIP(useProxy bool, localPort int, dialDirect func(ctx context.Context, network, addr string) (net.Conn, error)) IPDetectResult {
	result := IPDetectResult{
		Source:        "unknown",
		UsedProxy:     useProxy,
		CheckedAtUnix: time.Now().UnixMilli(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	transport := &http.Transport{DisableKeepAlives: true, Proxy: nil}
	defer transport.CloseIdleConnections()
	if useProxy {
		proxyAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort))
		conn, err := net.DialTimeout("tcp", proxyAddr, 900*time.Millisecond)
		if err != nil {
			result.Error = fmt.Sprintf("proxy core not listening on %s: %v", proxyAddr, err)
			return result
		}
		_ = conn.Close()
		dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		transport.DialContext = func(ctx context.Context, network string, addr string) (net.Conn, error) {
			if cd, ok := dialer.(proxy.ContextDialer); ok {
				return cd.DialContext(ctx, network, addr)
			}
			return dialer.Dial(network, addr)
		}
	} else if dialDirect != nil {
		transport.DialContext = dialDirect
	}
	client := &http.Client{Transport: transport, Timeout: 6 * time.Second}
	endpoints := []struct{ source, url string }{
		{"api.ip.sb", "https://api.ip.sb/geoip"},
		{"ipapi.co", "https://ipapi.co/json/"},
		{"ipinfo.io", "https://ipinfo.io/json"},
		{"api64.ipify.org", "https://api64.ipify.org?format=json"},
		{"ifconfig.me", "https://ifconfig.me/ip"},
	}
	var errs []string
	for _, ep := range endpoints {
		if ctx.Err() != nil {
			break
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ep.url, nil)
		resp, err := client.Do(req)
		if err != nil {
			errs = append(errs, ep.source+": "+err.Error())
			continue
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if rerr != nil {
			errs = append(errs, ep.source+": "+rerr.Error())
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errs = append(errs, ep.source+": http "+resp.Status)
			continue
		}
		ip, country, region, isp := parseIPDetectPayload(body)
		if ip == "" {
			errs = append(errs, ep.source+": empty ip")
			continue
		}
		result.Source, result.IP, result.Country, result.Region, result.ISP = ep.source, ip, country, region, isp
		return result
	}
	if len(errs) > 0 {
		result.Error = strings.Join(uniqueStrings(errs), " | ")
	} else {
		result.Error = "no available ip detection endpoint"
	}
	return result
}

func parseIPDetectPayload(body []byte) (ip, country, region, isp string) {
	type payload struct {
		IP           string `json:"ip"`
		Query        string `json:"query"`
		Country      string `json:"country"`
		CountryName  string `json:"country_name"`
		Region       string `json:"region"`
		RegionName   string `json:"region_name"`
		ISP          string `json:"isp"`
		Org          string `json:"org"`
		Organization string `json:"organization"`
	}

	var p payload
	if err := json.Unmarshal(body, &p); err == nil {
		ip = firstNonEmpty(strings.TrimSpace(p.IP), strings.TrimSpace(p.Query))
		if ip != "" && net.ParseIP(ip) != nil {
			country = firstNonEmpty(strings.TrimSpace(p.Country), strings.TrimSpace(p.CountryName))
			region = firstNonEmpty(strings.TrimSpace(p.Region), strings.TrimSpace(p.RegionName))
			isp = firstNonEmpty(strings.TrimSpace(p.ISP), strings.TrimSpace(p.Org), strings.TrimSpace(p.Organization))
			return ip, country, region, isp
		}
	}

	txt := strings.TrimSpace(string(body))
	for _, field := range strings.FieldsFunc(txt, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\t' || r == ' ' || r == ',' || r == ';'
	}) {
		candidate := strings.TrimSpace(field)
		if net.ParseIP(candidate) != nil {
			return candidate, "", "", ""
		}
	}
	return "", "", "", ""
}

func firstNonEmpty(items ...string) string {
	for _, it := range items {
		if strings.TrimSpace(it) != "" {
			return strings.TrimSpace(it)
		}
	}
	return ""
}

func sortLatencyResults(results []LatencyResult) {
	sort.SliceStable(results, func(i, j int) bool {
		a := results[i]
		b := results[j]
		if a.ConnectOK != b.ConnectOK {
			return a.ConnectOK
		}
		if a.LatencyMs < 0 {
			return false
		}
		if b.LatencyMs < 0 {
			return true
		}
		if a.LatencyMs == b.LatencyMs {
			return strings.ToLower(a.NodeName) < strings.ToLower(b.NodeName)
		}
		return a.LatencyMs < b.LatencyMs
	})
}
