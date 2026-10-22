//go:build !windows

package desktop

var windowsWndProcInterceptor func(hwnd uintptr, msg uint32, wParam, lParam uintptr) (uintptr, bool)

func onSessionEnd(func()) {}
