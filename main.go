package main

import (
	"embed"
	"errors"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/SUDOKU-ASCII/sudoku-desktop/internal/core"
	"github.com/SUDOKU-ASCII/sudoku/pkg/logx"
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
	startHidden := false
	for _, arg := range os.Args[1:] {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "--autostart", "--background", "--hidden":
			startHidden = true
		}
	}

	appService := NewApp(bundledRuntime, "runtime/bin")
	var quitting atomic.Bool
	var app *application.App
	var mainWindow application.Window

	showExistingInstance := func() {
		application.InvokeAsync(func() {
			if app != nil {
				app.Show()
			}
			if mainWindow != nil {
				if mainWindow.IsMinimised() {
					mainWindow.UnMinimise()
				}
				mainWindow.Restore()
				mainWindow.Show().Focus()
			}
			if app != nil {
				dialog := app.Dialog.Info().
					SetTitle("Already Running").
					SetMessage("4x4 sudoku is already running. Switched to the existing window.")
				if mainWindow != nil {
					dialog.AttachToWindow(mainWindow)
				}
				dialog.Show()
			}
		})
	}

	app = application.New(application.Options{
		Name:        "sudoku4x4",
		Description: "4x4 sudoku desktop client",
		Icon:        trayIcon,
		Services: []application.Service{
			application.NewService(appService),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.sudokuascii.sudoku4x4",
			ExitCode: 0,
			OnSecondInstanceLaunch: func(_ application.SecondInstanceData) {
				showExistingInstance()
			},
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		Linux: application.LinuxOptions{
			DisableQuitOnLastWindowClosed: true,
			ProgramName:                   "sudoku4x4",
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		ShouldQuit: func() bool {
			quitting.Store(true)
			appService.ShutdownNow()
			return true
		},
	})

	requestQuit := func() {
		if !quitting.CompareAndSwap(false, true) {
			return
		}
		appService.ShutdownNow()
		app.Quit()
	}

	mainWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                   "main",
		Title:                  "4x4 sudoku",
		Width:                  1160,
		Height:                 760,
		MinWidth:               390,
		MinHeight:              680,
		URL:                    "/",
		BackgroundColour:       application.NewRGB(245, 239, 225),
		Hidden:                 startHidden,
		OpenInspectorOnStartup: !startHidden,
		Linux: application.LinuxWindow{
			Icon: trayIcon,
		},
		KeyBindings: map[string]func(window application.Window){
			"cmd+q": func(window application.Window) {
				requestQuit()
			},
			"ctrl+q": func(window application.Window) {
				requestQuit()
			},
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
			if quitting.Load() {
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
			if errors.Is(err, core.ErrAdminRequired) {
				mainWindow.Show().Focus()
			}
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
		requestQuit()
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
