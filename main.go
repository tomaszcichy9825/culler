package main

import (
	"embed"
	"log"

	"github.com/tomaszcichy9825/culler/internal/app"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// The built frontend is embedded in the binary, so the app ships as a single
// file with no runtime dependency.
//
//go:embed all:frontend/dist
var assets embed.FS

// main starts the culling app: it loads the shared state, registers the
// services the frontend binds to, and opens the window.
func main() {
	backend, err := app.New()
	if err != nil {
		log.Fatal(err)
	}
	defer backend.Close()

	library := app.NewLibraryService(backend)
	decisions := app.NewDecisionService(backend)
	apply := app.NewApplyService(backend)
	settings := app.NewConfigService(backend)
	previews := app.NewPreviewService(backend)

	wailsApp := application.New(application.Options{
		Name:        "culler",
		Description: "RAW/JPEG culling for photographers",
		Services: []application.Service{
			application.NewService(library),
			application.NewService(decisions),
			application.NewService(apply),
			application.NewService(settings),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
			// Preview bytes are served over HTTP rather than through a
			// binding so the webview can point an <img> straight at them.
			Middleware: previews.Middleware,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "culler",
		Width:  1280,
		Height: 800,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		// Transparent so the CSS theme paints the whole window — a fixed
		// colour here bleeds through in whichever theme it doesn't match.
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/",
	})

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}
