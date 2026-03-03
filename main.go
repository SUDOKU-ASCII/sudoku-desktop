package main

import (
	"embed"
	"runtime"

	"github.com/saba-futai/sudoku/pkg/logx"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed all:runtime/bin
var bundledRuntime embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp(bundledRuntime, "runtime/bin")

	// Create application with options
	err := wails.Run(&options.App{
		Title:             "4x4 sudoku",
		Width:             1160,
		Height:            760,
		MinWidth:          390,
		MinHeight:         680,
		HideWindowOnClose: runtime.GOOS == "darwin",
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 245, G: 239, B: 225, A: 1},
		Debug:            options.Debug{OpenInspectorOnStartup: true},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		logx.Errorf("Desktop", "wails run failed: %v", err)
	}
}
