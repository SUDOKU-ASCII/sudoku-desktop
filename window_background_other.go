//go:build !windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

func mainWindowBackground() (application.BackgroundType, application.RGBA) {
	return application.BackgroundTypeTransparent, application.NewRGBA(0, 0, 0, 0)
}
