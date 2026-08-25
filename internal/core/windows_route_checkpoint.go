package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const windowsRouteCheckpointFile = "windows-route-checkpoint.json"

type windowsRouteCheckpoint struct {
	Route routeContext `json:"route"`
	Tun   TunSettings  `json:"tun"`
}

func (b *Backend) windowsRouteCheckpointPath() string {
	if b == nil || b.store == nil {
		return ""
	}
	return filepath.Join(b.store.RuntimeDir(), windowsRouteCheckpointFile)
}

func (b *Backend) saveWindowsRouteCheckpoint(route *routeContext, tun TunSettings) error {
	path := b.windowsRouteCheckpointPath()
	if path == "" || route == nil {
		return errors.New("windows route checkpoint is unavailable")
	}
	if route.TunIndex <= 0 || route.WindowsDefaultIfIndex <= 0 {
		return fmt.Errorf("invalid windows route checkpoint interfaces: tun=%d default=%d", route.TunIndex, route.WindowsDefaultIfIndex)
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	raw, err := json.Marshal(windowsRouteCheckpoint{Route: *route, Tun: tun})
	if err != nil {
		return fmt.Errorf("marshal windows route checkpoint: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write windows route checkpoint: %w", err)
	}
	return nil
}

func (b *Backend) removeWindowsRouteCheckpoint() {
	if path := b.windowsRouteCheckpointPath(); path != "" {
		_ = os.Remove(path)
	}
}

func (b *Backend) recoverWindowsRouteCheckpoint() error {
	path := b.windowsRouteCheckpointPath()
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read windows route checkpoint: %w", err)
	}
	var checkpoint windowsRouteCheckpoint
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		return fmt.Errorf("decode windows route checkpoint: %w", err)
	}
	if checkpoint.Route.TunIndex <= 0 || checkpoint.Route.WindowsDefaultIfIndex <= 0 {
		return fmt.Errorf(
			"invalid windows route checkpoint interfaces: tun=%d default=%d",
			checkpoint.Route.TunIndex,
			checkpoint.Route.WindowsDefaultIfIndex,
		)
	}
	b.addLog("warn", "route", "found an interrupted Windows TUN session; restoring routes and DNS before startup")
	if err := teardownRoutesWindows(&checkpoint.Route, checkpoint.Tun, func(line string) {
		b.addLog("info", "route", line)
	}); err != nil {
		return fmt.Errorf("recover interrupted windows TUN session: %w", err)
	}
	b.removeWindowsRouteCheckpoint()
	b.addLog("info", "route", "restored interrupted Windows TUN routes and DNS")
	return nil
}
