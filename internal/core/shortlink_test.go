package core

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	sudokuapis "github.com/saba-futai/sudoku/apis"
	sudokukey "github.com/saba-futai/sudoku/pkg/crypto"
	sudokutable "github.com/saba-futai/sudoku/pkg/obfs/sudoku"
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
	publicKey := sudokukey.EncodePoint(pair.Public)

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

	appCfg := &AppConfig{
		Core: CoreSettings{
			LocalPort: 1080,
		},
		Routing: RoutingSettings{
			ProxyMode: "global",
		},
	}
	runtimeCfg, err := buildSudokuClientConfig(appCfg, *node, "", false)
	if err != nil {
		t.Fatalf("build runtime config: %v", err)
	}
	if runtimeCfg.Key != splitKey {
		t.Fatalf("expected runtime config to keep split private key, got %q", runtimeCfg.Key)
	}

	if got := tableSeedKey(node.Key); got != publicKey {
		t.Fatalf("expected probe table seed public key %q, got %q", publicKey, got)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	serverTable := sudokutable.NewTable(publicKey, normalizeASCII(node.ASCII))
	if serverTable == nil {
		t.Fatalf("build server table failed")
	}
	serverCfg := sudokuapis.DefaultConfig()
	serverCfg.Key = publicKey
	serverCfg.AEADMethod = node.AEAD
	serverCfg.Table = serverTable
	serverCfg.PaddingMin = node.PaddingMin
	serverCfg.PaddingMax = node.PaddingMax
	serverCfg.EnablePureDownlink = node.EnablePureDownlink
	serverCfg.DisableHTTPMask = true
	serverCfg.HandshakeTimeoutSeconds = 3

	type handshakeResult struct {
		target   string
		userHash string
		err      error
	}
	resultCh := make(chan handshakeResult, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			resultCh <- handshakeResult{err: err}
			return
		}
		defer conn.Close()

		tunnelConn, targetAddr, userHash, err := sudokuapis.ServerHandshakeWithUserHash(conn, serverCfg)
		if tunnelConn != nil {
			defer tunnelConn.Close()
		}
		resultCh <- handshakeResult{
			target:   targetAddr,
			userHash: userHash,
			err:      err,
		}
	}()

	node.ServerAddress = listener.Addr().String()
	probeCfg, err := buildProtocolConfig(*node, "example.com:443")
	if err != nil {
		t.Fatalf("build probe config: %v", err)
	}
	if probeCfg.Key != splitKey {
		t.Fatalf("expected probe config to keep split private key, got %q", probeCfg.Key)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := sudokuapis.Dial(ctx, probeCfg)
	if err != nil {
		t.Fatalf("dial via probe config: %v", err)
	}
	_ = conn.Close()

	result := <-resultCh
	if result.err != nil {
		t.Fatalf("server handshake failed: %v", result.err)
	}
	if result.target != "example.com:443" {
		t.Fatalf("unexpected target address: %q", result.target)
	}

	wantUserHash := expectedSplitKeyUserHash(t, splitKey)
	if result.userHash != wantUserHash {
		t.Fatalf("unexpected user hash: got %q want %q", result.userHash, wantUserHash)
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

func expectedSplitKeyUserHash(t *testing.T, splitKey string) string {
	t.Helper()
	keyBytes, err := hex.DecodeString(splitKey)
	if err != nil {
		t.Fatalf("decode split key: %v", err)
	}
	sum := sha256.Sum256(keyBytes)
	return hex.EncodeToString(sum[:8])
}
