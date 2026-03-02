package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
)

type ManagedProcess struct {
	mu      sync.RWMutex
	name    string
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	running bool
	done    chan error
}

func NewManagedProcess(name string) *ManagedProcess {
	return &ManagedProcess{name: name}
}

func (p *ManagedProcess) Start(command string, args []string, env []string, workdir string, onLine func(string)) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return fmt.Errorf("%s already running", p.name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, command, args...)
	applyManagedProcessSysProcAttr(cmd)
	cmd.Env = append(os.Environ(), env...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("%s stdout pipe: %w", p.name, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("%s stderr pipe: %w", p.name, err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start %s: %w", p.name, err)
	}
	p.cmd = cmd
	p.cancel = cancel
	p.running = true
	p.done = make(chan error, 1)
	localDone := p.done

	go func() {
		_ = readLinesPipe(ctx, stdout, onLine)
	}()
	go func() {
		_ = readLinesPipe(ctx, stderr, onLine)
	}()
	go func() {
		err := cmd.Wait()
		localDone <- err
		close(localDone)
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.cancel != nil {
			p.cancel()
		}
		p.cancel = nil
		p.cmd = nil
		p.running = false
		if err != nil && onLine != nil {
			onLine(fmt.Sprintf("%s exited: %v", p.name, err))
		}
	}()

	return nil
}

func (p *ManagedProcess) Stop(timeout time.Duration) error {
	p.mu.Lock()
	if !p.running || p.cmd == nil {
		p.mu.Unlock()
		return nil
	}
	cmd := p.cmd
	cancel := p.cancel
	done := p.done
	p.mu.Unlock()

	if cmd.Process != nil {
		if runtime.GOOS == "windows" {
			_ = cmd.Process.Kill()
		} else {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
	}

	select {
	case err, ok := <-done:
		if cancel != nil {
			cancel()
		}
		if !ok {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		return nil
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		if cancel != nil {
			cancel()
		}
		return fmt.Errorf("stop %s timeout", p.name)
	}
}

func (p *ManagedProcess) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

func (p *ManagedProcess) PID() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
