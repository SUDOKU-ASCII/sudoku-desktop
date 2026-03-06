package core

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	sudokukey "github.com/saba-futai/sudoku/pkg/crypto"
)

type shortLinkPayload struct {
	Host            string   `json:"h"`
	Port            int      `json:"p"`
	Key             string   `json:"k"`
	ASCII           string   `json:"a,omitempty"`
	AEAD            string   `json:"e,omitempty"`
	MixPort         int      `json:"m,omitempty"`
	PackedDownlink  bool     `json:"x,omitempty"`
	CustomTable     string   `json:"t,omitempty"`
	CustomTables    []string `json:"ts,omitempty"`
	DisableHTTPMask bool     `json:"hd,omitempty"`
	HTTPMaskMode    string   `json:"hm,omitempty"`
	HTTPMaskTLS     bool     `json:"ht,omitempty"`
	HTTPMaskHost    string   `json:"hh,omitempty"`
	HTTPMaskMux     string   `json:"hx,omitempty"`
	HTTPMaskPath    string   `json:"hy,omitempty"`
}

func ParseShortLink(link string) (*NodeConfig, error) {
	if !strings.HasPrefix(link, "sudoku://") {
		return nil, errors.New("invalid scheme")
	}
	encoded := strings.TrimPrefix(link, "sudoku://")
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode short link: %w", err)
	}
	var payload shortLinkPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse short link: %w", err)
	}
	if payload.Host == "" || payload.Port <= 0 || payload.Key == "" {
		return nil, errors.New("short link missing required fields")
	}
	node := &NodeConfig{
		ID:                 newID("node_"),
		Name:               payload.Host,
		ServerAddress:      net.JoinHostPort(payload.Host, strconv.Itoa(payload.Port)),
		Key:                payload.Key,
		AEAD:               payload.AEAD,
		ASCII:              decodeASCII(payload.ASCII),
		PaddingMin:         5,
		PaddingMax:         15,
		EnablePureDownlink: !payload.PackedDownlink,
		CustomTable:        strings.TrimSpace(payload.CustomTable),
		CustomTables:       append([]string(nil), payload.CustomTables...),
		LocalPort:          payload.MixPort,
		Enabled:            true,
		HTTPMask: HTTPMaskSettings{
			Disable:   payload.DisableHTTPMask,
			Mode:      payload.HTTPMaskMode,
			TLS:       payload.HTTPMaskTLS,
			Host:      payload.HTTPMaskHost,
			PathRoot:  payload.HTTPMaskPath,
			Multiplex: payload.HTTPMaskMux,
		},
	}
	if node.LocalPort <= 0 {
		node.LocalPort = 1080
	}
	if node.AEAD == "" {
		node.AEAD = "chacha20-poly1305"
	}
	if node.ASCII == "" {
		node.ASCII = "prefer_entropy"
	}
	if node.HTTPMask.Mode == "" {
		node.HTTPMask.Mode = "legacy"
	}
	if node.HTTPMask.Multiplex == "" {
		node.HTTPMask.Multiplex = "off"
	}
	return node, nil
}

func BuildShortLink(node NodeConfig) (string, error) {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(node.ServerAddress))
	if err != nil {
		return "", fmt.Errorf("invalid serverAddress: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", fmt.Errorf("invalid server port: %w", err)
	}
	payload := shortLinkPayload{
		Host:            host,
		Port:            port,
		Key:             strings.TrimSpace(node.Key),
		ASCII:           encodeASCII(node.ASCII),
		AEAD:            strings.TrimSpace(node.AEAD),
		MixPort:         node.LocalPort,
		PackedDownlink:  !node.EnablePureDownlink,
		CustomTable:     strings.TrimSpace(node.CustomTable),
		CustomTables:    append([]string(nil), node.CustomTables...),
		DisableHTTPMask: node.HTTPMask.Disable,
		HTTPMaskMode:    strings.TrimSpace(node.HTTPMask.Mode),
		HTTPMaskTLS:     node.HTTPMask.TLS,
		HTTPMaskHost:    strings.TrimSpace(node.HTTPMask.Host),
		HTTPMaskMux:     strings.TrimSpace(node.HTTPMask.Multiplex),
		HTTPMaskPath:    strings.TrimSpace(node.HTTPMask.PathRoot),
	}
	if payload.AEAD == "" {
		payload.AEAD = "chacha20-poly1305"
	}
	if payload.MixPort <= 0 {
		payload.MixPort = 1080
	}
	if payload.HTTPMaskMode == "legacy" {
		payload.HTTPMaskMode = ""
	}
	if payload.HTTPMaskMux == "off" {
		payload.HTTPMaskMux = ""
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return "sudoku://" + base64.RawURLEncoding.EncodeToString(data), nil
}

func encodeASCII(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "ascii", "prefer_ascii":
		return "ascii"
	default:
		return "entropy"
	}
}

func decodeASCII(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "ascii", "prefer_ascii":
		return "prefer_ascii"
	default:
		return "prefer_entropy"
	}
}

func tableSeedKey(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ""
	}
	keyBytes, err := hex.DecodeString(trimmed)
	if err != nil || len(keyBytes) != 64 {
		return trimmed
	}
	pubKeyPoint, err := sudokukey.RecoverPublicKey(trimmed)
	if err != nil {
		return trimmed
	}
	return sudokukey.EncodePoint(pubKeyPoint)
}
