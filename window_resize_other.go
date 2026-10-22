//go:build !(linux && cgo && !gtk4 && !android && !server)

package main

import "github.com/wailsapp/wails/v3/pkg/application"

func registerWindowResize(*application.App, application.Window) {}
