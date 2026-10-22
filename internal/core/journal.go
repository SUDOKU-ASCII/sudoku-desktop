package core

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// journalFile records every system-level side effect the app has made that must be
// undone if the app dies uncleanly. It is rewritten atomically on each change and deleted
// on a clean shutdown. A user-level guard process (and the next launch) replays it.
//
// The root-owned TUN state (routes, DNS, firewall rules) lives in the helper's own journal,
// see tun_journal.go.
type journalFile struct {
	Version     int                  `json:"version"`
	AppPID      int                  `json:"appPid"`
	UpdatedUnix int64                `json:"updatedUnix"`
	ProxyPort   int                  `json:"proxyPort,omitempty"`
	SystemProxy *systemProxySnapshot `json:"systemProxy,omitempty"`
	// Children are processes we spawned and must not outlive us: pid + a name fragment
	// that must appear in the process command line before we kill it (pid reuse guard).
	Children []journalChild `json:"children,omitempty"`
}

type journalChild struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

type journal struct {
	path string
	mu   sync.Mutex
	data journalFile
}

func newJournal(path string) *journal {
	return &journal{path: path, data: journalFile{Version: 1, AppPID: os.Getpid()}}
}

// Update mutates the journal and persists it atomically.
func (j *journal) Update(fn func(*journalFile)) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn(&j.data)
	j.data.AppPID = os.Getpid()
	j.data.UpdatedUnix = time.Now().Unix()
	if j.data.SystemProxy == nil && len(j.data.Children) == 0 {
		return j.removeLocked()
	}
	buf, err := json.Marshal(&j.data)
	if err != nil {
		return err
	}
	return writeFileAtomic(j.path, buf, 0o600)
}

func (j *journal) removeLocked() error {
	if err := os.Remove(j.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Clear drops the journal. Call only after everything it describes has been undone.
func (j *journal) Clear() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.data = journalFile{Version: 1, AppPID: os.Getpid()}
	return j.removeLocked()
}

func (j *journal) SetChild(name string, pid int) error {
	return j.Update(func(f *journalFile) {
		f.Children = removeChild(f.Children, name)
		if pid > 0 {
			f.Children = append(f.Children, journalChild{PID: pid, Name: name})
		}
	})
}

func removeChild(list []journalChild, name string) []journalChild {
	out := list[:0:0]
	for _, c := range list {
		if c.Name != name {
			out = append(out, c)
		}
	}
	return out
}

func readJournal(path string) (*journalFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f journalFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// recoverStaleJournal replays the journal of a dead app instance: restore the system proxy
// (only where it still points at us) and kill leftover children. It is a no-op when the
// journal's owner is alive. Returns true when something was recovered.
func recoverStaleJournal(path string, logf func(string)) bool {
	f, err := readJournal(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(path) // unreadable/torn: nothing trustworthy to replay
		}
		return false
	}
	if f.AppPID > 0 && f.AppPID != os.Getpid() && processAlive(f.AppPID) {
		return false
	}
	if logf == nil {
		logf = func(string) {}
	}
	for _, c := range f.Children {
		if cmd := processCommand(c.PID); cmd != "" && strings.Contains(strings.ToLower(cmd), strings.ToLower(c.Name)) {
			logf("recover: killing leftover " + c.Name + " (pid " + strconv.Itoa(c.PID) + ")")
			killPID(c.PID)
		}
	}
	port := f.ProxyPort
	if port == 0 {
		port = ProxyPort
	}
	if f.SystemProxy != nil {
		if err := restoreSystemProxy(f.SystemProxy, port, logf); err != nil {
			logf("recover: restore system proxy: " + err.Error())
		} else {
			logf("recover: system proxy restored after unclean exit")
		}
	}
	_ = os.Remove(path)
	return true
}
