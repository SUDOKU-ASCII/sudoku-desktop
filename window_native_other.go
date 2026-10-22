//go:build !windows

package main

var windowsWndProcInterceptor func(hwnd uintptr, msg uint32, wParam, lParam uintptr) (uintptr, bool)

func onSessionEnd(func()) {}
