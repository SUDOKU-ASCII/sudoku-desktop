package core

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// lineWriter splits a byte stream into lines. Used as cmd.Stdout/Stderr so os/exec owns the
// copying goroutines and cmd.Wait only returns once every byte has been delivered.
type lineWriter struct {
	mu     sync.Mutex
	buf    []byte
	onLine func(string)
}

const maxLineBytes = 64 * 1024

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > maxLineBytes {
		w.emit(w.buf)
		w.buf = nil
	} else if len(w.buf) == 0 {
		w.buf = nil // release the backing array
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(w.buf)
		w.buf = nil
	}
}

func (w *lineWriter) emit(b []byte) {
	if w.onLine == nil {
		return
	}
	line := string(bytes.TrimRight(b, "\r"))
	if line != "" {
		w.onLine(line)
	}
}

// ManagedProcess supervises one child process.
type ManagedProcess struct {
	name string

	mu      sync.RWMutex
	cmd     *exec.Cmd
	done    chan struct{}
	exitErr error
}

func NewManagedProcess(name string) *ManagedProcess {
	return &ManagedProcess{name: name}
}

func (p *ManagedProcess) Start(command string, args []string, env []string, workdir string, onLine func(string)) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done != nil {
		select {
		case <-p.done:
		default:
			return fmt.Errorf("%s already running", p.name)
		}
	}
	cmd := exec.Command(command, args...)
	applyManagedProcessSysProcAttr(cmd)
	cmd.Env = append(os.Environ(), env...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	out := &lineWriter{onLine: onLine}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", p.name, err)
	}
	attachChildLifetime(cmd)
	done := make(chan struct{})
	p.cmd, p.done, p.exitErr = cmd, done, nil

	go func() {
		err := cmd.Wait()
		out.flush()
		p.mu.Lock()
		p.exitErr = err
		p.mu.Unlock()
		close(done)
		if err != nil && onLine != nil {
			onLine(fmt.Sprintf("%s exited: %v", p.name, err))
		}
	}()
	return nil
}

// Stop asks the process to exit gracefully and kills it after timeout.
func (p *ManagedProcess) Stop(timeout time.Duration) error {
	p.mu.RLock()
	cmd, done := p.cmd, p.done
	p.mu.RUnlock()
	if cmd == nil || done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	default:
	}
	terminateProcess(cmd)
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		select {
		case <-done:
			return nil
		case <-time.After(2 * time.Second):
			return fmt.Errorf("stop %s: timeout", p.name)
		}
	}
}

func (p *ManagedProcess) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.done == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// Done is closed when the current process exits. It is nil before the first Start.
func (p *ManagedProcess) Done() <-chan struct{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.done
}

func (p *ManagedProcess) PID() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.cmd == nil || p.cmd.Process == nil || !p.isRunningLocked() {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *ManagedProcess) isRunningLocked() bool {
	if p.done == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}
