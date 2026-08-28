package main

import (
	"context"
	"embed"
	"log"
	"runtime"
	"slices"
	"time"

	"github.com/MaimoryLab/codeoff-server/internal/buildinfo"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

//go:embed build/darwin/tray-icon.png
var macTrayIcon []byte

func main() {
	service, err := NewAppService()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = service.Shutdown() }()

	var window *application.WebviewWindow
	windowReady := make(chan struct{})

	app := application.New(application.Options{
		Name:        "Codeoff Server",
		Description: "Local Codex remote control",
		Services:    []application.Service{application.NewService(service)},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.maimorylab.codeoff.server",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				<-windowReady
				window.Show().Focus()
			},
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		Linux: application.LinuxOptions{
			DisableQuitOnLastWindowClosed: true,
		},
	})
	githubProvider, err := github.New(github.Config{
		Repository:    "MaimoryLab/codeoff-server",
		ChecksumAsset: "SHA256SUMS",
		AssetMatcher:  otaAssetMatcher,
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := app.Updater.Init(updater.Config{
		CurrentVersion: buildinfo.Version,
		Providers:      []updater.Provider{githubProvider},
	}); err != nil {
		log.Fatal(err)
	}
	showUpdateCheck := func(ctx context.Context) {
		if err := app.Updater.CheckAndInstall(ctx); err != nil {
			log.Printf("check for updates: %v", err)
		}
	}

	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Codeoff Server",
		Width:            900,
		Height:           600,
		AlwaysOnTop:      false,
		BackgroundColour: application.NewRGB(16, 18, 24),
		URL:              "/",
	})
	closeWindowHook := window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		window.Hide()
	})
	defer closeWindowHook()
	close(windowReady)

	if err := service.startControlServer(); err != nil {
		log.Fatal(err)
	}
	log.Printf("control API listening on %s", service.Overview().ControlAddr)
	if err := service.restore(); err != nil {
		log.Printf("restore services: %v", err)
	}

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
	preventSleep := menu.AddCheckbox("防止系统休眠", service.preventSleepEnabled()).OnClick(func(ctx *application.Context) {
		if err := service.setPreventSleep(ctx.ClickedMenuItem().Checked()); err != nil {
			log.Printf("set prevent sleep: %v", err)
		}
		updateTrayMenu()
	})
	menu.AddSeparator()
	menu.Add("检查更新").OnClick(func(*application.Context) {
		go showUpdateCheck(context.Background())
	})
	menu.Add("打开控制面板").OnClick(func(*application.Context) { window.Show().Focus() })
	menu.Add("退出").OnClick(func(*application.Context) { app.Quit() })
	menu.AddSeparator()

	updateTrayMenu = func() {
		overview := service.Overview()
		appServerStatus.SetLabel("App-server：" + serviceStatus(overview.Environment.AppServer.Installed, overview.AppServer.Running, overview.AppServer.Starting, overview.AppServer.Stopping))
		appServerAddress.SetLabel(overview.ControlAddr).SetHidden(!overview.AppServer.Running)
		appServerToggle.SetLabel(toggleLabel(overview.AppServer.Running || overview.AppServer.Starting)).SetEnabled(overview.Environment.AppServer.Installed && !overview.AppServer.Starting && !overview.AppServer.Stopping)
		tunnelStatus.SetLabel("Tunnel：" + serviceStatus(overview.Environment.Cloudflared.Installed || overview.Tunnel.External, overview.Tunnel.Running, overview.Tunnel.Starting, overview.Tunnel.Stopping))
		tunnelAddress.SetLabel(overview.Tunnel.URL).SetHidden(!overview.Tunnel.Running)
		tunnelToggle.SetLabel(toggleLabel(overview.Tunnel.Running || overview.Tunnel.Starting)).SetHidden(overview.Tunnel.External).SetEnabled(overview.Environment.Cloudflared.Installed && !overview.Tunnel.External && !overview.Tunnel.Starting && !overview.Tunnel.Stopping)
		preventSleep.SetChecked(service.preventSleepEnabled())
	}
	updateTrayMenu()

	tray := app.SystemTray.New().SetMenu(menu)
	tray.OnClick(func() {
		if window.IsVisible() {
			window.Hide()
			return
		}
		window.Show().Focus()
	})
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(macTrayIcon)
	} else {
		tray.SetIcon(appIcon)
	}
	tray.OnRightClick(func() {
		updateTrayMenu()
		tray.ShowMenu()
	})
	tray.SetTooltip("Codeoff Server")
	tray.Run()
	if buildinfo.Version != "dev" {
		go runUpdateChecks(app.Context(), 24*time.Hour, func(ctx context.Context) {
			release, err := app.Updater.Check(ctx)
			if err != nil {
				log.Printf("background update check: %v", err)
			} else if release != nil {
				showUpdateCheck(ctx)
			}
		})
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func runUpdateChecks(ctx context.Context, interval time.Duration, check func(context.Context)) {
	check(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			check(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func otaAssetMatcher(req updater.CheckRequest, assets []github.ReleaseAsset) int {
	want := "ota-codeoff-server-" + req.Platform + "-" + req.Arch + ".zip"
	return slices.IndexFunc(assets, func(asset github.ReleaseAsset) bool { return asset.Name == want })
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
