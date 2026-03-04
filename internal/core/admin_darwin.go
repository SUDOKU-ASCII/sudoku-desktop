package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type darwinAdminDetachedProcess struct {
	pid     int
	pidFile string
	logFile string
	label   string

	expectCmdBase    string
	expectRuntimeDir string
	verifiedPID      int
}

func (p *darwinAdminDetachedProcess) PID() int { return p.pid }

func (p *darwinAdminDetachedProcess) pidMatchesExpected(pid int) bool {
	if pid <= 0 {
		return false
	}
	args, err := darwinProcessArgs(pid)
	if err != nil {
		return false
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return false
	}
	if base := strings.TrimSpace(p.expectCmdBase); base != "" && !strings.Contains(args, base) {
		return false
	}
	dir := strings.TrimSpace(p.expectRuntimeDir)
	if dir == "" && strings.TrimSpace(p.pidFile) != "" {
		dir = strings.TrimSpace(filepath.Dir(strings.TrimSpace(p.pidFile)))
	}
	if dir != "" && !strings.Contains(args, dir) {
		return false
	}
	return true
}

func (p *darwinAdminDetachedProcess) IsRunning() bool {
	if pidLooksAlive(p.pid) {
		if p.verifiedPID == p.pid {
			return true
		}
		if p.pidMatchesExpected(p.pid) {
			p.verifiedPID = p.pid
			return true
		}
	}

	p.pid = 0
	p.verifiedPID = 0

	if strings.TrimSpace(p.pidFile) == "" {
		return false
	}
	if pid, err := readPIDFile(p.pidFile); err == nil && pidLooksAlive(pid) && p.pidMatchesExpected(pid) {
		p.pid = pid
		p.verifiedPID = pid
		return true
	} else if err == nil && pidLooksAlive(pid) {
		// PID reuse can make the pidfile point to an unrelated process. Never treat that as "running".
		_ = os.Remove(p.pidFile)
	} else if err != nil {
		// Corrupt/partial pid file; drop it so future checks don't flap.
		_ = os.Remove(p.pidFile)
	}
	return false
}

func newAdminDetachedProcess() adminDetachedProcess {
	return &darwinAdminDetachedProcess{}
}

