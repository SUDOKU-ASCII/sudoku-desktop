package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ctlClient talks to the Sudoku core's Clash-compatible external controller.
type ctlClient struct {
	base   string
	secret string
	hc     *http.Client // short requests
	sc     *http.Client // long-lived streams (no overall timeout)
}

type ctlError struct {
	Status  int
	Message string
}

func (e *ctlError) Error() string {
	return fmt.Sprintf("controller %d: %s", e.Status, e.Message)
}

func newCtlClient(port int, secret string) *ctlClient {
	// A dedicated transport per client: closing it on teardown releases idle sockets, which
	// the default transport would otherwise keep for the lifetime of the app.
	mk := func(timeout time.Duration) *http.Client {
		return &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy:               nil,
				DialContext:         (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     30 * time.Second,
			},
		}
	}
	return &ctlClient{
		base:   "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		secret: secret,
		hc:     mk(8 * time.Second),
		sc:     mk(0),
	}
}

func (c *ctlClient) Close() {
	for _, hc := range []*http.Client{c.hc, c.sc} {
		if tr, ok := hc.Transport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
	}
}

func (c *ctlClient) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var rd io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *ctlClient) do(ctx context.Context, method, path string, body any, out any) error {
	return c.doWith(ctx, c.hc, method, path, body, out)
}

func (c *ctlClient) doWith(ctx context.Context, hc *http.Client, method, path string, body any, out any) error {
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Message == "" {
			e.Message = strings.TrimSpace(string(raw))
		}
		return &ctlError{Status: resp.StatusCode, Message: e.Message}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (c *ctlClient) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	if err := c.do(ctx, http.MethodGet, "/version", nil, &v); err != nil {
		return "", err
	}
	return v.Version, nil
}

// WaitReady polls /version until the controller answers.
func (c *ctlClient) WaitReady(ctx context.Context, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var last error
	for {
		v, err := c.Version(ctx)
		if err == nil {
			return v, nil
		}
		var ce *ctlError
		if errors.As(err, &ce) && ce.Status == http.StatusUnauthorized {
			return "", err
		}
		last = err
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("controller not ready: %w", last)
		case <-time.After(80 * time.Millisecond):
		}
	}
}

func (c *ctlClient) Mode(ctx context.Context) (string, error) {
	var v struct {
		Mode string `json:"mode"`
	}
	if err := c.do(ctx, http.MethodGet, "/configs", nil, &v); err != nil {
		return "", err
	}
	return v.Mode, nil
}

// SetMode switches the core routing mode: rule | global | direct.
func (c *ctlClient) SetMode(ctx context.Context, mode string) error {
	return c.do(ctx, http.MethodPatch, "/configs", map[string]string{"mode": mode}, nil)
}

type ctlProxy struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Now     string   `json:"now"`
	All     []string `json:"all"`
	Alive   bool     `json:"alive"`
	History []struct {
		Delay int `json:"delay"`
	} `json:"history"`
}

func (c *ctlClient) Proxies(ctx context.Context) (map[string]ctlProxy, error) {
	var v struct {
		Proxies map[string]ctlProxy `json:"proxies"`
	}
	if err := c.do(ctx, http.MethodGet, "/proxies", nil, &v); err != nil {
		return nil, err
	}
	return v.Proxies, nil
}

func (c *ctlClient) Select(ctx context.Context, group, name string) error {
	return c.do(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), map[string]string{"name": name}, nil)
}

// Delay measures one proxy through the running core; returns milliseconds.
func (c *ctlClient) Delay(ctx context.Context, proxy, testURL string, timeout time.Duration) (int, error) {
	q := url.Values{}
	q.Set("timeout", strconv.Itoa(int(timeout/time.Millisecond)))
	if testURL != "" {
		q.Set("url", testURL)
	}
	var v struct {
		Delay int `json:"delay"`
	}
	hc := &http.Client{Timeout: timeout + 3*time.Second, Transport: c.hc.Transport}
	if err := c.doWith(ctx, hc, http.MethodGet, "/proxies/"+url.PathEscape(proxy)+"/delay?"+q.Encode(), nil, &v); err != nil {
		return 0, err
	}
	return v.Delay, nil
}

type ctlConnection struct {
	ID       string    `json:"id"`
	Upload   uint64    `json:"upload"`
	Download uint64    `json:"download"`
	Start    time.Time `json:"start"`
	Chains   []string  `json:"chains"`
	Rule     string    `json:"rule"`
	Metadata struct {
		Network         string `json:"network"`
		Type            string `json:"type"`
		SourceIP        string `json:"sourceIP"`
		SourcePort      string `json:"sourcePort"`
		DestinationIP   string `json:"destinationIP"`
		DestinationPort string `json:"destinationPort"`
		Host            string `json:"host"`
	} `json:"metadata"`
}

type ctlConnections struct {
	DownloadTotal uint64          `json:"downloadTotal"`
	UploadTotal   uint64          `json:"uploadTotal"`
	Memory        uint64          `json:"memory"`
	Connections   []ctlConnection `json:"connections"`
}

func (c *ctlClient) Connections(ctx context.Context) (*ctlConnections, error) {
	var v ctlConnections
	if err := c.do(ctx, http.MethodGet, "/connections", nil, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *ctlClient) CloseConnection(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/connections/"+url.PathEscape(id), nil, nil)
}

func (c *ctlClient) CloseAllConnections(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/connections", nil, nil)
}

type ctlTraffic struct {
	Up        uint64 `json:"up"`
	Down      uint64 `json:"down"`
	UpTotal   uint64 `json:"upTotal"`
	DownTotal uint64 `json:"downTotal"`
}

// StreamTraffic blocks, delivering one sample per second until ctx ends or the stream breaks.
func (c *ctlClient) StreamTraffic(ctx context.Context, fn func(ctlTraffic)) error {
	return c.stream(ctx, "/traffic", func(dec *json.Decoder) error {
		var t ctlTraffic
		if err := dec.Decode(&t); err != nil {
			return err
		}
		fn(t)
		return nil
	})
}

// StreamMemory delivers the core's Go runtime memory once per second.
func (c *ctlClient) StreamMemory(ctx context.Context, fn func(inuse uint64)) error {
	return c.stream(ctx, "/memory", func(dec *json.Decoder) error {
		var m struct {
			InUse uint64 `json:"inuse"`
		}
		if err := dec.Decode(&m); err != nil {
			return err
		}
		fn(m.InUse)
		return nil
	})
}

func (c *ctlClient) stream(ctx context.Context, path string, next func(*json.Decoder) error) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	resp, err := c.sc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &ctlError{Status: resp.StatusCode}
	}
	dec := json.NewDecoder(resp.Body)
	for {
		if err := next(dec); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}
