package core

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const defaultRuntimeRoot = "runtime/bin"

// runtimeSource locates the bundled helper binaries: an embedded FS (release builds) or
// a platform directory next to the executable (dev builds).
type runtimeSource struct {
	fsys fs.FS
	root string
}

func (s runtimeSource) platformDir() string {
	root := strings.Trim(strings.ReplaceAll(strings.TrimSpace(s.root), "\\", "/"), "/")
	if root == "" {
		root = defaultRuntimeRoot
	}
	return path.Join(root, runtime.GOOS+"-"+runtime.GOARCH)
}

// installBundledRuntime copies the bundled files accepted by keep (nil = all) into destDir,
// skipping files that are already identical.
func installBundledRuntime(fsys fs.FS, root, destDir string, keep func(name string) bool) error {
	src := runtimeSource{fsys: fsys, root: root}
	if fsys != nil {
		err := src.installFromFS(destDir, keep)
		if err == nil {
			return nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	dir, err := findBundledRuntimePlatformDir()
	if err != nil {
		return err
	}
	return installFromDir(dir, destDir, keep)
}

func (s runtimeSource) installFromFS(destDir string, keep func(string) bool) error {
	srcDir := s.platformDir()
	entries, err := fs.ReadDir(s.fsys, srcDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || (keep != nil && !keep(name)) {
			continue
		}
		content, err := fs.ReadFile(s.fsys, path.Join(srcDir, name))
		if err != nil {
			return err
		}
		dst := filepath.Join(destDir, name)
		if same, err := bytesLookIdentical(content, dst); err == nil && same {
			continue
		}
		if err := writeFileAtomic(dst, content, bundledFileMode(name)); err != nil {
			return err
		}
	}
	return nil
}

func installFromDir(srcDir, destDir string, keep func(string) bool) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	hostExe := currentExecutableBaseName()
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || (keep != nil && !keep(name)) || (hostExe != "" && strings.EqualFold(name, hostExe)) {
			continue
		}
		content, err := os.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			return err
		}
		dst := filepath.Join(destDir, name)
		if same, err := bytesLookIdentical(content, dst); err == nil && same {
			continue
		}
		if err := writeFileAtomic(dst, content, bundledFileMode(name)); err != nil {
			return err
		}
	}
	return nil
}

// ensureSudokuBinary makes sure the core binary exists (refreshing it from the bundle when
// it lives in the app's runtime dir) and returns its path. The TUN helper manages its own
// copy of the tunnel binary, so the app only ever needs the core.
func ensureSudokuBinary(core *CoreSettings, runtimeDir string, fsys fs.FS, root string) error {
	if strings.TrimSpace(core.SudokuBinary) == "" {
		core.SudokuBinary = defaultBinaryPath(runtimeDir, "sudoku")
	}
	bin := core.SudokuBinary
	if fileMissing(bin) || isWithinDir(bin, runtimeDir) {
		only := runtimeBinaryName("sudoku")
		err := installBundledRuntime(fsys, root, filepath.Dir(bin), func(n string) bool { return strings.EqualFold(n, only) })
		if err != nil && fileMissing(bin) {
			return fmt.Errorf("sudoku binary not found (%s): %w", bin, err)
		}
	}
	if fileMissing(bin) {
		return fmt.Errorf("sudoku binary not found: %s", bin)
	}
	return nil
}

func isWithinDir(p, dir string) bool {
	p, dir = strings.TrimSpace(p), strings.TrimSpace(dir)
	if p == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func fileMissing(p string) bool {
	if strings.TrimSpace(p) == "" {
		return true
	}
	_, err := os.Stat(p)
	return err != nil
}

func bytesLookIdentical(content []byte, dstPath string) (bool, error) {
	fi, err := os.Stat(dstPath)
	if err != nil {
		return false, err
	}
	if int64(len(content)) != fi.Size() {
		return false, nil
	}
	f, err := os.Open(dstPath)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	want := sha256.Sum256(content)
	return fmt.Sprintf("%x", h.Sum(nil)) == fmt.Sprintf("%x", want[:]), nil
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
	dir := filepath.Dir(hevBin)
	var missing []string
	for _, dep := range []string{"wintun.dll", "msys-2.0.dll"} {
		if fileMissing(filepath.Join(dir, dep)) {
			missing = append(missing, dep)
		}
	}
	return missing
}

func findBundledRuntimePlatformDir() (string, error) {
	platform := runtime.GOOS + "-" + runtime.GOARCH
	var candidates []string
	if exe, err := os.Executable(); err == nil && exe != "" {
		exeDir := filepath.Dir(exe)
		if runtime.GOOS == "darwin" {
			candidates = append(candidates, filepath.Join(exeDir, "..", "Resources", "runtime", "bin", platform))
		}
		candidates = append(candidates, exeDir, filepath.Join(exeDir, "runtime", "bin", platform))
	}
	if wd, err := os.Getwd(); err == nil && wd != "" {
		candidates = append(candidates, filepath.Join(wd, "runtime", "bin", platform))
	}
	for _, dir := range candidates {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue
		}
		if fileMissing(filepath.Join(dir, runtimeBinaryName("sudoku"))) || fileMissing(filepath.Join(dir, runtimeBinaryName("hev-socks5-tunnel"))) {
			continue
		}
		return dir, nil
	}
	return "", os.ErrNotExist
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
