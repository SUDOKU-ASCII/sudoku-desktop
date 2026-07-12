package core

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

func probeNodeLatency(node NodeConfig, appCfg AppConfig) LatencyResult {
	result := LatencyResult{
		NodeID:        node.ID,
		NodeName:      node.Name,
		LatencyMs:     -1,
		CheckedAtUnix: time.Now().UnixMilli(),
	}
	if strings.TrimSpace(appCfg.Core.SudokuBinary) == "" {
		result.Error = "sudoku binary is empty"
		return result
	}

	ln, err := net.Listen("tcp4", net.JoinHostPort(localLoopbackIPv4, "0"))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	localPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	tempDir, err := os.MkdirTemp("", "sudoku-node-probe-*")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer os.RemoveAll(tempDir)

	node.LocalPort = localPort
	appCfg.Core.LocalPort = localPort
	appCfg.Routing.ProxyMode = "global"
	appCfg.ReverseClient = ReverseClientSettings{Routes: []ReverseRoute{}}
	clientCfg, err := buildSudokuClientConfig(&appCfg, node, "", true)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	configPath := filepath.Join(tempDir, "client.config.json")
	if err := writeJSONFile(configPath, clientCfg); err != nil {
		result.Error = err.Error()
		return result
	}

	var logMu sync.Mutex
	var lastLines []string
	proc := NewManagedProcess("sudoku-probe")
	if err := proc.Start(
		appCfg.Core.SudokuBinary,
		[]string{"-c", configPath},
		[]string{"SUDOKU_LOG_LEVEL=error"},
		tempDir,
		func(line string) {
			line = strings.TrimSpace(line)
			if line == "" {
				return
			}
			logMu.Lock()
			lastLines = append(lastLines, line)
			if len(lastLines) > 8 {
				lastLines = lastLines[len(lastLines)-8:]
			}
			logMu.Unlock()
		},
	); err != nil {
		result.Error = err.Error()
		return result
	}
	defer proc.Stop(1200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	socksAddr := net.JoinHostPort(localLoopbackIPv4, strconv.Itoa(localPort))
	if err := waitForTCPReady(ctx, socksAddr, 4*time.Second); err != nil {
		logMu.Lock()
		tail := strings.Join(lastLines, " | ")
		logMu.Unlock()
		if tail != "" {
			result.Error = fmt.Sprintf("%v: %s", err, tail)
		} else {
			result.Error = err.Error()
		}
		return result
	}

	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, &net.Dialer{Timeout: 8 * time.Second})
	if err != nil {
		result.Error = err.Error()
		return result
	}
	start := time.Now()
	conn, err := dialer.Dial("tcp", "i.ytimg.com:443")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))

	tlsConn := tls.Client(conn, &tls.Config{ServerName: "i.ytimg.com"})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		result.Error = err.Error()
		return result
	}
	if _, err := io.WriteString(tlsConn, "GET /generate_204 HTTP/1.1\r\nHost: i.ytimg.com\r\nConnection: close\r\n\r\n"); err != nil {
		result.Error = err.Error()
		return result
	}
	reader := bufio.NewReader(tlsConn)
	line, err := reader.ReadString('\n')
	if err != nil {
		result.Error = err.Error()
		return result
	}
	parts := strings.Fields(strings.TrimSpace(line))
	statusCode := 0
	if len(parts) >= 2 {
		statusCode, _ = strconv.Atoi(parts[1])
	}
	result.StatusCode = statusCode
	result.ConnectOK = statusCode >= 200 && statusCode < 500
	result.LatencyMs = time.Since(start).Milliseconds()
	result.CheckedAtUnix = time.Now().UnixMilli()
	if !result.ConnectOK {
		result.Error = fmt.Sprintf("unexpected HTTP status %d", statusCode)
	}
	return result
}

func detectIP(useProxy bool, localPort int) IPDetectResult {
	result := IPDetectResult{
		Source:        "unknown",
		UsedProxy:     useProxy,
		CheckedAtUnix: time.Now().UnixMilli(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	transport := &http.Transport{}
	if useProxy {
		proxyAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort))
		if conn, err := net.DialTimeout("tcp", proxyAddr, 900*time.Millisecond); err != nil {
			result.Error = fmt.Sprintf("proxy core not listening on %s: %v", proxyAddr, err)
			return result
		} else {
			_ = conn.Close()
		}
		dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		transport.DialContext = func(_ context.Context, network string, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   6 * time.Second,
	}
	endpoints := []struct {
		source string
		url    string
	}{
		{source: "api.ip.sb", url: "https://api.ip.sb/geoip"},
		{source: "ipapi.co", url: "https://ipapi.co/json/"},
		{source: "ipinfo.io", url: "https://ipinfo.io/json"},
		{source: "api64.ipify.org", url: "https://api64.ipify.org?format=json"},
		{source: "ifconfig.me", url: "https://ifconfig.me/ip"},
	}
	var errs []string
	for _, ep := range endpoints {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ep.url, nil)
		resp, err := client.Do(req)
		if err != nil {
			errs = append(errs, ep.source+": "+err.Error())
			continue
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
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
		result.Source = ep.source
		result.IP = ip
		result.Country = country
		result.Region = region
		result.ISP = isp
		result.Error = ""
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
