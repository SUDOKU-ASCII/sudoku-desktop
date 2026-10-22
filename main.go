package main

import (
	"embed"
	"errors"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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
	application.RegisterEvent[core.TrafficTick](core.EventTrafficUpdated)
}

func main() {
	// Helper roles are second instances of this executable and must run before any GUI
	// initialisation (and before the single-instance lock).
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--guard":
			if len(os.Args) > 2 {
				core.GuardMain(os.Args[2])
			}
			return
		case "--tun-helper":
			os.Exit(core.RunTunHelper(os.Args[2:], bundledRuntime, "runtime/bin"))
		}
	}

	startHidden := false
	for _, arg := range os.Args[1:] {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "--autostart", "--background", "--hidden":
			startHidden = true
		}
	}

	appService := NewApp(bundledRuntime, "runtime/bin")
	var quitting atomic.Bool
	var shutdownDone atomic.Bool
	var shutdownOnce sync.Once
	var app *application.App
	var mainWindow application.Window
	var requestQuit, shutdown func()

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
			WndProcInterceptor:            windowsWndProcInterceptor,
		},
		Linux: application.LinuxOptions{
			DisableQuitOnLastWindowClosed: true,
			ProgramName:                   "sudoku4x4",
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		// ShouldQuit runs on the UI thread. On Windows tearing down the core, TUN and system
		// proxy there froze the message loop (window and tray stopped responding), so the first
		// request starts the shutdown in the background and re-issues the quit once it is done.
		// macOS keeps the synchronous path: cancelling applicationShouldTerminate would also
		// cancel a logout or shutdown.
		ShouldQuit: func() bool {
			if shutdownDone.Load() {
				return true
			}
			if runtime.GOOS == "darwin" {
				quitting.Store(true)
				shutdown()
				return true
			}
			requestQuit()
			return false
		},
	})

	shutdown = func() {
		shutdownOnce.Do(func() {
			appService.ShutdownNow()
			shutdownDone.Store(true)
		})
	}
	requestQuit = func() {
		if !quitting.CompareAndSwap(false, true) {
			return
		}
		go func() {
			// Backend teardown is bounded (it kills the core after its own deadline); this
			// only guards against a wedged UI thread keeping a dead process alive.
			time.AfterFunc(45*time.Second, func() { os.Exit(0) })
			if mainWindow != nil {
				mainWindow.Hide()
			}
			shutdown()
			app.Quit()
		}()
	}
	onSessionEnd(func() {
		quitting.Store(true)
		shutdown()
	})

	backgroundType, backgroundColour := mainWindowBackground()
	mainWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                   "main",
		Title:                  "4x4 sudoku",
		Width:                  523,
		Height:                 652,
		MinWidth:               390,
		MinHeight:              560,
		URL:                    "/",
		Frameless:              true,
		BackgroundType:         backgroundType,
		BackgroundColour:       backgroundColour,
		Hidden:                 startHidden,
		OpenInspectorOnStartup: false,
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropTransparent,
		},
		Windows: application.WindowsWindow{
			DisableFramelessWindowDecorations: false,
			// Visual hosting lets the host window hit-test its own resize borders natively, so
			// edge resizing no longer depends on page JS that a scrollbar can swallow.
			WebView2CompositionHosting: true,
		},
		Linux: application.LinuxWindow{
			Icon:                trayIcon,
			WindowIsTranslucent: true,
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

	mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if quitting.Load() {
			return
		}
		event.Cancel()
		mainWindow.Hide()
	})
	configureApplicationMenu(app, mainWindow)

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
	showMainWindow := func() {
		if mainWindow.IsMinimised() {
			mainWindow.UnMinimise()
		}
		mainWindow.Show().Focus()
	}
	tray.OnClick(showMainWindow)
	tray.OnDoubleClick(showMainWindow)

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
		showMainWindow()
	})
	proxyToggleItem.OnClick(func(_ *application.Context) {
		if err := appService.trayToggleProxy(); err != nil && !errors.Is(err, core.ErrStartCancelled) {
			logx.Errorf("Desktop", "tray toggle proxy failed: %v", err)
			mainWindow.Show().Focus()
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

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		requestQuit()
	}()

	err := app.Run()
	if err != nil {
		logx.Errorf("Desktop", "wails run failed: %v", err)
	}
}
