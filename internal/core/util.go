package core

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return prefix + hex.EncodeToString(b[:])
}

func cloneConfig(cfg *AppConfig) *AppConfig {
	if cfg == nil {
		return nil
	}
	buf, _ := json.Marshal(cfg)
	var out AppConfig
	_ = json.Unmarshal(buf, &out)
	return &out
}

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func writeJSONFile(path string, v any) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}

func readLinesPipe(ctx context.Context, r io.Reader, onLine func(string)) error {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1024), 1024*1024)
	for s.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		line := strings.TrimRight(s.Text(), "\r\n")
		if onLine != nil {
			onLine(line)
		}
	}
	if errors.Is(s.Err(), io.EOF) {
		return nil
	}
	return s.Err()
}

func levelFromLine(line string) string {
	clean := strings.ToLower(stripANSI(strings.TrimSpace(line)))
	parts := strings.Fields(clean)
	if len(parts) >= 2 {
		switch parts[1] {
		case "debug":
			return "debug"
		case "info":
			return "info"
		case "warn", "warning":
			return "warn"
		case "error":
			return "error"
		}
	}
	if strings.Contains(clean, " error ") || strings.HasPrefix(clean, "error") {
		return "error"
	}
	if strings.Contains(clean, " warn ") || strings.Contains(clean, " warning ") {
		return "warn"
	}
	if strings.Contains(clean, " debug ") {
		return "debug"
	}
	return "info"
}

func componentFromLine(line string) string {
	clean := stripANSI(line)
	l := strings.Index(clean, "[")
	r := strings.Index(clean, "]")
	if l >= 0 && r > l {
		return strings.TrimSpace(clean[l+1 : r])
	}
	return "core"
}

func isLikelyPermissionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "operation not permitted") ||
		strings.Contains(msg, "access is denied") ||
		strings.Contains(msg, "requires elevation") ||
		strings.Contains(msg, "administrator") ||
		strings.Contains(msg, "not permitted")
}