func (p *darwinAdminDetachedProcess) Start(command string, args []string, workdir string, pidFile string, logFile string) (int, error) {
	if pidFile == "" {
		return 0, errors.New("pidFile required")
	}
	if logFile == "" {
		return 0, errors.New("logFile required")
	}
	if err := ensureDir(filepath.Dir(pidFile)); err != nil {
		return 0, err
	}
	if err := ensureDir(filepath.Dir(logFile)); err != nil {
		return 0, err
	}
	p.expectCmdBase = strings.TrimSpace(filepath.Base(command))
	p.expectRuntimeDir = strings.TrimSpace(filepath.Dir(pidFile))
	if pidLooksAlive(p.pid) {
		if p.verifiedPID == p.pid || p.pidMatchesExpected(p.pid) {
			p.verifiedPID = p.pid
			return 0, fmt.Errorf("process already running (pid=%d)", p.pid)
		}
		p.pid = 0
		p.verifiedPID = 0
	}

	label := p.label
	if strings.TrimSpace(label) == "" {
		label = fmt.Sprintf("sudoku4x4.hev.%d", os.Getuid())
	}
	p.label = label

	if pid, err := readPIDFile(pidFile); err == nil && pidLooksAlive(pid) {
		if p.pidMatchesExpected(pid) {
			p.pid = pid
			p.pidFile = pidFile
			p.logFile = logFile
			p.verifiedPID = pid
			return pid, nil
		}
		_ = os.Remove(pidFile)
	}

	cmdline := shellJoin(append([]string{command}, args...)...)

	inner := fmt.Sprintf(
		"set -e; cd %s && umask 022 && ("+
			" pid=0;"+
			" if [ -f %s ]; then p=$(cat %s 2>/dev/null || true); case \"$p\" in (''|*[!0-9]*) p=0 ;; esac; if [ \"$p\" -gt 0 ] && kill -0 \"$p\" >/dev/null 2>&1; then pid=$p; fi; fi;"+
			" if [ \"$pid\" -gt 0 ]; then echo \"$pid\" > %s; exit 0; fi;"+
			" rm -f %s >/dev/null 2>&1 || true;"+
			" : > %s;"+
			" ( %s ) >> %s 2>&1 &"+
			" pid=$!; echo ${pid:-0} > %s;"+
			" sleep 0.2;"+
			" if [ \"$pid\" -le 0 ] || ! kill -0 \"$pid\" >/dev/null 2>&1; then rm -f %s >/dev/null 2>&1 || true; exit 23; fi;"+
			" )",
		shellQuote(workdirOrDot(workdir)),
		shellQuote(pidFile),
		shellQuote(pidFile),
		shellQuote(pidFile),
		shellQuote(pidFile),
		shellQuote(logFile),
		cmdline,
		shellQuote(logFile),
		shellQuote(pidFile),
		shellQuote(pidFile),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	output, err := darwinAdminRunShLC(ctx, inner)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return 0, fmt.Errorf("start (admin): timeout")
		}
		if tail := strings.TrimSpace(tailFile(logFile, 80)); tail != "" {
			return 0, fmt.Errorf("start (admin): %w: %s; hev log tail:\n%s", err, strings.TrimSpace(output), tail)
		}
		return 0, fmt.Errorf("start (admin): %w: %s", err, strings.TrimSpace(output))
	}

	pid, err := readPIDEventually(pidFile, 1500*time.Millisecond)
	if err != nil {
		if tail := strings.TrimSpace(tailFile(logFile, 80)); tail != "" {
			return 0, fmt.Errorf("start (admin): %w; hev log tail:\n%s", err, tail)
		}
		return 0, err
	}
	if pid <= 0 {
		if tail := strings.TrimSpace(tailFile(logFile, 80)); tail != "" {
			return 0, fmt.Errorf("start (admin): failed to obtain pid for %s; hev log tail:\n%s", p.label, tail)
		}
		return 0, fmt.Errorf("start (admin): failed to obtain pid for %s", p.label)
	}

	p.pid = pid
	p.verifiedPID = pid
	p.pidFile = pidFile
	p.logFile = logFile
	return pid, nil
}

var (
	darwinHevMarkerIF = regexp.MustCompile(`(?m)^__SUDOKU_TUN_IF__=([^\s]+)\s*$`)
)

