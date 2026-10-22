package core

import (
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (b *Backend) GetConfig() AppConfig {
	if cfg := b.cfgClone(); cfg != nil {
		return *cfg
	}
	return AppConfig{}
}

// SaveConfig persists the config and applies what it can live; settings the core only reads
// at launch trigger one debounced restart.
func (b *Backend) SaveConfig(next AppConfig) error {
	b.mu.Lock()
	prev := cloneConfig(b.cfg)
	normalizeConfig(&next, b.store.RuntimeDir())
	launchChanged := prev.UI.LaunchAtLogin != next.UI.LaunchAtLogin
	if launchChanged {
		if err := setLaunchAtLogin(next.UI.LaunchAtLogin); err != nil {
			b.state.LastError = err.Error()
			b.mu.Unlock()
			b.markDirty()
			return err
		}
	}
	if err := b.store.Save(&next); err != nil {
		if launchChanged {
			_ = setLaunchAtLogin(prev.UI.LaunchAtLogin)
		}
		b.mu.Unlock()
		return err
	}
	b.cfg = &next
	b.mu.Unlock()
	b.markDirty()

	b.pf.Apply(append([]PortForwardRule(nil), next.PortForwards...))
	st := b.GetState()
	if st.Phase == PhaseStopped {
		return nil
	}
	if configChangeRequiresRestart(prev, &next) {
		b.scheduleRestart("config changed; restarting to apply runtime settings")
		return nil
	}
	if prev.Tun.Enabled != next.Tun.Enabled && st.Phase == PhaseRunning {
		go b.applyTunLive(next.Tun.Enabled)
	}
	if !strings.EqualFold(prev.Routing.ProxyMode, next.Routing.ProxyMode) {
		go b.applyModeLive(prev.Routing.ProxyMode, next.Routing.ProxyMode)
	}
	if prev.ActiveNodeID != next.ActiveNodeID {
		go b.applyNodeLive()
	}
	return nil
}

func (b *Backend) applyTunLive(want bool) {
	b.opMu.Lock()
	defer b.opMu.Unlock()
	if b.GetState().Phase != PhaseRunning {
		return
	}
	ctx, cancel := contextTimeout(helperUpTimeout + 10*time.Second)
	defer cancel()
	_ = b.applyTunLocked(ctx, want) // failures are surfaced through RuntimeState
}

// configChangeRequiresRestart reports changes the core or the tunnel can only pick up at
// launch. Routing mode and the active node are applied live instead.
func configChangeRequiresRestart(prev, next *AppConfig) bool {
	if !reflect.DeepEqual(prev.Nodes, next.Nodes) ||
		!reflect.DeepEqual(prev.Controller, next.Controller) ||
		!reflect.DeepEqual(prev.ReverseClient, next.ReverseClient) {
		return true
	}
	if !reflect.DeepEqual(prev.Routing.RuleURLs, next.Routing.RuleURLs) ||
		prev.Routing.CustomRulesEnabled != next.Routing.CustomRulesEnabled ||
		strings.TrimSpace(prev.Routing.CustomRules) != strings.TrimSpace(next.Routing.CustomRules) {
		return true
	}
	pt, nt := prev.Tun, next.Tun
	pt.Enabled, nt.Enabled = false, false
	if pt != nt {
		return true
	}
	return prev.Core.LogLevel != next.Core.LogLevel ||
		prev.Core.WorkingDir != next.Core.WorkingDir ||
		prev.Core.SudokuBinary != next.Core.SudokuBinary ||
		prev.Core.AllowLAN != next.Core.AllowLAN
}

func (b *Backend) UpsertNode(node NodeConfig) (NodeConfig, error) {
	b.mu.Lock()
	if node.ID == "" {
		node.ID = newID("node_")
	}
	if strings.TrimSpace(node.Name) == "" {
		node.Name = "Node " + strconvBase36(time.Now().Unix())
	}
	normalizeNode(&node)
	changed := true
	found := false
	for i := range b.cfg.Nodes {
		if b.cfg.Nodes[i].ID == node.ID {
			changed = !reflect.DeepEqual(b.cfg.Nodes[i], node)
			b.cfg.Nodes[i], found = node, true
			break
		}
	}
	if !found {
		b.cfg.Nodes = append(b.cfg.Nodes, node)
	}
	if b.cfg.ActiveNodeID == "" {
		b.cfg.ActiveNodeID = node.ID
	}
	if err := b.store.Save(b.cfg); err != nil {
		b.mu.Unlock()
		return NodeConfig{}, err
	}
	delete(b.latency, node.ID)
	b.mu.Unlock()
	b.markDirty()
	if changed && b.GetState().Phase != PhaseStopped {
		b.scheduleRestart("node list changed; restarting")
	}
	return node, nil
}

func (b *Backend) DeleteNode(nodeID string) error {
	b.mu.Lock()
	idx := -1
	for i := range b.cfg.Nodes {
		if b.cfg.Nodes[i].ID == nodeID {
			idx = i
			break
		}
	}
	if idx < 0 {
		b.mu.Unlock()
		return nil
	}
	b.cfg.Nodes = append(b.cfg.Nodes[:idx], b.cfg.Nodes[idx+1:]...)
	delete(b.latency, nodeID)
	if b.cfg.ActiveNodeID == nodeID || (b.cfg.ActiveNodeID == BalanceNodeID && len(b.cfg.Nodes) < 2) {
		b.cfg.ActiveNodeID = ""
		if len(b.cfg.Nodes) > 0 {
			b.cfg.ActiveNodeID = b.cfg.Nodes[0].ID
		}
	}
	err := b.store.Save(b.cfg)
	b.mu.Unlock()
	b.markDirty()
	if err == nil && b.GetState().Phase != PhaseStopped {
		b.scheduleRestart("node removed; restarting")
	}
	return err
}

// SetActiveNode picks the upstream node (or the Balance group) and applies it live.
func (b *Backend) SetActiveNode(nodeID string) error {
	b.mu.Lock()
	if nodeID == BalanceNodeID {
		if len(b.cfg.Nodes) < 2 {
			b.mu.Unlock()
			return fmt.Errorf("load balancing needs at least two nodes")
		}
	} else if b.findNodeLocked(nodeID) == nil {
		b.mu.Unlock()
		return fmt.Errorf("node not found: %s", nodeID)
	}
	b.cfg.ActiveNodeID = nodeID
	err := b.store.Save(b.cfg)
	b.mu.Unlock()
	b.markDirty()
	if err != nil {
		return err
	}
	if b.GetState().Phase == PhaseRunning {
		return b.applyNodeLive()
	}
	return nil
}

func (b *Backend) SwitchNode(nodeID string) error { return b.SetActiveNode(nodeID) }

func (b *Backend) applyNodeLive() error {
	k := b.kernelRef()
	if k == nil {
		return nil
	}
	cfg := b.cfgClone()
	ctx, cancel := contextTimeout(5 * time.Second)
	defer cancel()
	if err := k.selectNode(ctx, cfg, cfg.ActiveNodeID); err != nil {
		b.addLog("warn", "app", "live node switch failed, restarting: "+err.Error())
		b.scheduleRestart("restarting to switch node")
		return nil
	}
	b.addLog("info", "app", "switched upstream node")
	_ = k.ctl.CloseAllConnections(ctx)
	return nil
}

// SetRoutingMode switches pac/global/direct live, without restarting anything.
func (b *Backend) SetRoutingMode(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "global", "direct", "pac":
	default:
		return fmt.Errorf("invalid routing mode: %s", mode)
	}
	b.mu.Lock()
	prev := b.cfg.Routing.ProxyMode
	if strings.EqualFold(prev, mode) {
		b.mu.Unlock()
		return nil
	}
	b.cfg.Routing.ProxyMode = mode
	if err := b.store.Save(b.cfg); err != nil {
		b.cfg.Routing.ProxyMode = prev
		b.mu.Unlock()
		return err
	}
	b.mu.Unlock()
	b.markDirty()
	if b.GetState().Phase != PhaseRunning {
		return nil
	}
	return b.applyModeLive(prev, mode)
}

