//go:build linux && cgo && !gtk4 && !android && !server

package main

/*
#cgo linux pkg-config: gtk+-3.0
#include <gtk/gtk.h>

static void beginResize(void *win, int edge) {
	GdkSeat *seat = gdk_display_get_default_seat(gdk_display_get_default());
	GdkDevice *pointer = gdk_seat_get_pointer(seat);
	gint x = 0, y = 0;
	gdk_device_get_position(pointer, NULL, &x, &y);
	gtk_window_begin_resize_drag(GTK_WINDOW(win), (GdkWindowEdge)edge, 1, x, y, GDK_CURRENT_TIME);
}
*/
import "C"

import (
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var gdkEdges = map[string]C.int{
	"nw": C.GDK_WINDOW_EDGE_NORTH_WEST,
	"n":  C.GDK_WINDOW_EDGE_NORTH,
	"ne": C.GDK_WINDOW_EDGE_NORTH_EAST,
	"w":  C.GDK_WINDOW_EDGE_WEST,
	"e":  C.GDK_WINDOW_EDGE_EAST,
	"sw": C.GDK_WINDOW_EDGE_SOUTH_WEST,
	"s":  C.GDK_WINDOW_EDGE_SOUTH,
	"se": C.GDK_WINDOW_EDGE_SOUTH_EAST,
}

// Wails' Linux backend leaves startResize empty and its JS resize hit-testing is Windows-only,
// so a frameless window can't be resized. The page draws invisible edge grips and reports
// the pressed edge here; the window manager then runs the resize.
func registerWindowResize(app *application.App, window application.Window) {
	app.Event.On("app:resize-edge", func(event *application.CustomEvent) {
		edge, ok := gdkEdges[fmt.Sprint(event.Data)]
		if !ok {
			return
		}
		application.InvokeAsync(func() {
			if native := window.NativeWindow(); native != nil {
				C.beginResize(native, edge)
			}
		})
	})
}
