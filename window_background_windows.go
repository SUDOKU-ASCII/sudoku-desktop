//go:build windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

func mainWindowBackground() (application.BackgroundType, application.RGBA) {
	// Transparent frameless WebView2 windows use WS_EX_TRANSPARENT. After an
	// interactive resize, newly exposed pixels can remain click-through.
	return application.BackgroundTypeSolid, application.NewRGB(244, 246, 247)
}