func (b *Backend) applyModeLive(prev, mode string) error {
	k := b.kernelRef()
	if k == nil {
		return nil
	}
	ctx, cancel := contextTimeout(5 * time.Second)
	err := k.ctl.SetMode(ctx, controllerMode(mode))
	cancel()
	if err != nil {
		b.addLog("error", "route", "apply routing mode failed: "+err.Error())
		b.update(func(s *RuntimeState) { s.LastError = err.Error() })
		return err
	}
	b.addLog("info", "route", "routing mode: "+mode)
	if (normalizeMode(prev) == "pac") != (mode == "pac") && b.GetState().TunRunning {
		if err := b.reapplyTun(); err != nil {
			return err
		}
	}
	return nil
}

func (b *Backend) ImportShortLink(name string, link string) (NodeConfig, error) {
	node, err := ParseShortLink(strings.TrimSpace(link))
	if err != nil {
		return NodeConfig{}, err
	}
	if n := strings.TrimSpace(name); n != "" {
		node.Name = n
	}
	return b.UpsertNode(*node)
}

func (b *Backend) ExportShortLink(nodeID string) (string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	node := b.findNodeLocked(nodeID)
	if node == nil {
		return "", fmt.Errorf("node not found")
	}
	return BuildShortLink(*node)
}

