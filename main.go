package main

import (
	"embed"
	"log"

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
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "dev.pkvbeihilfe.tracker",
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	desktop.app = app

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
