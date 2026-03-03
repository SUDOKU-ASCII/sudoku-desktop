package core

import (
	"crypto/sha256"
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

	hevDepMissing := missingHevRuntimeDeps(hevBin)

	installed := map[string]bool{}
	installDir := func(dir string) {
		if dir == "" || installed[dir] {
			return
		}
		installed[dir] = true
		_ = b.installBundledRuntimeDir(dir)
	}

	// If binaries live under the app runtime dir, keep them updated from bundled runtime.
	// This is important in dev builds and when users upgrade without deleting their runtime dir.
	if sudokuMissing || isWithinDir(sudokuBin, b.store.RuntimeDir()) {
		installDir(filepath.Dir(sudokuBin))
	}
	if hevMissing || len(hevDepMissing) > 0 || isWithinDir(hevBin, b.store.RuntimeDir()) {
		installDir(filepath.Dir(hevBin))
	}

	if fileMissing(sudokuBin) {
		return fmt.Errorf("sudoku binary not found: %s", sudokuBin)
	}
	if fileMissing(hevBin) {
		return fmt.Errorf("hev binary not found: %s", hevBin)
	}
	if missing := missingHevRuntimeDeps(hevBin); len(missing) > 0 {
		return fmt.Errorf("hev runtime dependencies missing in %s: %s", filepath.Dir(hevBin), strings.Join(missing, ", "))
	}
	return nil
}

func isWithinDir(path string, dir string) bool {
	path = strings.TrimSpace(path)
	dir = strings.TrimSpace(dir)
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
	hostExe := currentExecutableBaseName()
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if hostExe != "" && strings.EqualFold(name, hostExe) {
			continue
		}
		srcPath := filepath.Join(srcDir, name)
		dstPath := filepath.Join(destDir, name)
		if !fileMissing(dstPath) {
			same, err := filesLookIdentical(srcPath, dstPath)
			if err == nil && same {
				continue
			}
		}
		mode := bundledFileMode(name)
		if err := copyFileAtomic(srcPath, dstPath, mode); err != nil {
			return err
		}
	}
	return nil
}

func filesLookIdentical(srcPath, dstPath string) (bool, error) {
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return false, err
	}
	dstInfo, err := os.Stat(dstPath)
	if err != nil {
		return false, err
	}
	if srcInfo.Size() != dstInfo.Size() {
		return false, nil
	}
	srcHash, err := fileSHA256(srcPath)
	if err != nil {
		return false, err
	}
	dstHash, err := fileSHA256(dstPath)
	if err != nil {
		return false, err
	}
	return srcHash == dstHash, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
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

func missingHevRuntimeDeps(hevBin string) []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	hevDir := filepath.Dir(hevBin)
	if strings.TrimSpace(hevDir) == "" {
		return []string{"wintun.dll", "msys-2.0.dll"}
	}
	deps := []string{"wintun.dll", "msys-2.0.dll"}
	missing := make([]string, 0, len(deps))
	for _, dep := range deps {
		if fileMissing(filepath.Join(hevDir, dep)) {
			missing = append(missing, dep)
		}
	}
	return missing
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
		candidates = append(candidates, exeDir)
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
		if !hasBundledRuntimeFiles(dir) {
			continue
		}
		return dir, nil
	}
	return "", os.ErrNotExist
}

func hasBundledRuntimeFiles(dir string) bool {
	required := []string{
		runtimeBinaryName("sudoku"),
		runtimeBinaryName("hev-socks5-tunnel"),
	}
	for _, name := range required {
		if fileMissing(filepath.Join(dir, name)) {
			return false
		}
	}
	return true
}

func runtimeBinaryName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func currentExecutableBaseName() string {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return ""
	}
	return filepath.Base(exe)
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
