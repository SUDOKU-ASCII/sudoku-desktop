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
	"sort"
	"strconv"
	"strings"
	"time"

	sudokuapis "github.com/saba-futai/sudoku/apis"
	sudokutable "github.com/saba-futai/sudoku/pkg/obfs/sudoku"
	"golang.org/x/net/proxy"
)

func buildProtocolConfig(node NodeConfig, target string) (*sudokuapis.ProtocolConfig, error) {
	cfg := sudokuapis.DefaultConfig()
	cfg.ServerAddress = strings.TrimSpace(node.ServerAddress)
	cfg.TargetAddress = strings.TrimSpace(target)
	cfg.Key = strings.TrimSpace(node.Key)
	if cfg.Key == "" {
		return nil, fmt.Errorf("empty node key")
	}
	if node.AEAD != "" {
		cfg.AEADMethod = strings.TrimSpace(node.AEAD)
	}
	cfg.PaddingMin = node.PaddingMin
	cfg.PaddingMax = node.PaddingMax
	cfg.EnablePureDownlink = node.EnablePureDownlink
	cfg.DisableHTTPMask = node.HTTPMask.Disable
	cfg.HTTPMaskMode = strings.TrimSpace(node.HTTPMask.Mode)
	cfg.HTTPMaskTLSEnabled = node.HTTPMask.TLS
	cfg.HTTPMaskHost = strings.TrimSpace(node.HTTPMask.Host)
	cfg.HTTPMaskPathRoot = strings.TrimSpace(node.HTTPMask.PathRoot)
	cfg.HTTPMaskMultiplex = strings.TrimSpace(node.HTTPMask.Multiplex)
	if cfg.HTTPMaskMode == "" {
		cfg.HTTPMaskMode = "legacy"
	}
	if cfg.HTTPMaskMultiplex == "" {
		cfg.HTTPMaskMultiplex = "off"
	}
	if cfg.PaddingMax <= 0 {
		cfg.PaddingMax = 15
	}
	if cfg.PaddingMin > cfg.PaddingMax {
		cfg.PaddingMin = cfg.PaddingMax
	}

	ascii := normalizeASCII(node.ASCII)
	if len(node.CustomTables) > 0 {
		tables := make([]*sudokutable.Table, 0, len(node.CustomTables))
		for _, layout := range node.CustomTables {
			table, err := sudokutable.NewTableWithCustom(node.Key, ascii, layout)
			if err != nil {
				return nil, err
			}
			tables = append(tables, table)
		}
		cfg.Tables = tables
	} else if strings.TrimSpace(node.CustomTable) != "" {
		table, err := sudokutable.NewTableWithCustom(node.Key, ascii, strings.TrimSpace(node.CustomTable))
		if err != nil {
			return nil, err
		}
		cfg.Table = table
	} else {
		table := sudokutable.NewTable(node.Key, ascii)
		if table == nil {
			return nil, fmt.Errorf("build table failed")
		}
		cfg.Table = table
	}
	if err := cfg.ValidateClient(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func probeNodeLatency(node NodeConfig) LatencyResult {
	result := LatencyResult{
		NodeID:        node.ID,
		NodeName:      node.Name,
		LatencyMs:     -1,
		CheckedAtUnix: time.Now().UnixMilli(),
	}
	start := time.Now()
	cfg, err := buildProtocolConfig(node, "i.ytimg.com:443")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := sudokuapis.Dial(ctx, cfg)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer conn.Close()

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
		Source:        "api.ip.sb",
		UsedProxy:     useProxy,
		CheckedAtUnix: time.Now().UnixMilli(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	transport := &http.Transport{}
	if useProxy {
		dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)), nil, proxy.Direct)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		transport.DialContext = func(_ context.Context, network string, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	}
	client := &http.Client{Transport: transport}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ip.sb/geoip", nil)
	resp, err := client.Do(req)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	var payload struct {
		IP      string `json:"ip"`
		Country string `json:"country"`
		Region  string `json:"region"`
		ISP     string `json:"isp"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		result.Error = err.Error()
		return result
	}
	result.IP = payload.IP
	result.Country = payload.Country
	result.Region = payload.Region
	result.ISP = payload.ISP
	return result
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
