package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Runs the real upstream core against a throw-away config; set SUDOKU_BIN to enable.
func TestKernelControllerSmoke(t *testing.T) {
	bin := os.Getenv("SUDOKU_BIN")
	if bin == "" {
		t.Skip("SUDOKU_BIN not set")
	}
	cfg := DefaultConfig(t.TempDir())
	cfg.Nodes = []NodeConfig{
		{ID: "a", Name: "one", ServerAddress: "127.0.0.1:9", Key: "k1", AEAD: "chacha20-poly1305", ASCII: "prefer_entropy", PaddingMin: 5, PaddingMax: 15},
		{ID: "b", Name: "one", ServerAddress: "127.0.0.1:9", Key: "k2", AEAD: "chacha20-poly1305", ASCII: "prefer_entropy", PaddingMin: 5, PaddingMax: 15},
	}
	cfg.Controller = ControllerSettings{Port: 19197, Secret: "smoke"}
	files, err := buildKernelConfigs(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		f.Config.LocalPort = 19233
	}
	dir := t.TempDir()
	paths, err := writeKernelConfigs(dir, files)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{}
	for _, p := range paths {
		args = append(args, "-c", p)
	}
	p := NewManagedProcess("sudoku")
	env := []string{"SUDOKU_LOG_LEVEL=info", "SUDOKU_RULE_CACHE_DIR=" + filepath.Join(dir, "rules")}
	if err := p.Start(bin, args, env, dir, func(l string) { t.Log(l) }); err != nil {
		t.Fatal(err)
	}
	defer p.Stop(2 * time.Second)
	c := newCtlClient(19197, "smoke")
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	v, err := c.WaitReady(ctx, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("version", v)
	for _, m := range []string{"global", "direct", "rule"} {
		if err := c.SetMode(ctx, m); err != nil {
			t.Fatalf("mode %s: %v", m, err)
		}
		if got, _ := c.Mode(ctx); got != m {
			t.Fatalf("mode readback %q != %q", got, m)
		}
	}
	px, err := c.Proxies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := nodeProxyNames(cfg.Nodes)
	for _, n := range names {
		if _, ok := px[n]; !ok {
			t.Fatalf("proxy %q missing; have %v", n, keysOf(px))
		}
	}
	if _, ok := px["Balance"]; !ok {
		t.Fatalf("Balance group missing: %v", keysOf(px))
	}
	if err := c.Select(ctx, "PROXY", names["b"]); err != nil {
		t.Fatal(err)
	}
	if err := c.Select(ctx, "PROXY", "Balance"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Connections(ctx); err != nil {
		t.Fatal(err)
	}
	got := make(chan ctlTraffic, 1)
	sctx, scancel := context.WithTimeout(ctx, 3*time.Second)
	defer scancel()
	go c.StreamTraffic(sctx, func(tr ctlTraffic) {
		select {
		case got <- tr:
		default:
		}
	})
	select {
	case <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("no traffic tick")
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
