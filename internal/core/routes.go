package core

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

type routeContext struct {
	DefaultGateway string
	ServerIP       string
	TunIndex       int
}

func setupRoutes(activeNode NodeConfig, tun TunSettings, logf func(string)) (*routeContext, error) {
	ctx := &routeContext{}
	host, _, err := net.SplitHostPort(strings.TrimSpace(activeNode.ServerAddress))
	if err == nil {
		ips, _ := net.LookupIP(host)
		for _, ip := range ips {
			if v4 := ip.To4(); v4 != nil {
				ctx.ServerIP = v4.String()
				break
			}
		}
		if ctx.ServerIP == "" && len(ips) > 0 {
			ctx.ServerIP = ips[0].String()
		}
	}
	switch runtime.GOOS {
	case "linux":
		return setupRoutesLinux(ctx, tun, logf)
	case "darwin":
		return setupRoutesDarwin(ctx, tun, logf)
	case "windows":
		return setupRoutesWindows(ctx, tun, logf)
	default:
		return nil, nil
	}
}

func teardownRoutes(ctx *routeContext, tun TunSettings, logf func(string)) {
	if ctx == nil {
		return
	}
	switch runtime.GOOS {
	case "linux":
		teardownRoutesLinux(ctx, tun, logf)
	case "darwin":
		teardownRoutesDarwin(ctx, tun, logf)
	case "windows":
		teardownRoutesWindows(ctx, tun, logf)
	}
}

func setupRoutesLinux(ctx *routeContext, tun TunSettings, logf func(string)) (*routeContext, error) {
	cmds := make([][]string, 0, 16)
	if ctx.ServerIP != "" {
		if ip := net.ParseIP(ctx.ServerIP); ip != nil && ip.To4() != nil {
			cmds = append(cmds, []string{"ip", "rule", "add", "to", ctx.ServerIP, "lookup", "main", "pref", "5"})
		} else {
			cmds = append(cmds, []string{"ip", "-6", "rule", "add", "to", ctx.ServerIP, "lookup", "main", "pref", "5"})
		}
	}
	cmds = append(cmds,
		[]string{"sysctl", "-w", "net.ipv4.conf.all.rp_filter=0"},
		[]string{"sysctl", "-w", fmt.Sprintf("net.ipv4.conf.%s.rp_filter=0", tun.InterfaceName)},
		[]string{"ip", "rule", "add", "fwmark", strconv.Itoa(tun.SocksMark), "lookup", "main", "pref", "10"},
		[]string{"ip", "-6", "rule", "add", "fwmark", strconv.Itoa(tun.SocksMark), "lookup", "main", "pref", "10"},
		[]string{"ip", "route", "add", "default", "dev", tun.InterfaceName, "table", strconv.Itoa(tun.RouteTable)},
		[]string{"ip", "rule", "add", "lookup", strconv.Itoa(tun.RouteTable), "pref", "20"},
		[]string{"ip", "-6", "route", "add", "default", "dev", tun.InterfaceName, "table", strconv.Itoa(tun.RouteTable)},
		[]string{"ip", "-6", "rule", "add", "lookup", strconv.Itoa(tun.RouteTable), "pref", "20"},
	)
	for _, cmd := range cmds {
		if err := runCmd(logf, cmd[0], cmd[1:]...); err != nil {
			return nil, err
		}
	}
	return ctx, nil
}

func teardownRoutesLinux(ctx *routeContext, tun TunSettings, logf func(string)) {
	cmds := make([][]string, 0, 16)
	if ctx != nil && ctx.ServerIP != "" {
		if ip := net.ParseIP(ctx.ServerIP); ip != nil && ip.To4() != nil {
			cmds = append(cmds, []string{"ip", "rule", "del", "to", ctx.ServerIP, "lookup", "main", "pref", "5"})
		} else {
			cmds = append(cmds, []string{"ip", "-6", "rule", "del", "to", ctx.ServerIP, "lookup", "main", "pref", "5"})
		}
	}
	cmds = append(cmds,
		[]string{"ip", "rule", "del", "fwmark", strconv.Itoa(tun.SocksMark), "lookup", "main", "pref", "10"},
		[]string{"ip", "-6", "rule", "del", "fwmark", strconv.Itoa(tun.SocksMark), "lookup", "main", "pref", "10"},
		[]string{"ip", "rule", "del", "lookup", strconv.Itoa(tun.RouteTable), "pref", "20"},
		[]string{"ip", "-6", "rule", "del", "lookup", strconv.Itoa(tun.RouteTable), "pref", "20"},
		[]string{"ip", "route", "del", "default", "dev", tun.InterfaceName, "table", strconv.Itoa(tun.RouteTable)},
		[]string{"ip", "-6", "route", "del", "default", "dev", tun.InterfaceName, "table", strconv.Itoa(tun.RouteTable)},
	)
	for _, cmd := range cmds {
		_ = runCmd(logf, cmd[0], cmd[1:]...)
	}
}

