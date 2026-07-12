package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetRoutingModePersistsWhenProxyIsStopped(t *testing.T) {
	root := t.TempDir()
	store := &Store{
		rootDir:    root,
		configPath: filepath.Join(root, "config.json"),
		runtimeDir: filepath.Join(root, "runtime"),
		logDir:     filepath.Join(root, "logs"),
	}
	cfg := DefaultConfig(store.runtimeDir)
	b := &Backend{store: store, cfg: cfg}

	if err := b.SetRoutingMode("global"); err != nil {
		t.Fatalf("set routing mode: %v", err)
	}
	if b.cfg.Routing.ProxyMode != "global" {
		t.Fatalf("expected in-memory mode global, got %q", b.cfg.Routing.ProxyMode)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if loaded.Routing.ProxyMode != "global" {
		t.Fatalf("expected persisted mode global, got %q", loaded.Routing.ProxyMode)
	}
}

func TestSetRoutingModeRestoresMemoryWhenSaveFails(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block"), 0o644); err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	store := &Store{
		rootDir:    blocker,
		configPath: filepath.Join(blocker, "config.json"),
		runtimeDir: filepath.Join(blocker, "runtime"),
		logDir:     filepath.Join(blocker, "logs"),
	}
	cfg := DefaultConfig(filepath.Join(root, "runtime"))
	previous := cfg.Routing.ProxyMode
	b := &Backend{store: store, cfg: cfg}

	if err := b.SetRoutingMode("global"); err == nil {
		t.Fatal("expected save failure")
	}
	if b.cfg.Routing.ProxyMode != previous {
		t.Fatalf("expected in-memory mode %q after save failure, got %q", previous, b.cfg.Routing.ProxyMode)
	}
}