func escapeForDoubleQuotes(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

func ensureShellStmt(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	cmd = strings.TrimRight(cmd, ";\t\r\n ")
	if cmd == "" {
		return ""
	}
	return " " + cmd + ";"
}

// StartWithRoutes starts HEV with admin privileges and sets up routes in a single privileged
// invocation, avoiding repeated prompts and half-configured network state.
func (p *darwinAdminDetachedProcess) StartWithRoutes(ctx context.Context, command string, args []string, workdir string, pidFile string, logFile string, tunIPv4 string, serverIP string, defaultGateway string, defaultGatewayV6 string, defaultInterface string, dnsSetCmd string, dnsRestoreCmd string, pfSetCmd string, pfRestoreCmd string) (pid int, tunIf string, scriptOutput string, err error) {
	if pidFile == "" {
		return 0, "", "", errors.New("pidFile required")
	}
	if logFile == "" {
		return 0, "", "", errors.New("logFile required")
	}
	if strings.TrimSpace(tunIPv4) == "" {
		return 0, "", "", errors.New("tunIPv4 required")
	}
	if strings.TrimSpace(defaultGateway) == "" {
		return 0, "", "", errors.New("defaultGateway required")
	}
	defaultGatewayV6 = strings.TrimSpace(defaultGatewayV6)
	defaultInterface = strings.TrimSpace(defaultInterface)
	dnsSetCmd = strings.TrimSpace(dnsSetCmd)
	dnsRestoreCmd = strings.TrimSpace(dnsRestoreCmd)
	pfSetCmd = strings.TrimSpace(pfSetCmd)
	pfRestoreCmd = strings.TrimSpace(pfRestoreCmd)
	if err := ensureDir(filepath.Dir(pidFile)); err != nil {
		return 0, "", "", err
	}
	if err := ensureDir(filepath.Dir(logFile)); err != nil {
		return 0, "", "", err
	}
	if pidLooksAlive(p.pid) {
		return 0, "", "", fmt.Errorf("process already running (pid=%d)", p.pid)
	}

	quotedArgs := make([]string, 0, len(args))
	for _, a := range args {
		quotedArgs = append(quotedArgs, shellQuote(a))
	}

	label := p.label
	if strings.TrimSpace(label) == "" {
		label = fmt.Sprintf("sudoku4x4.hev.%d", os.Getuid())
	}
	p.label = label

	// Find the tunnel interface by the configured IPv4 (HEV assigns it to utunX).
	// Prefer interfaces that were not present before HEV starts, so we don't switch
	// default route to an unrelated/stale utun with the same address.
	findTunIFNew := fmt.Sprintf(
		`ifconfig 2>/dev/null | awk -v ip=%s -v before="$before_ifs" 'BEGIN{ifname=""} /^[^[:space:]]+:/ {gsub(":", "", $1); ifname=$1} $1=="inet" && $2==ip { if (index(" " before " ", " " ifname " ")==0) { print ifname; exit } }'`,
		shellQuote(strings.TrimSpace(tunIPv4)),
	)
	findTunIFAny := fmt.Sprintf(
		`ifconfig 2>/dev/null | awk -v ip=%s 'BEGIN{ifname=""} /^[^[:space:]]+:/ {gsub(":", "", $1); ifname=$1} $1=="inet" && $2==ip {print ifname; exit}'`,
		shellQuote(strings.TrimSpace(tunIPv4)),
	)

	serverRoute := ""
	if strings.TrimSpace(serverIP) != "" {
		serverRoute = shellJoin("route", "-n", "add", "-host", strings.TrimSpace(serverIP), strings.TrimSpace(defaultGateway)) +
			" || " + shellJoin("route", "-n", "change", "-host", strings.TrimSpace(serverIP), strings.TrimSpace(defaultGateway))
	}

	scopedDefaultRoute := ""
	if defaultInterface != "" {
		// Keep a physical scoped default route so the core can bind sockets to the physical interface
		// and egress without self-looping once the global default route switches to utun.
		scopedDefaultRoute = "(" +
			shellJoin("route", "-n", "add", "-ifscope", defaultInterface, "default", strings.TrimSpace(defaultGateway)) + " >/dev/null 2>&1 || " +
			shellJoin("route", "-n", "change", "-ifscope", defaultInterface, "default", strings.TrimSpace(defaultGateway)) + " >/dev/null 2>&1" +
			") || echo '__SUDOKU_WARN__=scoped_default_route_failed'"
	}
	scopedDefaultRoute6 := ""
	if defaultInterface != "" && defaultGatewayV6 != "" {
		// Keep a physical scoped IPv6 default route for direct sockets bound to the physical interface.
		scopedDefaultRoute6 = "(" +
			shellJoin("route", "-n", "add", "-inet6", "-ifscope", defaultInterface, "default", defaultGatewayV6) + " >/dev/null 2>&1 || " +
			shellJoin("route", "-n", "change", "-inet6", "-ifscope", defaultInterface, "default", defaultGatewayV6) + " >/dev/null 2>&1" +
			") || echo '__SUDOKU_WARN__=scoped_default_route6_failed'"
	}

	restoreSnippet := ""
	// If we fail after touching routes, try to restore the default route and clean up the server host route.
	// Be robust: `route change` can fail on macOS when multiple default routes exist; fall back to delete+add.
	restoreSnippet = "; (" +
		shellJoin("route", "-n", "change", "default", strings.TrimSpace(defaultGateway)) + " >/dev/null 2>&1 || (" +
		shellJoin("route", "-n", "delete", "default") + " >/dev/null 2>&1 || true; " +
		shellJoin("route", "-n", "add", "default", strings.TrimSpace(defaultGateway)) + " >/dev/null 2>&1 || true" +
		"))"
	if defaultGatewayV6 != "" {
		restoreSnippet += "; " + shellJoin("route", "-n", "change", "-inet6", "default", defaultGatewayV6) + " >/dev/null 2>&1 || true"
	}
	if strings.TrimSpace(serverIP) != "" {
		restoreSnippet += "; " + shellJoin("route", "-n", "delete", "-host", strings.TrimSpace(serverIP)) + " >/dev/null 2>&1 || true"
	}
	if dnsRestoreCmd != "" {
		restoreSnippet += "; " + dnsRestoreCmd + " >/dev/null 2>&1 || true"
	}
	if pfRestoreCmd != "" {
		restoreSnippet += "; " + pfRestoreCmd + " >/dev/null 2>&1 || true"
	}
	setDNSSnippet := ""
	if dnsSetCmd != "" {
		setDNSSnippet = ensureShellStmt(dnsSetCmd)
	}
	setPFSnippet := ""
	if pfSetCmd != "" {
		setPFSnippet = ensureShellStmt(pfSetCmd)
	}
	setDefaultV6Snippet := ""
	if defaultGatewayV6 != "" {
		setDefaultV6Snippet = ` route -n change -inet6 default -interface "$tun_if" || true;`
	}
	guardOKPath := pidFile + ".start_ok"

	inner := fmt.Sprintf(
		"set -e; cd %s && umask 022 && ("+
			" before_ifs=\"$(ifconfig -l 2>/dev/null || true)\";"+
			" launchctl remove %s >/dev/null 2>&1 || true;"+
			" launchctl submit -l %s -o %s -e %s -- %s %s;"+
			" sleep 0.2;"+
			" pid=0; for i in $(seq 1 50); do p=$(launchctl list | awk -v label=%s '$3==label {print $1; exit}'); case \"$p\" in (''|'-'|*[!0-9]*) p=0 ;; esac; if [ \"$p\" -gt 0 ]; then pid=$p; break; fi; sleep 0.1; done;"+
			" echo ${pid:-0} > %s;"+
			" guard_ok=%s; rm -f \"$guard_ok\" >/dev/null 2>&1 || true;"+
			" trap \"launchctl remove %s >/dev/null 2>&1 || true; rm -f \\\"%s\\\" >/dev/null 2>&1 || true%s\" EXIT;"+
			" ( sleep 35; if [ ! -f \"$guard_ok\" ]; then echo '__SUDOKU_GUARD__=revert'; launchctl remove %s >/dev/null 2>&1 || true; rm -f \\\"%s\\\" >/dev/null 2>&1 || true%s; fi ) >/dev/null 2>&1 &"+
			" tun_if=''; for i in $(seq 1 120); do tun_if=$(%s); [ -n \"$tun_if\" ] && break; sleep 0.1; done;"+
			" if [ -z \"$tun_if\" ]; then tun_if=$(%s); fi;"+
			" case \" $before_ifs \" in (*\" $tun_if \"*) echo '__SUDOKU_WARN__=reused_existing_tun_interface_'${tun_if} ;; esac;"+
			" if [ -z \"$tun_if\" ]; then echo '__SUDOKU_HEV_PID__='${pid:-0}; echo '__SUDOKU_TUN_IF__='; exit 22; fi;"+
			" echo '__SUDOKU_STEP__=routes';"+
			" %s"+
			" echo '__SUDOKU_STEP__=pf';"+
			" %s"+
			" echo '__SUDOKU_STEP__=default_route';"+
			" route -n change default -interface \"$tun_if\" || ("+
			" route -n delete default >/dev/null 2>&1 || true; "+
			" route -n add default -interface \"$tun_if\");"+
			" %s"+
			" echo '__SUDOKU_STEP__=scoped_routes';"+
			" %s"+
			" %s"+
			" echo '__SUDOKU_STEP__=dns';"+
			" %s"+
			" touch \"$guard_ok\" >/dev/null 2>&1 || true;"+
			" echo '__SUDOKU_HEV_PID__='${pid:-0};"+
			" echo '__SUDOKU_TUN_IF__='${tun_if};"+
			" trap - EXIT;"+
			" )",
		shellQuote(workdirOrDot(workdir)),
		shellQuote(label),
		shellQuote(label),
		shellQuote(logFile),
		shellQuote(logFile),
		shellQuote(command),
		strings.Join(quotedArgs, " "),
		shellQuote(label),
		shellQuote(pidFile),
		shellQuote(guardOKPath),
		shellQuote(label),
		escapeForDoubleQuotes(pidFile),
		escapeForDoubleQuotes(restoreSnippet),
		shellQuote(label),
		escapeForDoubleQuotes(pidFile),
		escapeForDoubleQuotes(restoreSnippet),
		findTunIFNew,
		findTunIFAny,
		func() string {
			if serverRoute == "" {
				return ""
			}
			return " " + serverRoute + ";"
		}(),
		setPFSnippet,
		setDefaultV6Snippet,
		func() string {
			if scopedDefaultRoute == "" {
				return ""
			}
			return " " + scopedDefaultRoute + ";"
		}(),
		func() string {
			if scopedDefaultRoute6 == "" {
				return ""
			}
			return " " + scopedDefaultRoute6 + ";"
		}(),
		setDNSSnippet,
	)
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	output, runErr := darwinAdminRunShLC(runCtx, inner)
	scriptOutput = strings.TrimSpace(output)
	if runErr != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return 0, "", scriptOutput, fmt.Errorf("start+routes (admin): timeout")
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			return 0, "", scriptOutput, fmt.Errorf("start+routes (admin): canceled")
		}
		return 0, "", scriptOutput, fmt.Errorf("start+routes (admin): %w: %s", runErr, scriptOutput)
	}

	pid, err = readPIDEventually(pidFile, 1500*time.Millisecond)
	if err != nil {
		return 0, "", scriptOutput, err
	}
	if pid <= 0 {
		return 0, "", scriptOutput, fmt.Errorf("start+routes (admin): failed to obtain pid for %s", p.label)
	}

	out := strings.ReplaceAll(output, "\r", "\n")
	if m := darwinHevMarkerIF.FindStringSubmatch(out); len(m) == 2 {
		tunIf = strings.TrimSpace(m[1])
	}
	if tunIf == "" {
		// Fall back to detection from the current network state.
		tunIf = darwinFindTunInterfaceByIPv4(tunIPv4)
	}
	if tunIf == "" {
		return pid, "", scriptOutput, errors.New("start+routes (admin): unable to detect tunnel interface")
	}

	p.pid = pid
	p.pidFile = pidFile
	p.logFile = logFile
	return pid, tunIf, scriptOutput, nil
}

