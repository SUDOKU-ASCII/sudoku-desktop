package main

import (
	"context"
	"io/fs"
	"strings"

	"github.com/SUDOKU-ASCII/sudoku-desktop/internal/core"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// App struct
type App struct {
	backend *core.Backend

	runtimeFS   fs.FS
	runtimeRoot string
}

// NewApp creates a new App application struct
func NewApp(runtimeFS fs.FS, runtimeRoot string) *App {
	backend, _ := core.NewBackendWithRuntimeFS(runtimeFS, runtimeRoot)
	return &App{
		backend:     backend,
		runtimeFS:   runtimeFS,
		runtimeRoot: runtimeRoot,
	}
}

func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if a.backend == nil {
		b, err := core.NewBackendWithRuntimeFS(a.runtimeFS, a.runtimeRoot)
		if err != nil {
			return err
		}
		a.backend = b
	}
	if a.backend != nil {
		app := application.Get()
		a.backend.SetEventEmitter(func(name string, payload any) {
			if app != nil {
				app.Event.Emit(name, payload)
			}
		})
		a.backend.Startup(ctx)
	}
	return nil
}

func (a *App) ServiceShutdown() error {
	if a.backend != nil {
		a.backend.Shutdown()
	}
	return nil
}

func (a *App) trayIsRunning() bool {
	state := a.GetState()
	return state.Running
}

func (a *App) trayToggleProxy() error {
	if a.trayIsRunning() {
		return a.StopProxy()
	}
	cfg := a.GetConfig()
	return a.StartProxy(core.StartRequest{WithTun: cfg.Tun.Enabled})
}

func (a *App) traySetProxyMode(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "global", "direct", "pac":
	default:
		return nil
	}
	cfg := a.GetConfig()
	cfg.Routing.ProxyMode = mode
	return a.SaveConfig(cfg)
}

func (a *App) trayCurrentProxyMode() string {
	cfg := a.GetConfig()
	mode := strings.ToLower(strings.TrimSpace(cfg.Routing.ProxyMode))
	switch mode {
	case "global", "direct", "pac":
		return mode
	default:
		return "pac"
	}
}

func (a *App) GetConfig() core.AppConfig {
	if a.backend == nil {
		return core.AppConfig{}
	}
	return a.backend.GetConfig()
}

func (a *App) SaveConfig(cfg core.AppConfig) error {
	if a.backend == nil {
		return nil
	}
	return a.backend.SaveConfig(cfg)
}

func (a *App) GetState() core.RuntimeState {
	if a.backend == nil {
		return core.RuntimeState{}
	}
	return a.backend.GetState()
}

func (a *App) StartProxy(req core.StartRequest) error {
	if a.backend == nil {
		return nil
	}
	return a.backend.StartProxy(req)
}

func (a *App) StopProxy() error {
	if a.backend == nil {
		return nil
	}
	return a.backend.StopProxy()
}

func (a *App) RestartProxy(req core.StartRequest) error {
	if a.backend == nil {
		return nil
	}
	return a.backend.RestartProxy(req)
}

func (a *App) UpsertNode(node core.NodeConfig) (core.NodeConfig, error) {
	if a.backend == nil {
		return core.NodeConfig{}, nil
	}
	return a.backend.UpsertNode(node)
}

func (a *App) DeleteNode(nodeID string) error {
	if a.backend == nil {
		return nil
	}
	return a.backend.DeleteNode(nodeID)
}

func (a *App) SetActiveNode(nodeID string) error {
	if a.backend == nil {
		return nil
	}
	return a.backend.SetActiveNode(nodeID)
}

func (a *App) SwitchNode(nodeID string) error {
	if a.backend == nil {
		return nil
	}
	return a.backend.SwitchNode(nodeID)
}

func (a *App) ImportShortLink(name string, link string) (core.NodeConfig, error) {
	if a.backend == nil {
		return core.NodeConfig{}, nil
	}
	return a.backend.ImportShortLink(name, link)
}

