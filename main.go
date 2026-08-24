package main

import (
	"embed"
	"log"

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
	if err := service.startControlServer(); err != nil {
		log.Fatal(err)
	}
	log.Printf("control API listening on %s", service.Overview().ControlAddr)

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
		Width:            900,
		Height:           600,
		BackgroundColour: application.NewRGB(16, 18, 24),
		URL:              "/",
	})
	window.OnWindowEvent(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		window.Hide()
	})

	menu := app.NewMenu()
	var updateTrayMenu func()
	appServerStatus := menu.Add("").SetEnabled(false)
	appServerAddress := menu.Add("").OnClick(func(*application.Context) {
		overview := service.Overview()
		if overview.AppServer.Running {
			app.Clipboard.SetText(overview.ControlAddr)
		}
	})
	appServerToggle := menu.Add("").OnClick(func(*application.Context) {
		if _, err := service.ToggleAppServer(); err != nil {
			log.Printf("toggle app-server: %v", err)
		}
		updateTrayMenu()
	})
	menu.AddSeparator()
	tunnelStatus := menu.Add("").SetEnabled(false)
	tunnelAddress := menu.Add("").OnClick(func(*application.Context) {
		overview := service.Overview()
		if overview.Tunnel.Running {
			app.Clipboard.SetText(overview.Tunnel.URL)
		}
	})
	tunnelToggle := menu.Add("").OnClick(func(*application.Context) {
		if _, err := service.ToggleTunnel(); err != nil {
			log.Printf("toggle tunnel: %v", err)
		}
		updateTrayMenu()
	})
	menu.AddSeparator()
	menu.Add("打开控制面板").OnClick(func(*application.Context) { window.Show().Focus() })
	menu.Add("退出").OnClick(func(*application.Context) { app.Quit() })
	menu.AddSeparator()

	updateTrayMenu = func() {
		overview := service.Overview()
		appServerStatus.SetLabel("App-server：" + serviceStatus(overview.Environment.AppServer.Installed, overview.AppServer.Running, overview.AppServer.Starting, overview.AppServer.Stopping))
		appServerAddress.SetLabel(overview.ControlAddr).SetHidden(!overview.AppServer.Running)
		appServerToggle.SetLabel(toggleLabel(overview.AppServer.Running || overview.AppServer.Starting)).SetEnabled(overview.Environment.AppServer.Installed && !overview.AppServer.Starting && !overview.AppServer.Stopping)
		tunnelStatus.SetLabel("CF Tunnel：" + serviceStatus(overview.Environment.Cloudflared.Installed, overview.Tunnel.Running, overview.Tunnel.Starting, overview.Tunnel.Stopping))
		tunnelAddress.SetLabel(overview.Tunnel.URL).SetHidden(!overview.Tunnel.Running)
		tunnelToggle.SetLabel(toggleLabel(overview.Tunnel.Running || overview.Tunnel.Starting)).SetEnabled(overview.Environment.Cloudflared.Installed && !overview.Tunnel.Starting && !overview.Tunnel.Stopping)
	}
	updateTrayMenu()

	tray := app.SystemTray.New().
		SetIcon(appIcon).
		AttachWindow(window).
		SetMenu(menu)
	tray.OnRightClick(func() {
		updateTrayMenu()
		tray.ShowMenu()
	})
	tray.SetTooltip("Codex Remote")
	tray.Run()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func serviceStatus(installed, running, starting, stopping bool) string {
	if !installed {
		return "未安装"
	}
	if starting {
		return "启动中"
	}
	if stopping {
		return "停止中"
	}
	if running {
		return "运行"
	}
	return "停止"
}

func toggleLabel(running bool) string {
	if running {
		return "停止"
	}
	return "启动"
}
