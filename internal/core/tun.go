package core

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	helperConnectTimeout = 120 * time.Second // includes the user typing a password
	helperPingEvery      = 2 * time.Second
	helperPingTimeout    = 4 * time.Second
	helperPingMisses     = 3
	helperUpTimeout      = 40 * time.Second
	helperDownTimeout    = 25 * time.Second
)

var (
	errAuthCancelled    = errors.New("administrator authorization was cancelled")
	errAdminBadPassword = errors.New("incorrect administrator password")
	errAdminNotSudoer   = errors.New("this account is not allowed to use sudo")
	errAdminTimeout     = errors.New("administrator password check timed out")
)

// tunClient is the app side of the TUN helper link: it launches the elevated helper once,
// keeps the loopback connection alive (heartbeat) and forwards up/down requests. The helper
// is a separate root process, so the GUI never holds privileges and a GUI crash simply
// makes the helper tear the tunnel down.
type tunClient struct {
	exe        string
	runtimeDir string
	logf       func(string)
	onEvent    func(event, line string)

	mu      sync.Mutex
	conn    *ipcConn
	gone    chan struct{} // closed when the current connection ends
	pending map[uint64]chan *ipcMsg
	nextID  uint64
	closing bool
	launch  sync.Mutex // serializes helper launches
}

func newTunClient(exe, runtimeDir string, logf func(string), onEvent func(event, line string)) *tunClient {
	return &tunClient{exe: exe, runtimeDir: runtimeDir, logf: logf, onEvent: onEvent, pending: map[uint64]chan *ipcMsg{}}
}

func (c *tunClient) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// Ensure launches (and authenticates) the helper unless it is already connected.
func (c *tunClient) Ensure(ctx context.Context) error {
	c.launch.Lock()
	defer c.launch.Unlock()
	if c.Connected() {
		return nil
	}
	if !admin.Has() {
		return ErrAdminRequired
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	token := randomSecret() + randomSecret()
	tokenPath := filepath.Join(c.runtimeDir, "helper.token")
	if err := writeFileAtomic(tokenPath, []byte(token), 0o600); err != nil {
		return err
	}
	defer os.Remove(tokenPath) // normally already deleted by the helper

	args := []string{"--tun-helper", "--addr", ln.Addr().String(), "--token-file", tokenPath, "--parent", strconv.Itoa(os.Getpid())}
	c.logf("starting the tunnel helper with administrator rights")
	cmd, stderr, err := elevatedLaunch(c.exe, args, false)
	if err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	type accepted struct {
		c   *ipcConn
		err error
	}
	acc := make(chan accepted, 1)
	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				acc <- accepted{err: err}
				return
			}
			ic := newIPCConn(raw)
			_ = raw.SetReadDeadline(time.Now().Add(5 * time.Second))
			m, err := ic.recv()
			_ = raw.SetReadDeadline(time.Time{})
			if err != nil || m.Op != opHello || m.Ver != ipcVersion || subtle.ConstantTimeCompare([]byte(m.Token), []byte(token)) != 1 {
				ic.close() // someone else poking at the port
				continue
			}
			if err := ic.send(&ipcMsg{OK: true}); err != nil {
				ic.close()
				continue
			}
			acc <- accepted{c: ic}
			return
		}
	}()

	timer := time.NewTimer(helperConnectTimeout)
	defer timer.Stop()
	for {
		select {
		case a := <-acc:
			if a.err != nil {
				return fmt.Errorf("helper connection failed: %w", a.err)
			}
			c.attach(a.c)
			return nil
		case err := <-exited:
			if err == nil && runtime.GOOS == "windows" {
				exited = nil // Start-Process returns as soon as UAC launched the helper
				continue
			}
			return launchError(stderr, err)
		case <-ctx.Done():
			_ = ln.Close()
			return ctx.Err()
		case <-timer.C:
			_ = ln.Close()
			return errors.New("timed out waiting for the tunnel helper")
		}
	}
}

