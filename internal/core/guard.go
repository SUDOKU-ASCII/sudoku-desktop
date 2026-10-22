package core

import (
	"os"
	"os/exec"
	"strconv"
	"time"
)

// startGuard spawns the detached guard: a tiny second instance of this executable that
// outlives a crashed or force-killed app and undoes its system changes (system proxy,
// orphaned core process) from the journal. The elevated TUN helper has its own watchdog.
func startGuard(exe string) error {
	cmd := exec.Command(exe, "--guard", strconv.Itoa(os.Getpid()))
	detachProcessSysProcAttr(cmd)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// RunGuard is the entry point of `--guard <ppid>`. It blocks until the parent is gone, then
// replays the journal once.
func RunGuard(parentPID int, journalPath string) {
	if parentPID <= 0 {
		return
	}
	for processAlive(parentPID) {
		time.Sleep(500 * time.Millisecond)
	}
	// The parent may have been mid-shutdown; give it a moment to clear the journal itself.
	time.Sleep(300 * time.Millisecond)
	recoverStaleJournal(journalPath, nil)
}
