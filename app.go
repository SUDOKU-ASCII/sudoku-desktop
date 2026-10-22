package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SUDOKU-ASCII/sudoku-desktop/internal/core"
	"github.com/SUDOKU-ASCII/sudoku/pkg/logx"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// App struct
type App struct {
	backend      *core.Backend
	shutdownOnce sync.Once
}

// NewApp creates a new App application struct
func NewApp(runtimeFS fs.FS, runtimeRoot string) *App {
	backend, err := core.NewBackendWithRuntimeFS(runtimeFS, runtimeRoot)
	if err != nil {
		logx.Errorf("Desktop", "backend init failed: %v", err)
		os.Exit(1)
	}
	return &App{backend: backend}
}

func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.backend.SetEventEmitter(func(name string, payload any) {
		if app := application.Get(); app != nil {
			app.Event.Emit(name, payload)
		}
	})
	a.backend.Startup(ctx)
	return nil
}

func (a *App) ServiceShutdown() error {
	a.shutdownBackend()
	return nil
}

func (a *App) shutdownBackend() {
	a.shutdownOnce.Do(a.backend.Shutdown)
}

func (a *App) ShutdownNow() {
	a.shutdownBackend()
}

func (a *App) trayIsRunning() bool {
	state := a.GetState()
	return state.Phase != core.PhaseStopped
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
	return a.SetRoutingMode(mode)
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
	return a.backend.GetConfig()
}

func (a *App) SaveConfig(cfg core.AppConfig) error {
	return a.backend.SaveConfig(cfg)
}

func (a *App) SetRoutingMode(mode string) error {
	return a.backend.SetRoutingMode(mode)
}

func (a *App) GetState() core.RuntimeState {
	return a.backend.GetState()
}

func (a *App) StartProxy(req core.StartRequest) error {
	return swallowCancel(a.backend.StartProxy(req))
}

func (a *App) StopProxy() error {
	return a.backend.StopProxy()
}

func (a *App) RestartProxy(req core.StartRequest) error {
	return swallowCancel(a.backend.RestartProxy(req))
}

func (a *App) UpsertNode(node core.NodeConfig) (core.NodeConfig, error) {
	return a.backend.UpsertNode(node)
}

func (a *App) DeleteNode(nodeID string) error {
	return a.backend.DeleteNode(nodeID)
}

func (a *App) SetActiveNode(nodeID string) error {
	return a.backend.SetActiveNode(nodeID)
}

func (a *App) SwitchNode(nodeID string) error {
	return a.backend.SwitchNode(nodeID)
}

func (a *App) ImportShortLink(name string, link string) (core.NodeConfig, error) {
	return a.backend.ImportShortLink(name, link)
}

func (a *App) ExportShortLink(nodeID string) (string, error) {
	return a.backend.ExportShortLink(nodeID)
}

func (a *App) ProbeNode(nodeID string) (core.LatencyResult, error) {
	return a.backend.ProbeNode(nodeID)
}

func (a *App) ProbeAllNodes() []core.LatencyResult {
	return a.backend.ProbeAllNodes()
}

func (a *App) AutoSelectLowestLatency() (core.LatencyResult, error) {
	return a.backend.AutoSelectLowestLatency()
}

func (a *App) SortNodesByName() error {
	return a.backend.SortNodesByName()
}

func (a *App) SortNodesByLatency() error {
	return a.backend.SortNodesByLatency()
}

func (a *App) DetectIPDirect() core.IPDetectResult {
	return a.backend.DetectIPDirect()
}

func (a *App) DetectIPProxy() core.IPDetectResult {
	return a.backend.DetectIPProxy()
}

func (a *App) GetLANProxyInfo() core.LANProxyInfo {
	return a.backend.GetLANProxyInfo()
}

func (a *App) StartReverseForwarder() error {
	return a.backend.StartReverseForwarder()
}

func (a *App) StopReverseForwarder() error {
	return a.backend.StopReverseForwarder()
}

func (a *App) GetLogs(level string, limit int) []core.LogEntry {
	return a.backend.GetLogs(level, limit)
}

func (a *App) ExportLogs(entries []core.LogEntry) (string, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("application is not ready")
	}
	filename := "sudoku-logs-" + time.Now().Format("2006-01-02-15-04-05") + ".csv"
	path, err := app.Dialog.SaveFile().
		SetFilename(filename).
		AddFilter("CSV Files", "*.csv").
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(path) == "" {
		return "", err
	}
	if filepath.Ext(path) == "" {
		path += ".csv"
	}
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	writer := csv.NewWriter(file)
	writeErr := writer.Write([]string{"timestamp", "level", "component", "message"})
	for _, entry := range entries {
		if writeErr != nil {
			break
		}
		writeErr = writer.Write([]string{
			entry.Timestamp.Format(time.RFC3339Nano),
			entry.Level,
			entry.Component,
			firstNonEmpty(entry.Message, entry.Raw),
		})
	}
	writer.Flush()
	if writeErr == nil {
		writeErr = writer.Error()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return path, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (a *App) GetConnections() []core.ActiveConnection {
	return a.backend.GetConnections()
}

func (a *App) CloseConnection(connectionID string) error {
	return a.backend.CloseConnection(connectionID)
}

func (a *App) CloseAllConnections() error {
	return a.backend.CloseAllConnections()
}

func (a *App) GetUsageHistory(limit int) []core.UsageDay {
	return a.backend.GetUsageHistory(limit)
}

func (a *App) EnsureCoreBinaries() error {
	return a.backend.EnsureCoreBinaries()
}

func (a *App) BuildInfo() map[string]string {
	return a.backend.BuildInfo()
}

func (a *App) ValidateYAML(content string) error {
	return a.backend.ValidateYAML(content)
}

func (a *App) OpenRuntimeDir() string {
	return a.backend.OpenRuntimeDir()
}

func (a *App) OpenConfigPath() string {
	return a.backend.OpenConfigPath()
}

func (a *App) ControllerInfo() core.ControllerInfo { return a.backend.ControllerInfo() }

// RepairNetwork undoes tunnel routes/DNS overrides left behind by a crashed session.
func (a *App) RepairNetwork() error { return swallowCancel(a.backend.RepairNetwork()) }

// TunHasPrivileges reports whether the tunnel helper can start without the password prompt.
func (a *App) TunHasPrivileges() bool { return a.backend.TunHasPrivileges() }

// TunAcquirePrivileges verifies the login password (kept in memory only) and resumes the
// action that was waiting for it.
func (a *App) TunAcquirePrivileges(password string) error {
	return a.backend.TunAcquirePrivileges(password)
}

// TunCancelPrivileges dismisses the password prompt.
func (a *App) TunCancelPrivileges() { a.backend.TunCancelPrivileges() }

// TunDropPrivileges forgets the password.
func (a *App) TunDropPrivileges() { a.backend.TunDropPrivileges() }

func (a *App) OpenURL(raw string) error {
	app := application.Get()
	if app == nil {
		return fmt.Errorf("application is not ready")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("refusing to open %q", raw)
	}
	return app.Browser.OpenURL(u.String())
}

// swallowCancel hides outcomes the UI learns from the runtime state instead: a start that
// was superseded by stop/restart, and an action parked behind the password prompt.
func swallowCancel(err error) error {
	if errors.Is(err, core.ErrStartCancelled) || errors.Is(err, core.ErrAdminRequired) {
		return nil
	}
	return err
}