// launchError turns a failed elevated launch into errAuthCancelled / errAdminBadPassword
// or a readable error. A rejected password is forgotten so the user is asked again.
func launchError(stderr *cappedBuffer, err error) error {
	msg := strings.TrimSpace(stderr.String())
	if isAuthCancel(msg) {
		return errAuthCancelled
	}
	if isSudoReject(msg) {
		admin.Forget()
		return errAdminBadPassword
	}
	if msg == "" && err != nil {
		msg = err.Error()
	}
	if msg == "" {
		msg = "the helper exited before connecting"
	}
	return fmt.Errorf("could not start the tunnel helper: %s", lastLine(msg))
}

func isSudoReject(msg string) bool {
	l := strings.ToLower(msg)
	return strings.Contains(l, "incorrect password") || strings.Contains(l, "no password was provided") ||
		strings.Contains(l, "sorry, try again") || strings.Contains(l, "a password is required")
}

func isAuthCancel(msg string) bool {
	l := strings.ToLower(msg)
	return strings.Contains(l, "user canceled") || strings.Contains(l, "user cancelled") || strings.Contains(l, "-128") ||
		strings.Contains(l, "dismissed") || strings.Contains(l, "canceled by the user") || strings.Contains(l, "operation was canceled")
}

func (c *tunClient) attach(ic *ipcConn) {
	c.mu.Lock()
	c.conn = ic
	c.closing = false
	gone := make(chan struct{})
	c.gone = gone
	c.mu.Unlock()
	go c.readLoop(ic, gone)
	go c.pingLoop(ic, gone)
}

func (c *tunClient) readLoop(ic *ipcConn, gone chan struct{}) {
	for {
		m, err := ic.recv()
		if err != nil {
			break
		}
		if m.Event != "" {
			if c.onEvent != nil {
				c.onEvent(m.Event, m.Line)
			}
			continue
		}
		c.mu.Lock()
		ch := c.pending[m.ID]
		delete(c.pending, m.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}
	ic.close()
	c.mu.Lock()
	intentional := c.closing
	if c.conn == ic {
		c.conn = nil
	}
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
	c.mu.Unlock()
	close(gone)
	if !intentional && c.onEvent != nil {
		c.onEvent(evExit, "connection to the tunnel helper was lost")
	}
}

func (c *tunClient) pingLoop(ic *ipcConn, gone chan struct{}) {
	t := time.NewTicker(helperPingEvery)
	defer t.Stop()
	miss := 0
	for {
		select {
		case <-gone:
			return
		case <-t.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), helperPingTimeout)
		_, err := c.callOn(ctx, ic, &ipcMsg{Op: opPing})
		cancel()
		if err == nil {
			miss = 0
			continue
		}
		if miss++; miss >= helperPingMisses {
			ic.close() // readLoop notices and reports the loss
			return
		}
	}
}

func (c *tunClient) callOn(ctx context.Context, ic *ipcConn, m *ipcMsg) (*ipcMsg, error) {
	ch := make(chan *ipcMsg, 1)
	c.mu.Lock()
	c.nextID++
	m.ID = c.nextID
	c.pending[m.ID] = ch
	c.mu.Unlock()
	drop := func() {
		c.mu.Lock()
		delete(c.pending, m.ID)
		c.mu.Unlock()
	}
	if err := ic.send(m); err != nil {
		drop()
		return nil, err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return nil, errors.New("tunnel helper disconnected")
		}
		if !r.OK {
			if r.Error == "" {
				r.Error = "unknown helper error"
			}
			return r, errors.New(r.Error)
		}
		return r, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}

func (c *tunClient) call(ctx context.Context, m *ipcMsg) (*ipcMsg, error) {
	c.mu.Lock()
	ic := c.conn
	c.mu.Unlock()
	if ic == nil {
		return nil, errors.New("tunnel helper is not running")
	}
	return c.callOn(ctx, ic, m)
}

// Up asks the helper to bring the tunnel up (launching the helper first if needed).
func (c *tunClient) Up(ctx context.Context, plan *tunPlan) (*tunStatus, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, helperUpTimeout)
	defer cancel()
	r, err := c.call(cctx, &ipcMsg{Op: opUp, Plan: plan})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			// The helper may still be mid-setup; make it unwind before reporting.
			dctx, dcancel := context.WithTimeout(context.Background(), helperDownTimeout)
			_ = c.Down(dctx)
			dcancel()
		}
		return nil, err
	}
	return r.Data, nil
}