func setupRoutesDarwin(ctx *routeContext, tun TunSettings, logf func(string)) (*routeContext, error) {
	gw, err := darwinDefaultGateway()
	if err != nil {
		return nil, err
	}
	ctx.DefaultGateway = gw
	if ctx.ServerIP != "" {
		if err := runCmd(logf, "route", "-n", "add", "-host", ctx.ServerIP, gw); err != nil {
			return nil, err
		}
	}
	if err := runCmd(logf, "route", "-n", "change", "default", "-interface", tun.InterfaceName); err != nil {
		return nil, err
	}
	_ = runCmd(logf, "route", "-n", "change", "-inet6", "default", "-interface", tun.InterfaceName)
	return ctx, nil
}

func teardownRoutesDarwin(ctx *routeContext, _ TunSettings, logf func(string)) {
	if ctx.DefaultGateway != "" {
		_ = runCmd(logf, "route", "-n", "change", "default", ctx.DefaultGateway)
	}
	if ctx.ServerIP != "" && ctx.DefaultGateway != "" {
		_ = runCmd(logf, "route", "-n", "delete", "-host", ctx.ServerIP, ctx.DefaultGateway)
	}
}

func setupRoutesWindows(ctx *routeContext, tun TunSettings, logf func(string)) (*routeContext, error) {
	gw, err := windowsDefaultGateway()
	if err != nil {
		return nil, err
	}
	ctx.DefaultGateway = gw
	idx, err := windowsInterfaceIndex(tun.InterfaceName)
	if err != nil {
		return nil, err
	}
	ctx.TunIndex = idx
	if ctx.ServerIP != "" {
		if err := runCmd(logf, "route", "add", ctx.ServerIP, "mask", "255.255.255.255", gw); err != nil {
			return nil, err
		}
	}
	if err := runCmd(logf, "route", "change", "0.0.0.0", "mask", "0.0.0.0", "0.0.0.0", "if", strconv.Itoa(idx)); err != nil {
		return nil, err
	}
	return ctx, nil
}

func teardownRoutesWindows(ctx *routeContext, _ TunSettings, logf func(string)) {
	if ctx.DefaultGateway != "" {
		_ = runCmd(logf, "route", "change", "0.0.0.0", "mask", "0.0.0.0", ctx.DefaultGateway)
	}
	if ctx.ServerIP != "" {
		_ = runCmd(logf, "route", "delete", ctx.ServerIP)
	}
}

func runCmd(logf func(string), name string, args ...string) error {
	if runtime.GOOS == "linux" && os.Geteuid() != 0 {
		if _, err := exec.LookPath("pkexec"); err == nil {
			return runCmdExec(logf, "pkexec", append([]string{name}, args...)...)
		}
	}
	if runtime.GOOS == "darwin" && os.Geteuid() != 0 {
		return runCmdDarwinAdmin(logf, name, args...)
	}
	return runCmdExec(logf, name, args...)
}

func runCmdExec(logf func(string), name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	clean := strings.TrimSpace(string(output))
	if logf != nil {
		if clean != "" {
			logf(fmt.Sprintf("[route] %s %s => %s", name, strings.Join(args, " "), clean))
		} else {
			logf(fmt.Sprintf("[route] %s %s", name, strings.Join(args, " ")))
		}
	}
	if err != nil {
		return fmt.Errorf("run %s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func runCmdDarwinAdmin(logf func(string), name string, args ...string) error {
	cmdline := shellJoin(append([]string{name}, args...)...)
	script := fmt.Sprintf(`do shell script "%s" with administrator privileges`, appleScriptEscape(cmdline))
	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	clean := strings.TrimSpace(string(output))
	if logf != nil {
		if clean != "" {
			logf(fmt.Sprintf("[route] sudo %s %s => %s", name, strings.Join(args, " "), clean))
		} else {
			logf(fmt.Sprintf("[route] sudo %s %s", name, strings.Join(args, " ")))
		}
	}
	if err != nil {
		return fmt.Errorf("run %s %s (admin): %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func shellJoin(args ...string) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$&;|<>*?()[]{}!") {
		return s
	}
	// Single-quote with proper escaping: ' -> '"'"'
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func appleScriptEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

func darwinDefaultGateway() (string, error) {
	cmd := exec.Command("route", "-n", "get", "default")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	s := bufio.NewScanner(strings.NewReader(string(output)))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "gateway:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "gateway:")), nil
		}
	}
	return "", errors.New("gateway not found")
}

func windowsDefaultGateway() (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-Command", "(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Sort-Object RouteMetric | Select-Object -First 1).NextHop")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	gw := strings.TrimSpace(string(output))
	if gw == "" {
		return "", errors.New("windows default gateway not found")
	}
	return gw, nil
}

func windowsInterfaceIndex(name string) (int, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf("(Get-NetIPInterface -AddressFamily IPv4 -InterfaceAlias '%s' | Select-Object -First 1).InterfaceIndex", strings.ReplaceAll(name, "'", "''")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, err
	}
	re := regexp.MustCompile(`\d+`)
	m := re.FindString(string(output))
	if m == "" {
		return 0, errors.New("interface index not found")
	}
	idx, err := strconv.Atoi(m)
	if err != nil {
		return 0, err
	}
	return idx, nil
}
