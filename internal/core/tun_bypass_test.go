package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCIDRsFromYAMLFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cidrs.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(`
payload:
  - 1.1.1.0/24
  - 1.1.1.0/24
  - 2001:db8::/32
  - not-a-cidr
`)+"\n"), 0o644); err != nil {
		t.Fatalf("write temp yaml: %v", err)
	}

	v4, err := parseCIDRsFromYAMLFile(path, false)
	if err != nil {
		t.Fatalf("parse v4: %v", err)
	}
	if len(v4) != 1 || v4[0] != "1.1.1.0/24" {
		t.Fatalf("unexpected v4: %#v", v4)
	}

	v6, err := parseCIDRsFromYAMLFile(path, true)
	if err != nil {
		t.Fatalf("parse v6: %v", err)
	}
	if len(v6) != 1 || v6[0] != "2001:db8::/32" {
		t.Fatalf("unexpected v6: %#v", v6)
	}
}

func TestChnCIDRURLCandidates(t *testing.T) {
	v4, v6 := chnCIDRURLCandidates(" https://example.com/ipv4.yaml ", "")
	if len(v4) == 0 || strings.TrimSpace(v4[0]) != "https://example.com/ipv4.yaml" {
		t.Fatalf("unexpected v4 candidates: %#v", v4)
	}
	if len(v6) != 0 {
		t.Fatalf("unexpected v6 candidates: %#v", v6)
	}
}

func TestFetchCIDRsFromAny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bad":
			http.Error(w, "bad", http.StatusInternalServerError)
			return
		case "/v4":
			_, _ = w.Write([]byte("payload:\n  - 2.2.2.0/24\n"))
			return
		case "/v6":
			_, _ = w.Write([]byte("payload:\n  - 2001:db8:1::/48\n"))
			return
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cidrs, used, err := fetchCIDRsFromAny(ctx, []string{srv.URL + "/bad", srv.URL + "/v4"}, cacheDir, time.Hour, &http.Client{Timeout: time.Second}, false)
	if err != nil {
		t.Fatalf("fetch v4: %v", err)
	}
	if used != srv.URL+"/v4" {
		t.Fatalf("unexpected used url: %q", used)
	}
	if len(cidrs) != 1 || cidrs[0] != "2.2.2.0/24" {
		t.Fatalf("unexpected cidrs: %#v", cidrs)
	}

	cidrs6, used6, err := fetchCIDRsFromAny(ctx, []string{srv.URL + "/v6"}, cacheDir, time.Hour, &http.Client{Timeout: time.Second}, true)
	if err != nil {
		t.Fatalf("fetch v6: %v", err)
	}
	if used6 != srv.URL+"/v6" {
		t.Fatalf("unexpected used6 url: %q", used6)
	}
	if len(cidrs6) != 1 || cidrs6[0] != "2001:db8:1::/48" {
		t.Fatalf("unexpected cidrs6: %#v", cidrs6)
	}
}