func (b *Backend) ImportNodeShare(name string, link string) (NodeConfig, error) {
	return b.ImportShortLink(name, link)
}

func (b *Backend) ExportNodeShare(nodeID string) (string, error) { return b.ExportShortLink(nodeID) }

func (b *Backend) findNodeLocked(id string) *NodeConfig {
	for i := range b.cfg.Nodes {
		if b.cfg.Nodes[i].ID == id {
			return &b.cfg.Nodes[i]
		}
	}
	return nil
}

func hasDuplicateNames(nodes []NodeConfig) bool {
	seen := map[string]bool{}
	for _, n := range nodes {
		if seen[n.Name] {
			return true
		}
		seen[n.Name] = true
	}
	return false
}

func (b *Backend) sortNodes(less func(a, c *NodeConfig) bool) error {
	b.mu.Lock()
	sort.SliceStable(b.cfg.Nodes, func(i, j int) bool { return less(&b.cfg.Nodes[i], &b.cfg.Nodes[j]) })
	err := b.store.Save(b.cfg)
	dup := hasDuplicateNames(b.cfg.Nodes)
	b.mu.Unlock()
	b.markDirty()
	if err == nil && dup && b.GetState().Phase != PhaseStopped {
		b.scheduleRestart("node order changed; restarting")
	}
	return err
}

func (b *Backend) SortNodesByName() error {
	return b.sortNodes(func(a, c *NodeConfig) bool { return strings.ToLower(a.Name) < strings.ToLower(c.Name) })
}

func (b *Backend) SortNodesByLatency() error {
	b.mu.RLock()
	lat := make(map[string]LatencyResult, len(b.latency))
	for k, v := range b.latency {
		lat[k] = v
	}
	b.mu.RUnlock()
	return b.sortNodes(func(a, c *NodeConfig) bool {
		la, aok := lat[a.ID]
		lc, cok := lat[c.ID]
		if aok != cok {
			return aok
		}
		if !aok {
			return strings.ToLower(a.Name) < strings.ToLower(c.Name)
		}
		if la.ConnectOK != lc.ConnectOK {
			return la.ConnectOK
		}
		if la.LatencyMs < 0 || lc.LatencyMs < 0 {
			return lc.LatencyMs < 0 && la.LatencyMs >= 0
		}
		return la.LatencyMs < lc.LatencyMs
	})
}

// ControllerInfo describes how to reach the external controller and offers dashboard links
// with the connection already filled in.
func (b *Backend) ControllerInfo() ControllerInfo {
	cfg := b.cfgClone()
	st := b.GetState()
	c := cfg.Controller
	port := strconv.Itoa(c.Port)
	info := ControllerInfo{
		Ready: st.CoreRunning, Port: c.Port, Secret: c.Secret, AllowLAN: c.AllowLAN,
		URL: "http://127.0.0.1:" + port, Version: st.Kernel.Version,
	}
	if c.AllowLAN {
		for _, ip := range localLANIPv4s() {
			info.LANURLs = append(info.LANURLs, "http://"+ip+":"+port)
		}
	}
	q := func(host string) string {
		v := url.Values{}
		v.Set("hostname", host)
		v.Set("port", port)
		v.Set("secret", c.Secret)
		return v.Encode()
	}
	info.Boards = []Board{
		{ID: "zashboard", Name: "Zashboard", URL: "https://board.zash.run.place/#/setup?http=true&" + q("127.0.0.1")},
		{ID: "metacubexd", Name: "MetaCubeXD", URL: "https://metacubex.github.io/metacubexd/#/setup?http=true&" + q("127.0.0.1")},
		{ID: "yacd", Name: "Yacd", URL: "https://yacd.metacubex.one/?http=true&" + q("127.0.0.1")},
	}
	return info
}
