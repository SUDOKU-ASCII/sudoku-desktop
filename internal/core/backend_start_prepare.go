package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type proxyStartPlan struct {
	node              NodeConfig
	runtimeConfig     *AppConfig
	runtimeWarnings   []string
	withTun           bool
	sudokuConfigPath  string
	hevConfigPath     string
	localPort         int
	workDir           string
	sudokuBinary      string
	hevBinary         string
	coreLogLevel      string
	trafficStatsFile  string
	routing           RoutingSettings
	tun               TunSettings
	portForwards      []PortForwardRule
	staleRouteState   *routeContext
	staleTunRunning   bool
	staleTunInterface string
}

// prepareProxyStartLocked builds an immutable snapshot for one start operation.
// The caller must hold b.mu.
func (b *Backend) prepareProxyStartLocked(req StartRequest) (proxyStartPlan, error) {
	plan := proxyStartPlan{
		staleRouteState:   b.routeState,
		staleTunRunning:   b.tunRunningLocked(),
		staleTunInterface: strings.TrimSpace(b.runningTunInterface),
	}
	if err := b.ensureCoreBinariesLocked(); err != nil {
		b.setStartErrorLocked(err)
		return proxyStartPlan{}, err
	}

	node := b.findNode(b.cfg.ActiveNodeID)
	if node == nil {
		return proxyStartPlan{}, errors.New("no active node")
	}
	plan.node = *node
	plan.withTun = req.WithTun && b.cfg.Tun.Enabled

	runtimeConfig, warnings, err := effectiveRuntimeConfig(b.cfg, plan.withTun)
	if err != nil {
		b.setStartErrorLocked(err)
		return proxyStartPlan{}, err
	}
	sudokuConfigPath, hevConfigPath, localPort, err := writeRuntimeConfigs(b.store, runtimeConfig, plan.node, b.pacURL)
	if err != nil {
		b.setStartErrorLocked(err)
		return proxyStartPlan{}, err
	}

	plan.runtimeConfig = runtimeConfig
	plan.runtimeWarnings = warnings
	plan.sudokuConfigPath = sudokuConfigPath
	plan.hevConfigPath = hevConfigPath
	plan.localPort = localPort
	plan.workDir = runtimeConfig.Core.WorkingDir
	plan.sudokuBinary = runtimeConfig.Core.SudokuBinary
	plan.hevBinary = runtimeConfig.Core.HevBinary
	plan.coreLogLevel = runtimeConfig.Core.LogLevel
	plan.trafficStatsFile = filepath.Join(b.store.RuntimeDir(), "traffic_stats.json")
	plan.routing = runtimeConfig.Routing
	plan.tun = runtimeConfig.Tun
	plan.portForwards = append([]PortForwardRule(nil), runtimeConfig.PortForwards...)
	return plan, nil
}

func (b *Backend) setStartErrorLocked(err error) {
	if err == nil {
		return
	}
	b.state.LastError = err.Error()
	b.emitStateLocked()
}

func (b *Backend) cleanupStaleTunBeforeStart(plan *proxyStartPlan) error {
	if plan == nil {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return b.cleanupStaleDarwinTunBeforeStart(plan)
	case "linux":
		return b.cleanupStaleLinuxTunBeforeStart(plan)
	default:
		return nil
	}
}

func (b *Backend) cleanupStaleDarwinTunBeforeStart(plan *proxyStartPlan) error {
	detectedTunIf := strings.TrimSpace(darwinFindTunInterfaceByIPv4(plan.tun.IPv4))
	if plan.staleRouteState == nil && !plan.staleTunRunning && detectedTunIf == "" {
		return nil
	}

	tunIf := strings.TrimSpace(plan.staleTunInterface)
	if tunIf == "" {
		tunIf = detectedTunIf
	}
	if tunIf == "" {
		if routes, err := darwinNetstatRoutesIPv4(); err == nil {
			for _, route := range routes {
				if route.Destination == "default" &&
					darwinIsTunLikeInterface(route.Netif) &&
					strings.TrimSpace(route.Netif) != "" {
					tunIf = strings.TrimSpace(route.Netif)
					break
				}
			}
		}
	}
	if tunIf != "" {
		plan.tun.InterfaceName = tunIf
	}

	routeState := plan.staleRouteState
	if routeState == nil {
		physicalInterface, _ := darwinResolveOutboundBypassInterface(2 * time.Second)
		physicalInterface = strings.TrimSpace(physicalInterface)
		routeState = &routeContext{
			DefaultInterface: physicalInterface,
			PFAnchor:         fmt.Sprintf("com.apple/sudoku4x4.tun.%d", os.Getuid()),
		}
		if physicalInterface != "" {
			if service, err := darwinNetworkServiceForDevice(physicalInterface); err == nil && strings.TrimSpace(service) != "" {
				routeState.DarwinDNSSnapshots = []darwinDNSSnapshot{{
					Service:      strings.TrimSpace(service),
					WasAutomatic: true,
				}}
			}
		}
	}
	return b.teardownStaleTunState("darwin", routeState, plan.tun)
}

