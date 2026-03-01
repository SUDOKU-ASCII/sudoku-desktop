package core

import (
	"fmt"
	"io"
	"net"
	"sync"
)

type forwardInstance struct {
	rule     PortForwardRule
	listener net.Listener
	stopOnce sync.Once
	stopCh   chan struct{}
}

func newForwardInstance(rule PortForwardRule) (*forwardInstance, error) {
	ln, err := net.Listen("tcp", rule.Listen)
	if err != nil {
		return nil, err
	}
	fi := &forwardInstance{rule: rule, listener: ln, stopCh: make(chan struct{})}
	return fi, nil
}

func (f *forwardInstance) run(onLog func(string)) {
	for {
		c, err := f.listener.Accept()
		if err != nil {
			select {
			case <-f.stopCh:
				return
			default:
				if onLog != nil {
					onLog(fmt.Sprintf("port-forward accept failed: %v", err))
				}
				continue
			}
		}
		go func(src net.Conn) {
			defer src.Close()
			dst, err := net.Dial("tcp", f.rule.Target)
			if err != nil {
				if onLog != nil {
					onLog(fmt.Sprintf("port-forward %s -> %s dial failed: %v", f.rule.Listen, f.rule.Target, err))
				}
				return
			}
			defer dst.Close()
			go io.Copy(dst, src)
			io.Copy(src, dst)
		}(c)
	}
}

func (f *forwardInstance) stop() {
	f.stopOnce.Do(func() {
		close(f.stopCh)
		_ = f.listener.Close()
	})
}

type portForwardManager struct {
	mu      sync.Mutex
	active  map[string]*forwardInstance
	onLog   func(string)
	enabled bool
}

func newPortForwardManager(onLog func(string)) *portForwardManager {
	return &portForwardManager{
		active: map[string]*forwardInstance{},
		onLog:  onLog,
	}
}

func (m *portForwardManager) Apply(rules []PortForwardRule) {
	m.mu.Lock()
	defer m.mu.Unlock()

	next := map[string]PortForwardRule{}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if rule.ID == "" {
			rule.ID = newID("pf_")
		}
		next[rule.ID] = rule
	}

	for id, inst := range m.active {
		if _, ok := next[id]; !ok {
			inst.stop()
			delete(m.active, id)
			if m.onLog != nil {
				m.onLog(fmt.Sprintf("stopped port-forward: %s", inst.rule.Name))
			}
		}
	}

	for id, rule := range next {
		if _, ok := m.active[id]; ok {
			continue
		}
		inst, err := newForwardInstance(rule)
		if err != nil {
			if m.onLog != nil {
				m.onLog(fmt.Sprintf("start port-forward %s failed: %v", rule.Name, err))
			}
			continue
		}
		m.active[id] = inst
		if m.onLog != nil {
			m.onLog(fmt.Sprintf("port-forward listening %s -> %s", rule.Listen, rule.Target))
		}
		go inst.run(m.onLog)
	}
}

func (m *portForwardManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, inst := range m.active {
		inst.stop()
		delete(m.active, id)
	}
}
