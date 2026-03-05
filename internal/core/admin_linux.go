//go:build linux

package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type linuxAdminDetachedProcess struct {
	pid     int
	pidFile string
	logFile string

	expectCmdBase    string
	expectRuntimeDir string
	verifiedPID      int
}

func newAdminDetachedProcess() adminDetachedProcess {
	return &linuxAdminDetachedProcess{}
}

func (p *linuxAdminDetachedProcess) PID() int { return p.pid }

func (p *linuxAdminDetachedProcess) pidMatchesExpected(pid int) bool {
	if pid <= 0 {
		return false
	}
	args, err := linuxProcessCmdline(pid)
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

func (p *linuxAdminDetachedProcess) IsRunning() bool {
	if pidLooksAliveLinux(p.pid) {
		// Always validate the process command line. PIDs can be reused, and a stale PID
		// must never be treated as "still running".
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
	if pid, err := readPIDFileLinux(p.pidFile); err == nil && pidLooksAliveLinux(pid) && p.pidMatchesExpected(pid) {
		p.pid = pid
		p.verifiedPID = pid
		return true
	} else if err == nil && pidLooksAliveLinux(pid) {
		// PID reuse can make the pidfile point to an unrelated process. Never treat that as "running".
		_ = os.Remove(p.pidFile)
	} else if err != nil {
		// Corrupt/partial pid file; drop it so future checks don't flap.
		_ = os.Remove(p.pidFile)
	}
	return false
}

func (p *linuxAdminDetachedProcess) Start(command string, args []string, workdir string, pidFile string, logFile string) (int, error) {
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
	p.pidFile = pidFile
	p.logFile = logFile

	if pidLooksAliveLinux(p.pid) {
		// Never trust cached PIDs without re-validation; PIDs can be reused.
		if p.pidMatchesExpected(p.pid) {
			p.verifiedPID = p.pid
			return 0, fmt.Errorf("process already running (pid=%d)", p.pid)
		}
		p.pid = 0
		p.verifiedPID = 0
	}

	if pid, err := readPIDFileLinux(pidFile); err == nil && pidLooksAliveLinux(pid) {
		if p.pidMatchesExpected(pid) {
			p.pid = pid
			p.verifiedPID = pid
			return pid, nil
		}
		_ = os.Remove(pidFile)
	}

	cmdline := shellJoin(append([]string{command}, args...)...)

	inner := fmt.Sprintf(
		"set -e; export PATH=\"/usr/sbin:/sbin:/usr/bin:/bin:$PATH\"; cd %s && umask 022 && ("+
			" base=%s; dir=%s;"+
			" pid=0;"+
			" if [ -f %s ]; then p=$(cat %s 2>/dev/null || true); case \"$p\" in (''|*[!0-9]*) p=0 ;; esac;"+
			"   if [ \"$p\" -gt 0 ] && kill -0 \"$p\" >/dev/null 2>&1; then"+
			"     ok=1; cmd=$(tr '\\\\0' ' ' < /proc/\"$p\"/cmdline 2>/dev/null || true);"+
			"     if [ -n \"$base\" ] && ! printf '%%s' \"$cmd\" | grep -F -q -- \"$base\"; then ok=0; fi;"+
			"     if [ -n \"$dir\" ] && ! printf '%%s' \"$cmd\" | grep -F -q -- \"$dir\"; then ok=0; fi;"+
			"     if [ \"$ok\" -eq 1 ]; then pid=$p; else rm -f %s >/dev/null 2>&1 || true; fi;"+
			"   fi;"+
			" fi;"+
			" if [ \"$pid\" -gt 0 ]; then echo \"$pid\" > %s; exit 0; fi;"+
			" rm -f %s >/dev/null 2>&1 || true;"+
			" : > %s;"+
			" ( %s ) >> %s 2>&1 &"+
			" pid=$!; echo ${pid:-0} > %s;"+
			" sleep 0.2;"+
			" if [ \"$pid\" -le 0 ] || ! kill -0 \"$pid\" >/dev/null 2>&1; then rm -f %s >/dev/null 2>&1 || true; exit 23; fi;"+
			" )",
		shellQuote(workdirOrDotLinux(workdir)),
		shellQuote(strings.TrimSpace(p.expectCmdBase)),
		shellQuote(strings.TrimSpace(p.expectRuntimeDir)),
		shellQuote(pidFile),
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
	output, err := linuxAdminRunShLC(ctx, inner)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return 0, fmt.Errorf("start (admin): timeout")
		}
		if tail := strings.TrimSpace(tailFile(logFile, 80)); tail != "" {
			return 0, fmt.Errorf("start (admin): %w: %s; hev log tail:\n%s", err, strings.TrimSpace(output), tail)
		}
		return 0, fmt.Errorf("start (admin): %w: %s", err, strings.TrimSpace(output))
	}

	pid, err := readPIDEventuallyLinux(pidFile, 1500*time.Millisecond)
	if err != nil {
		if tail := strings.TrimSpace(tailFile(logFile, 80)); tail != "" {
			return 0, fmt.Errorf("start (admin): %w; hev log tail:\n%s", err, tail)
		}
		return 0, err
	}
	if pid <= 0 {
		if tail := strings.TrimSpace(tailFile(logFile, 80)); tail != "" {
			return 0, fmt.Errorf("start (admin): failed to obtain pid; hev log tail:\n%s", tail)
		}
		return 0, errors.New("start (admin): failed to obtain pid")
	}

	p.pid = pid
	p.verifiedPID = pid
	return pid, nil
}

func (p *linuxAdminDetachedProcess) Stop(timeout time.Duration) error {
	if strings.TrimSpace(p.pidFile) == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	waitSteps := int(timeout / (120 * time.Millisecond))
	if waitSteps < 1 {
		waitSteps = 1
	}
	if waitSteps > 200 {
		waitSteps = 200
	}

	inner := fmt.Sprintf(
		"set -e; export PATH=\"/usr/sbin:/sbin:/usr/bin:/bin:$PATH\"; ("+
			" base=%s; dir=%s;"+
			" pid=0;"+
			" if [ -f %s ]; then p=$(cat %s 2>/dev/null || true); case \"$p\" in (''|*[!0-9]*) p=0 ;; esac;"+
			"   if [ \"$p\" -gt 0 ] && kill -0 \"$p\" >/dev/null 2>&1; then"+
			"     ok=1; cmd=$(tr '\\\\0' ' ' < /proc/\"$p\"/cmdline 2>/dev/null || true);"+
			"     if [ -n \"$base\" ] && ! printf '%%s' \"$cmd\" | grep -F -q -- \"$base\"; then ok=0; fi;"+
			"     if [ -n \"$dir\" ] && ! printf '%%s' \"$cmd\" | grep -F -q -- \"$dir\"; then ok=0; fi;"+
			"     if [ \"$ok\" -eq 1 ]; then pid=$p; else rm -f %s >/dev/null 2>&1 || true; exit 0; fi;"+
			"   fi;"+
			" fi;"+
			" if [ \"$pid\" -le 0 ]; then rm -f %s >/dev/null 2>&1 || true; exit 0; fi;"+
			" kill -TERM \"$pid\" >/dev/null 2>&1 || true;"+
			" i=0; while [ $i -lt %d ]; do if ! kill -0 \"$pid\" >/dev/null 2>&1; then break; fi; i=$((i+1)); sleep 0.12; done;"+
			" if kill -0 \"$pid\" >/dev/null 2>&1; then kill -KILL \"$pid\" >/dev/null 2>&1 || true; fi;"+
			" rm -f %s >/dev/null 2>&1 || true;"+
			" )",
		shellQuote(strings.TrimSpace(p.expectCmdBase)),
		shellQuote(strings.TrimSpace(p.expectRuntimeDir)),
		shellQuote(p.pidFile),
		shellQuote(p.pidFile),
		shellQuote(p.pidFile),
		shellQuote(p.pidFile),
		waitSteps,
		shellQuote(p.pidFile),
	)
	ctx, cancel := context.WithTimeout(context.Background(), timeout+4*time.Second)
	defer cancel()
	_, err := linuxAdminRunShLC(ctx, inner)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("stop (admin): timeout")
		}
		return fmt.Errorf("stop (admin): %w", err)
	}

	// Clear cached pid so future IsRunning checks use pidfile validation again.
	p.pid = 0
	p.verifiedPID = 0
	return nil
}

func workdirOrDotLinux(workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return "."
	}
	return workdir
}

func readPIDFileLinux(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return 0, errors.New("empty pid file")
	}
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if pid <= 0 {
		return 0, errors.New("invalid pid")
	}
	return pid, nil
}

func readPIDEventuallyLinux(path string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for {
		pid, err := readPIDFileLinux(path)
		if err == nil && pid > 0 {
			return pid, nil
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

func pidLooksAliveLinux(pid int) bool {
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

func linuxProcessCmdline(pid int) (string, error) {
	if pid <= 0 {
		return "", errors.New("invalid pid")
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", errors.New("empty cmdline")
	}
	// cmdline is NUL-separated.
	out := strings.ReplaceAll(string(raw), "\x00", " ")
	return strings.TrimSpace(out), nil
}