func (b *Backend) cleanupStaleLinuxTunBeforeStart(plan *proxyStartPlan) error {
	detectedRoutes := false
	if plan.tun.RouteTable > 0 {
		if out, err := exec.Command("ip", "rule", "show").CombinedOutput(); err == nil {
			needle := fmt.Sprintf("lookup %d", plan.tun.RouteTable)
			for _, raw := range strings.Split(string(out), "\n") {
				line := strings.TrimSpace(raw)
				if strings.HasPrefix(line, "20:") && strings.Contains(line, needle) {
					detectedRoutes = true
					break
				}
			}
		}
	}
	if detectedRoutes && strings.TrimSpace(plan.tun.InterfaceName) != "" {
		if out, err := exec.Command("ip", "route", "show", "table", fmt.Sprintf("%d", plan.tun.RouteTable)).CombinedOutput(); err == nil {
			routeOutput := string(out)
			if !strings.Contains(routeOutput, "default") ||
				!strings.Contains(routeOutput, "dev "+strings.TrimSpace(plan.tun.InterfaceName)) {
				detectedRoutes = false
			}
		}
	}
	if plan.staleRouteState == nil && !plan.staleTunRunning && !detectedRoutes {
		return nil
	}

	routeState := plan.staleRouteState
	if routeState == nil {
		routeState = &routeContext{
			ServerIP:              resolveServerIPFromAddress(plan.node.ServerAddress),
			LinuxResolvConfBackup: fmt.Sprintf("/tmp/sudoku4x4-resolv.conf.%d.bak", os.Getuid()),
		}
		if sourceIP, err := linuxDefaultOutboundIPv4(); err == nil && strings.TrimSpace(sourceIP) != "" {
			routeState.LinuxOutboundSrcIP = strings.TrimSpace(sourceIP)
		}
	}
	return b.teardownStaleTunState("linux", routeState, plan.tun)
}

func (b *Backend) teardownStaleTunState(platform string, routeState *routeContext, tun TunSettings) error {
	b.addLog("warn", "tun", platform+": detected stale TUN state; tearing down before start")
	if err := teardownRoutes(routeState, tun, func(line string) {
		b.addLog("info", "route", line)
	}); err != nil {
		b.mu.Lock()
		b.state.NeedsAdmin = isLikelyPermissionError(err)
		b.state.RouteSetupError = err.Error()
		b.setStartErrorLocked(err)
		b.mu.Unlock()
		return err
	}
	if err := b.stopTunLocked(6 * time.Second); err != nil && b.tunRunningLocked() {
		b.mu.Lock()
		b.setStartErrorLocked(err)
		b.mu.Unlock()
		return err
	}

	b.mu.Lock()
	b.routeState = nil
	b.runningTunInterface = ""
	b.tunRecovering = false
	b.state.TunRunning = false
	b.state.NeedsAdmin = false
	b.state.RouteSetupError = ""
	b.emitStateLocked()
	b.mu.Unlock()
	return nil
}

func baseCoreEnvironment(logLevel, trafficStatsFile, ruleCacheDir string) []string {
	return []string{
		"SUDOKU_LOG_LEVEL=" + logLevel,
		"SUDOKU_TRAFFIC_REPORT=1",
		"SUDOKU_TRAFFIC_INTERVAL_MS=1000",
		"SUDOKU_TRAFFIC_FILE=" + trafficStatsFile,
		"SUDOKU_RULE_CACHE_DIR=" + ruleCacheDir,
	}
}
