package core

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	sudokukey "github.com/SUDOKU-ASCII/sudoku/pkg/crypto"
)

func TestEasyInstallShortLinkSplitPrivateKeyInterop(t *testing.T) {
	pair, err := sudokukey.GenerateMasterKey()
	if err != nil {
		t.Fatalf("generate master key: %v", err)
	}
	splitKey, err := sudokukey.SplitPrivateKey(pair.Private)
	if err != nil {
		t.Fatalf("split private key: %v", err)
	}
	payload := shortLinkPayload{
		Host:            "127.0.0.1",
		Port:            443,
		Key:             splitKey,
		ASCII:           "ascii",
		AEAD:            "chacha20-poly1305",
		DisableHTTPMask: true,
	}
	link := encodeShortLinkForTest(t, payload)

	node, err := ParseShortLink(link)
	if err != nil {
		t.Fatalf("parse short link: %v", err)
	}
	if node.Key != splitKey {
		t.Fatalf("expected split private key %q, got %q", splitKey, node.Key)
	}

	roundTripLink, err := BuildShortLink(*node)
	if err != nil {
		t.Fatalf("build short link: %v", err)
	}
	roundTripPayload := decodeShortLinkForTest(t, roundTripLink)
	if roundTripPayload.Key != splitKey {
		t.Fatalf("expected exported short link to keep split private key")
	}

	runtimeCfg, err := buildSudokuClientConfig(*node)
	if err != nil {
		t.Fatalf("build runtime config: %v", err)
	}
	if runtimeCfg.Key != splitKey {
		t.Fatalf("expected runtime config to keep split private key, got %q", runtimeCfg.Key)
	}

	probeCfg, err := buildSudokuClientConfig(*node)
	if err != nil {
		t.Fatalf("build probe config: %v", err)
	}
	if probeCfg.Key != splitKey {
		t.Fatalf("expected probe config to keep split private key, got %q", probeCfg.Key)
	}
}

func TestDirectionalASCIIModeRoundTrip(t *testing.T) {
	node := NodeConfig{
		ID:                 "node_test",
		Name:               "directional",
		ServerAddress:      "example.com:443",
		Key:                "split-private-key",
		AEAD:               "chacha20-poly1305",
		ASCII:              "up_ascii_down_entropy",
		PaddingMin:         5,
		PaddingMax:         15,
		EnablePureDownlink: true,
		HTTPMask: HTTPMaskSettings{
			Mode: "auto",
			TLS:  true,
		},
		Multiplex: "auto",
	}

	link, err := BuildShortLink(node)
	if err != nil {
		t.Fatalf("build short link: %v", err)
	}
	payload := decodeShortLinkForTest(t, link)
	if payload.ASCII != "up_ascii_down_entropy" {
		t.Fatalf("expected directional ascii in short link, got %q", payload.ASCII)
	}

	parsed, err := ParseShortLink(link)
	if err != nil {
		t.Fatalf("parse short link: %v", err)
	}
	if parsed.ASCII != "up_ascii_down_entropy" {
		t.Fatalf("expected parsed directional ascii, got %q", parsed.ASCII)
	}

	runtimeCfg, err := buildSudokuClientConfig(*parsed)
	if err != nil {
		t.Fatalf("build runtime config: %v", err)
	}
	if runtimeCfg.ASCII != "up_ascii_down_entropy" {
		t.Fatalf("expected runtime directional ascii, got %q", runtimeCfg.ASCII)
	}
}

func encodeShortLinkForTest(t *testing.T, payload shortLinkPayload) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal short link payload: %v", err)
	}
	return "sudoku://" + base64.RawURLEncoding.EncodeToString(raw)
}

func decodeShortLinkForTest(t *testing.T, link string) shortLinkPayload {
	t.Helper()
	raw := strings.TrimPrefix(link, "sudoku://")
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode short link: %v", err)
	}
	var payload shortLinkPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		t.Fatalf("unmarshal short link payload: %v", err)
	}
	return payload
}

func TestParseShortLinkUsesNodeName(t *testing.T) {
	link := encodeShortLinkForTest(t, shortLinkPayload{Name: "HKG", Host: "203.0.113.7", Port: 443, Key: "k"})
	node, err := ParseShortLink(link)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if node.Name != "HKG" {
		t.Fatalf("expected name HKG, got %q", node.Name)
	}
	rt, err := BuildShortLink(*node)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := decodeShortLinkForTest(t, rt).Name; got != "HKG" {
		t.Fatalf("expected round-trip name HKG, got %q", got)
	}

	noName := encodeShortLinkForTest(t, shortLinkPayload{Host: "203.0.113.7", Port: 443, Key: "k"})
	node, err = ParseShortLink(noName)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if node.Name != "203.0.113.7" {
		t.Fatalf("expected host fallback name, got %q", node.Name)
	}
}

func TestParseShortLinkFromUserSample(t *testing.T) {
	node, err := ParseShortLink("sudoku://eyJhIjoiZW50cm9weSIsImUiOiJjaGFjaGEyMC1wb2x5MTMwNSIsImgiOiIyMTYuMjM2LjYzLjEzNSIsImhkIjp0cnVlLCJoeCI6Im9uIiwiaHkiOiJxaHp3bXJ2dCIsImsiOiI3NWJmNGUwNGQ1Njk5NDQyOGFlZGM2YWUxODM3OGFjMzRkZDQ0MmM3YTQyNjNmNzQxNDM5YmU2Y2I3ODkwZDBlMWNkNGRiZjEyMjg3ZDQxYTM4MDFlODUwMjIyYWQzYWQ1YTdlN2M4NTFlZThkNTgxYzRlYzQyNDZjMjQ1NWUwOCIsIm4iOiJIS0ciLCJwIjoxMDIzMywieCI6dHJ1ZX0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if node.Name != "HKG" {
		t.Fatalf("expected HKG, got %q", node.Name)
	}
}
