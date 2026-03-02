package core

import "time"

type adminDetachedProcess interface {
	Start(command string, args []string, workdir string, pidFile string, logFile string) (int, error)
	Stop(timeout time.Duration) error
	IsRunning() bool
	PID() int
}