func (c *tunClient) Down(ctx context.Context) error {
	if !c.Connected() {
		return nil
	}
	_, err := c.call(ctx, &ipcMsg{Op: opDown})
	return err
}

func (c *tunClient) Status(ctx context.Context) (*tunStatus, error) {
	r, err := c.call(ctx, &ipcMsg{Op: opStatus})
	if err != nil {
		return nil, err
	}
	return r.Data, nil
}

// Shutdown tears the tunnel down and dismisses the helper. The helper also does this on its
// own when the connection drops, so this is only about being quick and graceful.
func (c *tunClient) Shutdown(timeout time.Duration) {
	c.mu.Lock()
	ic, gone := c.conn, c.gone
	c.closing = true
	c.mu.Unlock()
	if ic == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, _ = c.callOn(ctx, ic, &ipcMsg{Op: opDown})
	_ = ic.send(&ipcMsg{Op: opQuit})
	select {
	case <-gone:
	case <-time.After(2 * time.Second):
	}
	ic.close()
}

// Recover runs the helper in one-shot recovery mode (elevated) to undo routes/DNS left behind
// by a helper that died.
func (c *tunClient) Recover(ctx context.Context) error {
	c.Shutdown(5 * time.Second)
	c.launch.Lock()
	defer c.launch.Unlock()
	if !admin.Has() {
		return ErrAdminRequired
	}
	cmd, stderr, err := elevatedLaunch(c.exe, []string{"--tun-helper", "--recover"}, true)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return launchError(stderr, err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ---------------------------------------------------------------------------------------
// elevation

// elevatedLaunch starts exe with administrator rights: through sudo with the in-app
// password on macOS / Linux (pkexec when sudo is missing), through UAC on Windows. The
// launcher (sudo, pkexec) stays alive for the helper's lifetime; on Windows Start-Process
// returns as soon as the helper started unless wait is set.
func elevatedLaunch(exe string, args []string, wait bool) (*exec.Cmd, *cappedBuffer, error) {
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS != "windows" && os.Geteuid() == 0:
		cmd = exec.Command(exe, args...)
	case adminUsesPassword():
		pw, ok := admin.stdin()
		if !ok {
			return nil, nil, ErrAdminRequired
		}
		cmd = exec.Command(sudoPath, append([]string{"-S", "-k", "-p", "", "--", exe}, args...)...)
		cmd.Stdin = &zeroingReader{buf: pw}
	case runtime.GOOS == "linux":
		pk, err := exec.LookPath("pkexec")
		if err != nil {
			return nil, nil, errors.New("sudo or pkexec is required to grant the tunnel administrator rights")
		}
		cmd = exec.Command(pk, append([]string{exe}, args...)...)
	case runtime.GOOS == "windows":
		quoted := make([]string, 0, len(args))
		for _, a := range args {
			quoted = append(quoted, `"`+strings.ReplaceAll(a, `"`, `\"`)+`"`)
		}
		ps := "Start-Process -Verb RunAs -WindowStyle Hidden -FilePath " + psQuote(exe) +
			" -ArgumentList " + psQuote(strings.Join(quoted, " "))
		if wait {
			ps += " -Wait"
		}
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-Command", ps)
		applyManagedProcessSysProcAttr(cmd)
	default:
		return nil, nil, errors.New("tunnel mode is not supported on " + runtime.GOOS)
	}
	stderr := &cappedBuffer{max: 16 << 10}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return cmd, stderr, nil
}

// zeroingReader hands the password to sudo once and wipes it afterwards.
type zeroingReader struct {
	buf []byte
	off int
}

func (r *zeroingReader) Read(p []byte) (int, error) {
	if r.off >= len(r.buf) {
		zeroBytes(r.buf)
		return 0, io.EOF
	}
	n := copy(p, r.buf[r.off:])
	r.off += n
	return n, nil
}

// cappedBuffer keeps the last max bytes written (the helper's stderr lives as long as it).
type cappedBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.b = append(c.b, p...)
	if len(c.b) > c.max {
		c.b = append(c.b[:0], c.b[len(c.b)-c.max:]...)
	}
	c.mu.Unlock()
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.b)
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
