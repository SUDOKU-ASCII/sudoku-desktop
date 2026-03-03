package main

import (
	"embed"
	"runtime"
	"time"

	"github.com/SUDOKU-ASCII/sudoku-desktop/internal/core"
	"github.com/saba-futai/sudoku/pkg/logx"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed all:runtime/bin
var bundledRuntime embed.FS

//go:embed build/appicon.png
var trayIcon []byte

//go:embed build/tray/logo-monochrome.svg
var trayTemplateIcon []byte

func init() {
	application.RegisterEvent[core.RuntimeState](core.EventStateUpdated)
	application.RegisterEvent[core.LogEntry](core.EventLogAdded)
}

func main() {
	appService := NewApp(bundledRuntime, "runtime/bin")
	quitting := false

	app := application.New(application.Options{
		Name:        "sudoku4x4",
		Description: "4x4 sudoku desktop client",
		Services: []application.Service{
			application.NewService(appService),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		Linux: application.LinuxOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                   "main",
		Title:                  "4x4 sudoku",
		Width:                  1160,
		Height:                 760,
		MinWidth:               390,
		MinHeight:              680,
		URL:                    "/",
		BackgroundColour:       application.NewRGB(245, 239, 225),
		OpenInspectorOnStartup: true,
		KeyBindings: map[string]func(window application.Window){
			"cmd+w": func(window application.Window) {
				if runtime.GOOS == "darwin" {
					window.Hide()
				}
			},
			"ctrl+w": func(window application.Window) {
				if runtime.GOOS == "darwin" {
					window.Hide()
				}
			},
		},
	})

	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
			if quitting {
				return
			}
			event.Cancel()
			mainWindow.Hide()
		})
	}

	tray := app.SystemTray.New().
		AttachWindow(mainWindow).
		WindowDebounce(160 * time.Millisecond)
	tray.SetTooltip("4x4 sudoku")
	if runtime.GOOS == "darwin" {
		if len(trayTemplateIcon) > 0 {
			tray.SetTemplateIcon(trayTemplateIcon)
		} else if len(trayIcon) > 0 {
			tray.SetTemplateIcon(trayIcon)
		}
	} else if len(trayIcon) > 0 {
		tray.SetIcon(trayIcon)
	}
	tray.OnClick(func() {
		mainWindow.Show().Focus()
	})

	trayMenu := application.NewMenu()
	openItem := trayMenu.Add("Open Window")
	proxyToggleItem := trayMenu.Add("Start Proxy")
	trayMenu.AddSeparator()
	modeMenu := trayMenu.AddSubmenu("Proxy Mode")
	modeGlobalItem := modeMenu.AddRadio("Global", false)
	modeDirectItem := modeMenu.AddRadio("Direct", false)
	modePACItem := modeMenu.AddRadio("PAC", false)
	trayMenu.AddSeparator()
	quitItem := trayMenu.Add("Quit")

	refreshTrayMenu := func() {
		if appService.trayIsRunning() {
			proxyToggleItem.SetLabel("Stop Proxy")
		} else {
			proxyToggleItem.SetLabel("Start Proxy")
		}
		mode := appService.trayCurrentProxyMode()
		modeGlobalItem.SetChecked(mode == "global")
		modeDirectItem.SetChecked(mode == "direct")
		modePACItem.SetChecked(mode == "pac")
	}

	openItem.OnClick(func(_ *application.Context) {
		mainWindow.Show().Focus()
	})
	proxyToggleItem.OnClick(func(_ *application.Context) {
		if err := appService.trayToggleProxy(); err != nil {
			logx.Errorf("Desktop", "tray toggle proxy failed: %v", err)
		}
		refreshTrayMenu()
	})
	modeGlobalItem.OnClick(func(_ *application.Context) {
		if err := appService.traySetProxyMode("global"); err != nil {
			logx.Errorf("Desktop", "tray set mode global failed: %v", err)
		}
		refreshTrayMenu()
	})
	modeDirectItem.OnClick(func(_ *application.Context) {
		if err := appService.traySetProxyMode("direct"); err != nil {
			logx.Errorf("Desktop", "tray set mode direct failed: %v", err)
		}
		refreshTrayMenu()
	})
	modePACItem.OnClick(func(_ *application.Context) {
		if err := appService.traySetProxyMode("pac"); err != nil {
			logx.Errorf("Desktop", "tray set mode pac failed: %v", err)
		}
		refreshTrayMenu()
	})
	quitItem.OnClick(func(_ *application.Context) {
		quitting = true
		app.Quit()
	})

	tray.SetMenu(trayMenu)
	tray.OnRightClick(func() {
		refreshTrayMenu()
		tray.OpenMenu()
	})
	refreshTrayMenu()

	err := app.Run()
	if err != nil {
		logx.Errorf("Desktop", "wails run failed: %v", err)
	}
}
