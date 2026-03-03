package core

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type endpointTuple struct {
	Host string
	Port string
}

func parseEndpoint(raw string) endpointTuple {
	v := strings.TrimSpace(raw)
	if v == "" {
		return endpointTuple{}
	}
	if host, port, err := net.SplitHostPort(v); err == nil {
		return endpointTuple{Host: strings.TrimSpace(host), Port: strings.TrimSpace(port)}
	}
	// Best effort fallback for non-standard strings.
	if i := strings.LastIndex(v, ":"); i > 0 && i < len(v)-1 && !strings.Contains(v[i+1:], "]") {
		return endpointTuple{Host: strings.TrimSpace(v[:i]), Port: strings.TrimSpace(v[i+1:])}
	}
	return endpointTuple{Host: v}
}

func terminateActiveConnection(network, source, destination string) error {
	src := parseEndpoint(source)
	dst := parseEndpoint(destination)
	if strings.TrimSpace(dst.Host) == "" {
		return fmt.Errorf("invalid destination: %s", destination)
	}
	netw := strings.ToLower(strings.TrimSpace(network))
	if netw == "" {
		netw = "tcp"
	}

	switch runtime.GOOS {
	case "darwin":
		// Kill PF state by source+destination host pair.
		return runCmdWithTimeout(4*time.Second, "pfctl", "-k", src.Host, "-k", dst.Host)
	case "linux":
		if strings.TrimSpace(dst.Port) == "" {
			return fmt.Errorf("destination port is required on linux: %s", destination)
		}
		args := []string{"-K", "dst", dst.Host, "dport", "=", dst.Port}
		if strings.TrimSpace(src.Host) != "" {
			args = append(args, "src", src.Host)
		}
		return runCmdWithTimeout(4*time.Second, "ss", args...)
	case "windows":
		if netw != "tcp" || strings.TrimSpace(dst.Port) == "" {
			return fmt.Errorf("windows close-connection currently supports tcp with explicit port only")
		}
		script := fmt.Sprintf(
			`Get-NetTCPConnection -RemoteAddress '%s' -RemotePort %s -ErrorAction SilentlyContinue | Close-NetTCPConnection -ErrorAction SilentlyContinue`,
			escapePowershellSingleQuote(dst.Host),
			dst.Port,
		)
		return runCmdWithTimeout(5*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	default:
		return fmt.Errorf("close connection is not supported on %s", runtime.GOOS)
	}
}

func runCmdWithTimeout(timeout time.Duration, name string, args ...string) error {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

func escapePowershellSingleQuote(v string) string {
	return strings.ReplaceAll(v, "'", "''")
}