func (a *App) ExportShortLink(nodeID string) (string, error) {
	if a.backend == nil {
		return "", nil
	}
	return a.backend.ExportShortLink(nodeID)
}

func (a *App) ProbeNode(nodeID string) (core.LatencyResult, error) {
	if a.backend == nil {
		return core.LatencyResult{}, nil
	}
	return a.backend.ProbeNode(nodeID)
}

func (a *App) ProbeAllNodes() []core.LatencyResult {
	if a.backend == nil {
		return nil
	}
	return a.backend.ProbeAllNodes()
}

func (a *App) AutoSelectLowestLatency() (core.LatencyResult, error) {
	if a.backend == nil {
		return core.LatencyResult{}, nil
	}
	return a.backend.AutoSelectLowestLatency()
}

func (a *App) SortNodesByName() error {
	if a.backend == nil {
		return nil
	}
	return a.backend.SortNodesByName()
}

func (a *App) SortNodesByLatency() error {
	if a.backend == nil {
		return nil
	}
	return a.backend.SortNodesByLatency()
}

func (a *App) DetectIPDirect() core.IPDetectResult {
	if a.backend == nil {
		return core.IPDetectResult{}
	}
	return a.backend.DetectIPDirect()
}

func (a *App) DetectIPProxy() core.IPDetectResult {
	if a.backend == nil {
		return core.IPDetectResult{}
	}
	return a.backend.DetectIPProxy()
}

func (a *App) StartReverseForwarder() error {
	if a.backend == nil {
		return nil
	}
	return a.backend.StartReverseForwarder()
}

func (a *App) StopReverseForwarder() error {
	if a.backend == nil {
		return nil
	}
	return a.backend.StopReverseForwarder()
}

func (a *App) GetLogs(level string, limit int) []core.LogEntry {
	if a.backend == nil {
		return nil
	}
	return a.backend.GetLogs(level, limit)
}

func (a *App) GetConnections() []core.ActiveConnection {
	if a.backend == nil {
		return nil
	}
	return a.backend.GetConnections()
}

func (a *App) CloseConnection(connectionID string) error {
	if a.backend == nil {
		return nil
	}
	return a.backend.CloseConnection(connectionID)
}

func (a *App) CloseAllConnections() error {
	if a.backend == nil {
		return nil
	}
	return a.backend.CloseAllConnections()
}

func (a *App) GetUsageHistory(limit int) []core.UsageDay {
	if a.backend == nil {
		return nil
	}
	return a.backend.GetUsageHistory(limit)
}

func (a *App) EnsureCoreBinaries() error {
	if a.backend == nil {
		return nil
	}
	return a.backend.EnsureCoreBinaries()
}

func (a *App) BuildInfo() map[string]string {
	if a.backend == nil {
		return map[string]string{}
	}
	return a.backend.BuildInfo()
}

func (a *App) ValidateYAML(content string) error {
	if a.backend == nil {
		return nil
	}
	return a.backend.ValidateYAML(content)
}

func (a *App) OpenRuntimeDir() string {
	if a.backend == nil {
		return ""
	}
	return a.backend.OpenRuntimeDir()
}

func (a *App) OpenConfigPath() string {
	if a.backend == nil {
		return ""
	}
	return a.backend.OpenConfigPath()
}

// TunHasPrivileges reports whether the app can perform privileged TUN operations
// silently on the current platform (supported on macOS/Linux via in-app sudo caching).
func (a *App) TunHasPrivileges() bool {
	if a.backend == nil {
		return false
	}
	return a.backend.TunHasPrivileges()
}

// TunAcquirePrivileges validates and caches the system password for sudo-backed TUN
// operations. The password is stored in-memory only and never written to disk.
func (a *App) TunAcquirePrivileges(password string) error {
	if a.backend == nil {
		return nil
	}
	return a.backend.TunAcquirePrivileges(password)
}

// TunDropPrivileges clears any cached admin credentials used for TUN operations.
func (a *App) TunDropPrivileges() error {
	if a.backend == nil {
		return nil
	}
	return a.backend.TunDropPrivileges()
}
