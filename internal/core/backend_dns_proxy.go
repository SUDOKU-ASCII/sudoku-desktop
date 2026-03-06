package core

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"
)

type tunDNSRuntime struct {
	proxy            *dnsProxyServer
	systemDNSAddress string
	healthAddr       string
	upstreamAddr     string
}

func (b *Backend) prepareTunDNSRuntime(ctx context.Context, cfg *AppConfig, localPort int) (*tunDNSRuntime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	tunCfg := cfg.Tun
	mapDNSHost := strings.TrimSpace(tunCfg.MapDNSAddress)
	if !tunCfg.MapDNSEnabled || mapDNSHost == "" || tunCfg.MapDNSPort <= 0 {
		return &tunDNSRuntime{}, nil
	}

	upstreamAddr := net.JoinHostPort(mapDNSHost, fmt.Sprintf("%d", tunCfg.MapDNSPort))
	out := &tunDNSRuntime{
		systemDNSAddress: mapDNSHost,
		healthAddr:       upstreamAddr,
		upstreamAddr:     upstreamAddr,
	}

	if runtime.GOOS != "darwin" || strings.ToLower(strings.TrimSpace(cfg.Routing.ProxyMode)) != "pac" {
		return out, nil
	}

	directDialer, err := b.newTunDNSDirectDialer()
	if err != nil {
		b.addLog("warn", "dns", fmt.Sprintf("split DNS skipped: outbound bypass unavailable: %v", err))
		return out, nil
	}

	cnRules := b.loadTunCNRules(ctx, cfg, localPort, directDialer)
	if cnRules == nil {
		b.addLog("warn", "dns", "split DNS skipped: PAC direct domain rules unavailable")
		return out, nil
	}

	proxy := newDNSProxyServer(dnsProxyConfig{
		CNRules:    cnRules,
		MapDNSAddr: upstreamAddr,
		PreferIPv4: true,
		DirectDial: directDialer.DialContext,
		Logf: func(line string) {
			b.addLog("info", "dns", line)
		},
	})
	if err := proxy.Start(); err != nil {
		return nil, err
	}

	out.proxy = proxy
	out.systemDNSAddress = localLoopbackIPv4
	out.healthAddr = net.JoinHostPort(localLoopbackIPv4, "53")
	b.addLog("info", "dns", fmt.Sprintf("darwin split DNS enabled: CN domains direct, others via HEV MapDNS %s", upstreamAddr))
	return out, nil
}

func (b *Backend) newTunDNSDirectDialer() (*net.Dialer, error) {
	cfg := outboundBypassConfig{}
	switch runtime.GOOS {
	case "darwin":
		ifName, err := darwinResolveOutboundBypassInterface(2 * time.Second)
		if err != nil {
			return nil, err
		}
		ifName = strings.TrimSpace(ifName)
		if ifName == "" {
			return nil, fmt.Errorf("default physical interface not found")
		}
		cfg.DarwinInterface = ifName
	case "linux":
		if srcIP, err := linuxDefaultOutboundIPv4(); err == nil && strings.TrimSpace(srcIP) != "" {
			cfg.LinuxSourceIP = strings.TrimSpace(srcIP)
		}
	case "windows":
		if ifIndex, err := windowsDefaultInterfaceIndex(); err == nil && ifIndex > 0 {
			cfg.WindowsIfIndex = ifIndex
		}
	}
	return newOutboundBypassDialer(3*time.Second, cfg), nil
}

func (b *Backend) loadTunCNRules(ctx context.Context, cfg *AppConfig, localPort int, directDialer *net.Dialer) *cnRuleSet {
	if cfg == nil {
		return nil
	}

	var cnRules *cnRuleSet
	socksAddr := net.JoinHostPort(localLoopbackIPv4, fmt.Sprintf("%d", localPort))
	if err := waitForTCPReady(ctx, socksAddr, 4*time.Second); err == nil {
		if httpc, herr := newHTTPClientViaSOCKS5(socksAddr, 20*time.Second); herr == nil {
			cnRules, _ = prepareCNRules(ctx, b.store, cfg, httpc, func(line string) {
				b.addLog("info", "rule", line)
			})
		}
	}

	if cnRules == nil || (len(cnRules.domainExact) == 0 && len(cnRules.domainSuffix) == 0) {
		client := &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				DialContext: directDialer.DialContext,
			},
		}
		cnRules, _ = prepareCNRules(ctx, b.store, cfg, client, func(line string) {
			b.addLog("info", "rule", line)
		})
	}

	if cnRules != nil && len(cnRules.domainExact) == 0 && len(cnRules.domainSuffix) == 0 {
		return nil
	}
	return cnRules
}
