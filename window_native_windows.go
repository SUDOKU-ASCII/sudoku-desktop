//go:build windows

package main

import (
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

var (
	minSizeWindow     atomic.Pointer[application.Window]
	minSizeHWND       atomic.Uintptr
	minSizeW          atomic.Int32
	minSizeH          atomic.Int32
	sessionEndHandler atomic.Pointer[func()]
)

// Wails clears the window's MinWidth/MinHeight options whenever it maximises or goes
// fullscreen (DisableSizeConstraints writes through to the shared options), so after one
// maximise the frameless window could be dragged down to nothing. Enforce the minimum here.
// The native window only exists once the app runs, so its handle is resolved lazily.
func enforceNativeMinSize(window application.Window, width, height int) {
	minSizeW.Store(int32(width))
	minSizeH.Store(int32(height))
	minSizeWindow.Store(&window)
}

func isMinSizeWindow(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	if cached := minSizeHWND.Load(); cached != 0 {
		return cached == hwnd
	}
	window := minSizeWindow.Load()
	if window == nil {
		return false
	}
	native := uintptr((*window).NativeWindow())
	if native == 0 {
		return false
	}
	minSizeHWND.Store(native)
	return native == hwnd
}

// onSessionEnd runs fn synchronously when Windows logs off or shuts down, so the system
// proxy and routes are restored before the process is killed.
func onSessionEnd(fn func()) {
	sessionEndHandler.Store(&fn)
}

func windowsWndProcInterceptor(hwnd uintptr, msg uint32, wParam, lParam uintptr) (uintptr, bool) {
	switch msg {
	case w32.WM_GETMINMAXINFO:
		if !isMinSizeWindow(hwnd) {
			return 0, false
		}
		dpi := int32(96)
		if w32.HasGetDpiForWindowFunc() {
			if d := int32(w32.GetDpiForWindow(hwnd)); d > 0 {
				dpi = d
			}
		}
		mmi := *(**w32.MINMAXINFO)(unsafe.Pointer(&lParam)) // lParam is a pointer owned by the OS
		mmi.PtMinTrackSize.X = max(mmi.PtMinTrackSize.X, minSizeW.Load()*dpi/96)
		mmi.PtMinTrackSize.Y = max(mmi.PtMinTrackSize.Y, minSizeH.Load()*dpi/96)
		return 0, true
	case w32.WM_ENDSESSION:
		if wParam == 0 {
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
	}
	return 0, false
}
