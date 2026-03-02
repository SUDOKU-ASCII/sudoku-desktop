package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
}

func (p *darwinAdminDetachedProcess) PID() int { return p.pid }

func (p *darwinAdminDetachedProcess) IsRunning() bool {
	if pidLooksAlive(p.pid) {
		return true
	}
	label := strings.TrimSpace(p.label)
	if label == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "launchctl", "list", label)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	re := regexp.MustCompile(`(?m)\\bPID\\b\\s*=\\s*(\\d+)`)
	if m := re.FindStringSubmatch(string(output)); len(m) == 2 {
		if pid, perr := strconv.Atoi(m[1]); perr == nil && pid > 0 {
			p.pid = pid
			return pidLooksAlive(pid)
		}
	}
	// If the job is listed, treat it as running (even if PID isn't shown yet).
	return true
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
	if pidLooksAlive(p.pid) {
		return 0, fmt.Errorf("process already running (pid=%d)", p.pid)
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

	inner := fmt.Sprintf(
		"set -e; cd %s && umask 022 && ("+
			" launchctl remove %s >/dev/null 2>&1 || true;"+
			" launchctl submit -l %s -o %s -e %s -- %s %s;"+
			" sleep 0.2;"+
			" pid=0; for i in $(seq 1 50); do p=$(launchctl list | awk -v label=%s '$3==label {print $1; exit}'); case \"$p\" in (''|'-'|*[!0-9]*) p=0 ;; esac; if [ \"$p\" -gt 0 ]; then pid=$p; break; fi; sleep 0.1; done;"+
			" echo ${pid:-0} > %s;"+
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
	)
	cmdline := shellJoin("sh", "-lc", inner)

	script := fmt.Sprintf(`do shell script "%s" with administrator privileges`, appleScriptEscape(cmdline))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	if output, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return 0, fmt.Errorf("start (admin): timeout")
		}
		return 0, fmt.Errorf("start (admin): %w: %s", err, strings.TrimSpace(string(output)))
	}

	pid, err := readPIDEventually(pidFile, 1500*time.Millisecond)
	if err != nil {
		return 0, err
	}
	if pid <= 0 {
		return 0, fmt.Errorf("start (admin): failed to obtain pid for %s", p.label)
	}

	p.pid = pid
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

