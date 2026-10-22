package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)
var logxLineRegex = regexp.MustCompile(`^\s*(\d{2}:\d{2}:\d{2})\s+(debug|info|warn|warning|error)\b(?:\s+\[([^\]]+)\])?\s*(.*)\s*$`)
var leadingBracketComponentRegex = regexp.MustCompile(`^\[([^\]]+)\]\s*`)

type parsedLogLine struct {
	Timestamp string
	Level     string
	Component string
	Message   string
}

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

func parseLogxLine(line string) (parsedLogLine, bool) {
	clean := stripANSI(strings.TrimSpace(line))
	m := logxLineRegex.FindStringSubmatch(clean)
	if len(m) != 5 {
		return parsedLogLine{}, false
	}
	return parsedLogLine{
		Timestamp: strings.TrimSpace(m[1]),
		Level:     strings.ToLower(strings.TrimSpace(m[2])),
		Component: strings.TrimSpace(m[3]),
		Message:   strings.TrimSpace(m[4]),
	}, true
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
	return writeFileAtomic(path, buf, 0o600)
}

func levelFromLine(line string) string {
	if parsed, ok := parseLogxLine(line); ok && parsed.Level != "" {
		switch parsed.Level {
		case "debug":
			return "debug"
		case "warn", "warning":
			return "warn"
		case "error":
			return "error"
		default:
			return "info"
		}
	}
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
	if parsed, ok := parseLogxLine(line); ok {
		if parsed.Component != "" {
			return parsed.Component
		}
		return "core"
	}
	clean := stripANSI(line)
	l := strings.Index(clean, "[")
	r := strings.Index(clean, "]")
	if l >= 0 && r > l {
		return strings.TrimSpace(clean[l+1 : r])
	}
	return "core"
}

func trimComponentPrefix(message, component string) string {
	msg := strings.TrimSpace(stripANSI(message))
	comp := strings.TrimSpace(component)
	if msg == "" || comp == "" {
		return msg
	}
	if m := leadingBracketComponentRegex.FindStringSubmatch(msg); len(m) == 2 {
		if strings.EqualFold(strings.TrimSpace(m[1]), comp) {
			return strings.TrimSpace(msg[len(m[0]):])
		}
	}
	prefix := comp + ":"
	if len(msg) >= len(prefix) && strings.EqualFold(msg[:len(prefix)], prefix) {
		return strings.TrimSpace(msg[len(prefix):])
	}
	return msg
}

// ErrAdminRequired reports that an operation needs administrator/root rights.
var ErrAdminRequired = errors.New("administrator privileges required")

func strconvBase36(v int64) string { return strconv.FormatInt(v, 36) }
