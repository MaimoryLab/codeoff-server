package main

import (
	"context"
	"embed"
	"log"
	"time"

	"github.com/MaimoryLab/codex-server/internal/control"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	service, err := NewAppService()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = service.Shutdown() }()
	controlServer := control.New(func(context.Context) Overview {
		return service.Overview()
	}, service.devices)
	if err := controlServer.Start(); err != nil {
		log.Fatal(err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = controlServer.Close(shutdownCtx)
	}()
	log.Printf("local control API: %s", controlServer.Addr())

	app := application.New(application.Options{
		Name:        "Codex Remote",
		Description: "Local Codex remote control",
		Services:    []application.Service{application.NewService(service)},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Codex Remote",
		Width:            1000,
		Height:           700,
		BackgroundColour: application.NewRGB(16, 18, 24),
		URL:              "/",
	})
	window.OnWindowEvent(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		window.Hide()
	})

	menu := app.NewMenu()
	menu.Add("Open Control Panel").OnClick(func(*application.Context) { window.Show().Focus() })
	menu.Add("Refresh Environment").OnClick(func(*application.Context) {
		service.RefreshStatus()
		app.Event.Emit("status:changed", service.Status())
	})
	menu.Add("Update Codex").OnClick(func(*application.Context) {
		go func() {
			if _, err := service.InstallCodex(); err != nil {
				log.Printf("update Codex: %v", err)
			}
		}()
	})
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(*application.Context) { app.Quit() })

	tray := app.SystemTray.New().
		SetIcon(appIcon).
		AttachWindow(window).
		SetMenu(menu)
	tray.SetTooltip("Codex Remote")
	tray.Run()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