func (p *darwinAdminDetachedProcess) Stop(timeout time.Duration) error {
	pid := p.pid
	pidFile := p.pidFile
	label := strings.TrimSpace(p.label)

	if pid <= 0 && strings.TrimSpace(pidFile) != "" {
		if filePID, err := readPIDFile(pidFile); err == nil {
			pid = filePID
		}
	}

	if pid <= 0 && label == "" {
		return nil
	}

	killSnippet := ""
	if pid > 0 && p.pidMatchesExpected(pid) {
		killSnippet = fmt.Sprintf(" kill -TERM %d >/dev/null 2>&1 || true; sleep 0.4; kill -KILL %d >/dev/null 2>&1 || true;", pid, pid)
	} else if pid > 0 && strings.TrimSpace(pidFile) != "" {
		// PID reuse safety: don't kill unrelated processes. Just drop the stale pidfile.
		pid = 0
	}
	removeJob := ""
	if label != "" {
		removeJob = " launchctl remove " + shellQuote(label) + " >/dev/null 2>&1 || true;"
	}
	rmPID := ""
	if strings.TrimSpace(pidFile) != "" {
		rmPID = " rm -f " + shellQuote(pidFile) + " >/dev/null 2>&1 || true;"
	}

	inner := "set -e;" + removeJob + killSnippet + rmPID + " true"

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, err := darwinAdminRunShLC(ctx, inner)
	p.pid = 0
	p.verifiedPID = 0
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("stop (admin): timeout")
		}
		return fmt.Errorf("stop (admin): %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

func workdirOrDot(workdir string) string {
	if strings.TrimSpace(workdir) == "" {
		return "."
	}
	return workdir
}

func readPIDFile(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return 0, fmt.Errorf("invalid pid in %s", path)
	}
	pid, err := strconv.Atoi(s)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid in %s", path)
	}
	return pid, nil
}

func readPIDEventually(path string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			s := strings.TrimSpace(string(raw))
			if s != "" {
				pid, perr := strconv.Atoi(s)
				if perr == nil && pid > 0 {
					return pid, nil
				}
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return 0, fmt.Errorf("read pid file %s: %w", path, err)
			}
			return 0, fmt.Errorf("invalid pid in %s", path)
		}
		time.Sleep(60 * time.Millisecond)
	}
}

func pidLooksAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}
