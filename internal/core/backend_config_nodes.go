package core

import (
	"fmt"
	"strings"
	"time"
)

func (b *Backend) GetConfig() AppConfig {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cfg := cloneConfig(b.cfg)
	if cfg == nil {
		return AppConfig{}
	}
	return *cfg
}

func (b *Backend) SaveConfig(next AppConfig) error {
	b.mu.Lock()
	prev := cloneConfig(b.cfg)
	wasRunning := b.state.Running
	wasTun := b.state.TunRunning
	normalizeConfig(&next, b.store.RuntimeDir())

	launchAtLoginChanged := prev != nil && prev.UI.LaunchAtLogin != next.UI.LaunchAtLogin
	launchAtLoginEnabled := next.UI.LaunchAtLogin
	if launchAtLoginChanged {
		// Apply OS-level autostart first so a failure doesn't leave config in an inconsistent state.
		if err := setLaunchAtLogin(launchAtLoginEnabled); err != nil {
			b.state.LastError = err.Error()
			b.emitStateLocked()
			b.mu.Unlock()
			return err
		}
	}
	if err := b.store.Save(&next); err != nil {
		// Best-effort rollback when config persistence fails after applying the side effect.
		if launchAtLoginChanged && prev != nil {
			_ = setLaunchAtLogin(prev.UI.LaunchAtLogin)
		}
		b.mu.Unlock()
		return err
	}
	b.cfg = &next
	b.state.ActiveNodeID = next.ActiveNodeID
	if node := b.findNode(next.ActiveNodeID); node != nil {
		b.state.ActiveNodeName = node.Name
	}
	b.emitStateLocked()
	portForwards := append([]PortForwardRule(nil), next.PortForwards...)
	b.mu.Unlock()

	if launchAtLoginChanged {
		if launchAtLoginEnabled {
			b.addLog("info", "app", "launch at login enabled")
		} else {
			b.addLog("info", "app", "launch at login disabled")
		}
	}

	// Apply may emit logs; do it outside b.mu to avoid self-deadlock via b.addLog.
	b.pfMgr.Apply(portForwards)

	if wasRunning && prev != nil && configChangeRequiresRestart(*prev, next) {
		b.addLog("info", "app", "config changed; restarting proxy to apply runtime settings")
		go func(withTun bool) {
			_ = b.RestartProxy(StartRequest{WithTun: withTun})
		}(wasTun)
	}
	return nil
}

func configChangeRequiresRestart(prev AppConfig, next AppConfig) bool {
	if strings.TrimSpace(prev.ActiveNodeID) != strings.TrimSpace(next.ActiveNodeID) {
		return true
	}
	if strings.ToLower(strings.TrimSpace(prev.Routing.ProxyMode)) != strings.ToLower(strings.TrimSpace(next.Routing.ProxyMode)) {
		return true
	}
	if prev.Routing.CustomRulesEnabled != next.Routing.CustomRulesEnabled ||
		strings.TrimSpace(prev.Routing.CustomRules) != strings.TrimSpace(next.Routing.CustomRules) {
		return true
	}
	if strings.Join(prev.Routing.RuleURLs, "\n") != strings.Join(next.Routing.RuleURLs, "\n") {
		return true
	}

	// TUN/runtime behavior.
	if prev.Tun.MapDNSEnabled != next.Tun.MapDNSEnabled ||
		strings.TrimSpace(prev.Tun.MapDNSAddress) != strings.TrimSpace(next.Tun.MapDNSAddress) ||
		prev.Tun.MapDNSPort != next.Tun.MapDNSPort ||
		prev.Tun.BlockQUIC != next.Tun.BlockQUIC {
		return true
	}
	if strings.TrimSpace(prev.Tun.InterfaceName) != strings.TrimSpace(next.Tun.InterfaceName) ||
		strings.TrimSpace(prev.Tun.IPv4) != strings.TrimSpace(next.Tun.IPv4) {
		return true
	}

	// Core settings that affect listeners/logging.
	if prev.Core.LocalPort != next.Core.LocalPort ||
		strings.TrimSpace(prev.Core.LogLevel) != strings.TrimSpace(next.Core.LogLevel) ||
		strings.TrimSpace(prev.Core.WorkingDir) != strings.TrimSpace(next.Core.WorkingDir) {
		return true
	}
	return false
}

func (b *Backend) UpsertNode(node NodeConfig) (NodeConfig, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if node.ID == "" {
		node.ID = newID("node_")
	}
	if strings.TrimSpace(node.Name) == "" {
		node.Name = "Node " + strconvBase36(time.Now().Unix())
	}
	normalizeNode(&node, b.cfg.Core.LocalPort)
	updated := false
	for i := range b.cfg.Nodes {
		if b.cfg.Nodes[i].ID == node.ID {
			b.cfg.Nodes[i] = node
			updated = true
			break
		}
	}
	if !updated {
		b.cfg.Nodes = append(b.cfg.Nodes, node)
	}
	if b.cfg.ActiveNodeID == "" {
		b.cfg.ActiveNodeID = node.ID
	}
	if err := b.store.Save(b.cfg); err != nil {
		return NodeConfig{}, err
	}
	b.emitStateLocked()
	return node, nil
}

func (b *Backend) DeleteNode(nodeID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	idx := -1
	for i := range b.cfg.Nodes {
		if b.cfg.Nodes[i].ID == nodeID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	b.cfg.Nodes = append(b.cfg.Nodes[:idx], b.cfg.Nodes[idx+1:]...)
	delete(b.latencyByID, nodeID)
	if b.cfg.ActiveNodeID == nodeID {
		b.cfg.ActiveNodeID = ""
		if len(b.cfg.Nodes) > 0 {
			b.cfg.ActiveNodeID = b.cfg.Nodes[0].ID
		}
	}
	if err := b.store.Save(b.cfg); err != nil {
		return err
	}
	b.emitStateLocked()
	return nil
}

func (b *Backend) SetActiveNode(nodeID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	node := b.findNode(nodeID)
	if node == nil {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	b.cfg.ActiveNodeID = nodeID
	if err := b.store.Save(b.cfg); err != nil {
		return err
	}
	b.state.ActiveNodeID = nodeID
	b.state.ActiveNodeName = node.Name
	b.emitStateLocked()
	return nil
}

func (b *Backend) ImportShortLink(name string, link string) (NodeConfig, error) {
	node, err := ParseShortLink(strings.TrimSpace(link))
	if err != nil {
		return NodeConfig{}, err
	}
	if strings.TrimSpace(name) != "" {
		node.Name = strings.TrimSpace(name)
	}
	return b.UpsertNode(*node)
}

func (b *Backend) ExportShortLink(nodeID string) (string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	node := b.findNode(nodeID)
	if node == nil {
		return "", fmt.Errorf("node not found")
	}
	return BuildShortLink(*node)
}

func (b *Backend) findNode(id string) *NodeConfig {
	for i := range b.cfg.Nodes {
		if b.cfg.Nodes[i].ID == id {
			return &b.cfg.Nodes[i]
		}
	}
	return nil
}