// StartWithRoutes starts HEV with admin privileges and sets up routes in the same admin session.
// This avoids repeated password prompts from multiple `osascript` invocations.
func (p *darwinAdminDetachedProcess) StartWithRoutes(ctx context.Context, command string, args []string, workdir string, pidFile string, logFile string, tunIPv4 string, serverIP string, defaultGateway string, defaultInterface string, dnsSetCmd string, dnsRestoreCmd string, pfSetCmd string, pfRestoreCmd string) (pid int, tunIf string, err error) {
	if pidFile == "" {
		return 0, "", errors.New("pidFile required")
	}
	if logFile == "" {
		return 0, "", errors.New("logFile required")
	}
	if strings.TrimSpace(tunIPv4) == "" {
		return 0, "", errors.New("tunIPv4 required")
	}
	if strings.TrimSpace(defaultGateway) == "" {
		return 0, "", errors.New("defaultGateway required")
	}
	defaultInterface = strings.TrimSpace(defaultInterface)
	dnsSetCmd = strings.TrimSpace(dnsSetCmd)
	dnsRestoreCmd = strings.TrimSpace(dnsRestoreCmd)
	pfSetCmd = strings.TrimSpace(pfSetCmd)
	pfRestoreCmd = strings.TrimSpace(pfRestoreCmd)
	if err := ensureDir(filepath.Dir(pidFile)); err != nil {
		return 0, "", err
	}
	if err := ensureDir(filepath.Dir(logFile)); err != nil {
		return 0, "", err
	}
	if pidLooksAlive(p.pid) {
		return 0, "", fmt.Errorf("process already running (pid=%d)", p.pid)
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
	findTunIF := fmt.Sprintf(
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
		scopedDefaultRoute = shellJoin("route", "-n", "add", "-ifscope", defaultInterface, "default", strings.TrimSpace(defaultGateway)) + " >/dev/null 2>&1 || true"
	}

	restoreSnippet := ""
	// If we fail after touching routes, try to restore the default route and clean up the server host route.
	restoreSnippet = "; " + shellJoin("route", "-n", "change", "default", strings.TrimSpace(defaultGateway)) + " >/dev/null 2>&1 || true"
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

	inner := fmt.Sprintf(
		"set -e; cd %s && umask 022 && ("+
			" launchctl remove %s >/dev/null 2>&1 || true;"+
			" launchctl submit -l %s -o %s -e %s -- %s %s;"+
			" sleep 0.2;"+
			" pid=0; for i in $(seq 1 50); do p=$(launchctl list | awk -v label=%s '$3==label {print $1; exit}'); case \"$p\" in (''|'-'|*[!0-9]*) p=0 ;; esac; if [ \"$p\" -gt 0 ]; then pid=$p; break; fi; sleep 0.1; done;"+
			" echo ${pid:-0} > %s;"+
			" trap \"launchctl remove %s >/dev/null 2>&1 || true; rm -f \\\"%s\\\" >/dev/null 2>&1 || true%s\" EXIT;"+
			" tun_if=''; for i in $(seq 1 120); do tun_if=$(%s); [ -n \"$tun_if\" ] && break; sleep 0.1; done;"+
			" if [ -z \"$tun_if\" ]; then echo '__SUDOKU_HEV_PID__='${pid:-0}; echo '__SUDOKU_TUN_IF__='; exit 22; fi;"+
			" echo '__SUDOKU_STEP__=routes';"+
			" %s"+
			" %s"+
			" echo '__SUDOKU_STEP__=default_route';"+
			" route -n change default -interface \"$tun_if\" || ("+
			" route -n delete default >/dev/null 2>&1 || true; "+
			" route -n add default -interface \"$tun_if\");"+
			" route -n change -inet6 default -interface \"$tun_if\" || true;"+
			" echo '__SUDOKU_STEP__=pf';"+
			" %s"+
			" echo '__SUDOKU_STEP__=dns';"+
			" %s"+
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
		shellQuote(label),
		escapeForDoubleQuotes(pidFile),
		escapeForDoubleQuotes(restoreSnippet),
		findTunIF,
		func() string {
			if serverRoute == "" {
				return ""
			}
			return " " + serverRoute + ";"
		}(),
		func() string {
			if scopedDefaultRoute == "" {
				return ""
			}
			return " " + scopedDefaultRoute + ";"
		}(),
		setPFSnippet,
		setDNSSnippet,
	)
	cmdline := shellJoin("sh", "-lc", inner)

	script := fmt.Sprintf(`do shell script "%s" with administrator privileges`, appleScriptEscape(cmdline))
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "osascript", "-e", script)
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return 0, "", fmt.Errorf("start+routes (admin): timeout")
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			return 0, "", fmt.Errorf("start+routes (admin): canceled")
		}
		return 0, "", fmt.Errorf("start+routes (admin): %w: %s", runErr, strings.TrimSpace(string(output)))
	}

	pid, err = readPIDEventually(pidFile, 1500*time.Millisecond)
	if err != nil {
		return 0, "", err
	}
	if pid <= 0 {
		return 0, "", fmt.Errorf("start+routes (admin): failed to obtain pid for %s", p.label)
	}

	out := strings.ReplaceAll(string(output), "\r", "\n")
	if m := darwinHevMarkerIF.FindStringSubmatch(out); len(m) == 2 {
		tunIf = strings.TrimSpace(m[1])
	}
	if tunIf == "" {
		// Fall back to detection from the current network state.
		tunIf = darwinFindTunInterfaceByIPv4(tunIPv4)
	}
	if tunIf == "" {
		return pid, "", errors.New("start+routes (admin): unable to detect tunnel interface")
	}

	p.pid = pid
	p.pidFile = pidFile
	p.logFile = logFile
	return pid, tunIf, nil
}

func (p *darwinAdminDetachedProcess) Stop(timeout time.Duration) error {
	pid := p.pid
	pidFile := p.pidFile
	label := p.label

	if strings.TrimSpace(label) == "" && pid <= 0 {
		return nil
	}

	killSnippet := ""
	if pid > 0 {
		killSnippet = fmt.Sprintf(" kill -TERM %d >/dev/null 2>&1 || true; sleep 0.3; kill -KILL %d >/dev/null 2>&1 || true;", pid, pid)
	}

	inner := fmt.Sprintf(
		"launchctl remove %s >/dev/null 2>&1 || true;%s rm -f %s || true",
		shellQuote(label),
		killSnippet,
		shellQuote(pidFile),
	)
	cmdline := shellJoin("sh", "-lc", inner)

	script := fmt.Sprintf(`do shell script "%s" with administrator privileges`, appleScriptEscape(cmdline))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	done := make(chan error, 1)
	go func() {
		if output, err := cmd.CombinedOutput(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				done <- fmt.Errorf("stop (admin): timeout")
				return
			}
			done <- fmt.Errorf("stop (admin): %w: %s", err, strings.TrimSpace(string(output)))
			return
		}
		done <- nil
	}()

	select {
	case err := <-done:
		p.pid = 0
		return err
	case <-time.After(timeout):
		return fmt.Errorf("stop timeout (pid=%d)", pid)
	}
}

func workdirOrDot(workdir string) string {
	if strings.TrimSpace(workdir) == "" {
		return "."
	}
	return workdir
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
