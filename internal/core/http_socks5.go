package core

import (
	"context"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/proxy"
)

func newHTTPClientViaSOCKS5(proxyAddr string, timeout time.Duration) (*http.Client, error) {
	d, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// The SOCKS5 dialer doesn't support contexts. Rely on the client timeout.
			return d.Dial(network, addr)
		},
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
	}, nil
}
