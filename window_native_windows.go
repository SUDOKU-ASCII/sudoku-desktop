//go:build windows

package main

import (
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

var sessionEndHandler atomic.Pointer[func()]

// onSessionEnd runs fn synchronously when Windows logs off or shuts down, so the system
// proxy and routes are restored before the process is killed.
func onSessionEnd(fn func()) {
	sessionEndHandler.Store(&fn)
}

func windowsWndProcInterceptor(hwnd uintptr, msg uint32, wParam, lParam uintptr) (uintptr, bool) {
	if msg != w32.WM_ENDSESSION || wParam == 0 {
		return 0, false
	}
	if fn := sessionEndHandler.Load(); fn != nil {
		done := make(chan struct{})
		go func() {
			defer close(done)
			(*fn)()
		}()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
		}
	}
	return 0, false
}
