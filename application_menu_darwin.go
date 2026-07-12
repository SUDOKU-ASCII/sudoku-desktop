//go:build darwin

package main

import "github.com/wailsapp/wails/v3/pkg/application"

func configureApplicationMenu(app *application.App, mainWindow application.Window) {
	menu := app.NewMenu()
	menu.AddRole(application.AppMenu)

	fileMenu := menu.AddSubmenu("File")
	fileMenu.Add("Close").
		SetAccelerator("CmdOrCtrl+w").
		OnClick(func(_ *application.Context) {
			mainWindow.Hide()
		})

	menu.AddRole(application.EditMenu)
	menu.AddRole(application.ViewMenu)
	menu.AddRole(application.WindowMenu)
	menu.AddRole(application.HelpMenu)
	app.Menu.Set(menu)
}
