package main

import (
	"embed"

	"github.com/SUDOKU-ASCII/sudoku-desktop/internal/desktop"
)

//go:embed all:frontend/dist
var frontend embed.FS

//go:embed all:runtime/bin
var bundledRuntime embed.FS

//go:embed build/appicon.png
var trayIcon []byte

//go:embed build/tray/logo-monochrome.svg
var trayTemplateIcon []byte

func main() {
	desktop.Run(desktop.Resources{
		Frontend:         frontend,
		Runtime:          bundledRuntime,
		TrayIcon:         trayIcon,
		TrayTemplateIcon: trayTemplateIcon,
	})
}
