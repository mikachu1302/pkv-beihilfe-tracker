package main

import (
	"embed"
	"log"
	"net/http"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/service"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	svc := service.New()
	desktop := &DesktopService{}

	app := application.New(application.Options{
		Name:        "PKV Beihilfe Tracker",
		Description: "Arztrechnungen, Beihilfe und PKV im Blick",
		Services: []application.Service{
			application.NewService(svc),
			application.NewService(desktop),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
			// Inbox files and stored documents for previews and text recognition.
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !svc.ServeFile(w, r) {
						next.ServeHTTP(w, r)
					}
				})
			},
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "dev.pkvbeihilfe.tracker",
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		// Only for the headless server mode (-tags server, used for testing): its
		// default of 30 s per call would cut off reading a document with the AI,
		// which takes about a minute per page. The desktop app has no such limit.
		Server: application.ServerOptions{WriteTimeout: 30 * time.Minute},
	})
	desktop.app = app
	svc.SetEmitter(app.Event) // "data-changed" when another device changed the data folder

	desktop.window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "PKV Beihilfe Tracker",
		Width:            1280,
		Height:           820,
		MinWidth:         960,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(247, 248, 250),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
