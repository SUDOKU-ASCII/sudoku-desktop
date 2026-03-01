package core

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func (b *Backend) ensureCoreBinariesLocked() error {
	if b.cfg == nil || b.store == nil {
		return errors.New("backend not initialized")
	}
	if strings.TrimSpace(b.cfg.Core.SudokuBinary) == "" {
		b.cfg.Core.SudokuBinary = defaultBinaryPath(b.store.RuntimeDir(), "sudoku")
	}
	if strings.TrimSpace(b.cfg.Core.HevBinary) == "" {
		b.cfg.Core.HevBinary = defaultBinaryPath(b.store.RuntimeDir(), "hev-socks5-tunnel")
	}

	sudokuBin := b.cfg.Core.SudokuBinary
	hevBin := b.cfg.Core.HevBinary

	sudokuMissing := fileMissing(sudokuBin)
	hevMissing := fileMissing(hevBin)
	if sudokuMissing || hevMissing {
		// Try to install from bundled runtime directory.
		_ = b.installBundledRuntimeDir(filepath.Dir(sudokuBin))
		// In case user configured different directories for the two binaries.
		if filepath.Dir(hevBin) != filepath.Dir(sudokuBin) {
			_ = b.installBundledRuntimeDir(filepath.Dir(hevBin))
		}
	}

	if fileMissing(sudokuBin) {
		return fmt.Errorf("sudoku binary not found: %s", sudokuBin)
	}
	if fileMissing(hevBin) {
		return fmt.Errorf("hev binary not found: %s", hevBin)
	}
	return nil
}

func fileMissing(path string) bool {
	if strings.TrimSpace(path) == "" {
		return true
	}
	_, err := os.Stat(path)
	return err != nil
}

func (b *Backend) installBundledRuntimeDir(destDir string) error {
	srcDir, err := findBundledRuntimePlatformDir()
	if err != nil {
		return err
	}
	if err := ensureDir(destDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		srcPath := filepath.Join(srcDir, name)
		dstPath := filepath.Join(destDir, name)
		if !fileMissing(dstPath) {
			continue
		}
		mode := bundledFileMode(name)
		if err := copyFileAtomic(srcPath, dstPath, mode); err != nil {
			return err
		}
	}
	return nil
}

func bundledFileMode(name string) os.FileMode {
	if runtime.GOOS == "windows" {
		return 0o644
	}
	lower := strings.ToLower(name)
	if lower == "sudoku" || lower == "hev-socks5-tunnel" || strings.HasSuffix(lower, ".exe") {
		return 0o755
	}
	return 0o644
}

func findBundledRuntimePlatformDir() (string, error) {
	platform := runtime.GOOS + "-" + runtime.GOARCH
	candidates := make([]string, 0, 6)

	exe, err := os.Executable()
	if err == nil && exe != "" {
		exeDir := filepath.Dir(exe)
		if runtime.GOOS == "darwin" {
			candidates = append(candidates, filepath.Join(exeDir, "..", "Resources", "runtime", "bin", platform))
		}
		candidates = append(candidates, filepath.Join(exeDir, "runtime", "bin", platform))
	}

	if wd, err := os.Getwd(); err == nil && wd != "" {
		candidates = append(candidates, filepath.Join(wd, "runtime", "bin", platform))
	}

	for _, dir := range candidates {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		return dir, nil
	}
	return "", os.ErrNotExist
}

func copyFileAtomic(srcPath, dstPath string, mode os.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	if err := ensureDir(filepath.Dir(dstPath)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dstPath), filepath.Base(dstPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := io.Copy(tmp, src); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if mode != 0 {
		_ = os.Chmod(tmpPath, mode)
	}
	_ = os.Remove(dstPath)
	if err := os.Rename(tmpPath, dstPath); err != nil {
		cleanup()
		return err
	}
	return nil
}
