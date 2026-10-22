//go:build !windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

var windowsWndProcInterceptor func(hwnd uintptr, msg uint32, wParam, lParam uintptr) (uintptr, bool)

func enforceNativeMinSize(application.Window, int, int) {}

func onSessionEnd(func()) {}
