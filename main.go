// CableDrop — move data between a computer and an Android phone over a USB
// cable, with no network involved.
//
// The desktop side is a tray panel; the same embedded pages and the same JSON
// API are also served to the phone through `adb reverse`, so nothing needs to
// be installed on the phone.
package main

import (
	"embed"
	"log"
	"os"
	"os/exec"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:assets
var assetsFS embed.FS

// version is stamped in at build time:
// -ldflags "-X main.version=$(git describe --tags --always)"
var version = "dev"

func main() {
	// Icon generation runs before any GUI setup, so it works headlessly in a
	// build script.
	if len(os.Args) > 1 && os.Args[1] == "--gen-icons" {
		dir := "build/icons"
		if len(os.Args) > 2 {
			dir = os.Args[2]
		}
		if err := generateIcons(dir); err != nil {
			log.Fatal(err)
		}
		return
	}

	// Launcher icons for the Android APK, at every density the platform wants.
	if len(os.Args) > 1 && os.Args[1] == "--gen-android-icons" {
		dir := "android/app/src/main/res"
		if len(os.Args) > 2 {
			dir = os.Args[2]
		}
		if err := generateAndroidIcons(dir); err != nil {
			log.Fatal(err)
		}
		return
	}

	app := NewApp()

	core := application.New(application.Options{
		Name:        "CableDrop",
		Description: "Move files and clipboard text between this computer and an Android phone over USB",
		// The frontend is these embedded files: no build step, no bundled
		// browser. The system webview renders them.
		Assets: application.AssetOptions{
			Handler: NewHandler(app, false),
		},
		Mac: application.MacOptions{
			// Tray only: no Dock icon.
			ActivationPolicy: application.ActivationPolicyAccessory,
		},
		Windows: application.WindowsOptions{
			// Hiding the panel must not end the process.
			DisableQuitOnLastWindowClosed: true,
		},
	})
	app.core = core

	panel := core.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:          "CableDrop",
		Width:         380,
		Height:        540,
		Frameless:     true,
		AlwaysOnTop:   true,
		Hidden:        true,
		DisableResize: true,
		// Dropping files onto the panel hands us real filesystem paths, which
		// a browser File object cannot provide.
		EnableFileDrop: true,
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	})

	// Dropping files on the panel sends them to the phone.
	panel.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		files := e.Context().DroppedFiles()
		if len(files) == 0 {
			return
		}
		app.PushFiles(files, "")
	})

	// The panel is dismissed, never destroyed: building a webview per click
	// costs far more than hiding one.
	panel.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		panel.Hide()
		e.Cancel()
	})

	tray := core.SystemTray.New()
	tray.SetTemplateIcon(trayIcon(false))
	tray.SetTooltip("CableDrop · 未连接手机")
	tray.SetMenu(buildMenu(app, tray, panel))
	// The window is attached for positioning under the icon, but the show/
	// hide toggle is handled by togglePanel: Wails' own toggle cannot tell
	// "the click that opens" apart from "the blur this click just caused".
	tray.AttachWindow(panel).WindowOffset(4).WindowDebounce(120)
	tray.OnClick(app.togglePanel)
	app.tray = tray
	app.panel = panel

	// Clicking anywhere outside the panel dismisses it — how every menu-bar
	// panel behaves. AttachWindow's doc claims focus loss hides the window;
	// the implementation never does, so it is wired up from the per-platform
	// blur events here. Registering all three is safe: the other two never
	// fire on a given platform.
	dismissOnBlur := func(*application.WindowEvent) { app.panelFocusLost() }
	panel.OnWindowEvent(events.Mac.WindowDidResignKey, dismissOnBlur)
	panel.OnWindowEvent(events.Windows.WindowKillFocus, dismissOnBlur)
	panel.OnWindowEvent(events.Linux.WindowFocusOut, dismissOnBlur)

	serveOnLaunch := len(os.Args) > 1 && os.Args[1] == "--serve"

	// Everything that touches the tray must wait for the event loop.
	//
	// Wails' main-thread dispatcher does not exist until the app is running,
	// so an InvokeAsync issued before that is a nil-pointer crash — and the
	// device watcher issues one on its very first tick. The framework's own
	// "started" event is the earliest moment it is safe to begin.
	core.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		app.Start()
		if serveOnLaunch {
			if err := app.StartServing(); err != nil {
				log.Printf("启动服务失败: %v", err)
			}
		}
	})

	defer app.Stop()

	if err := core.Run(); err != nil {
		log.Fatal(err)
	}
}

// buildMenu is the right-click menu, the fallback for anything the panel
// can't reach.
func buildMenu(app *App, tray *application.SystemTray, panel *application.WebviewWindow) *application.Menu {
	menu := application.Get().NewMenu()

	menu.Add("CableDrop " + version).SetEnabled(false)
	menu.AddSeparator()

	menu.Add("打开面板").OnClick(func(*application.Context) {
		tray.ShowWindow()
	})

	menu.Add("打开共享文件夹").OnClick(func(*application.Context) {
		openInFileManager(app.ServeDir())
	})

	menu.Add("重新检测手机").OnClick(func(*application.Context) {
		adb.Refresh()
		app.refresh()
	})

	menu.AddSeparator()

	menu.Add("退出").OnClick(func(*application.Context) {
		app.Stop()
		application.Get().Quit()
	})

	return menu
}

// openInFileManager reveals a folder in Finder, Explorer or the desktop's
// file manager, so the user doesn't have to remember where it is.
func openInFileManager(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("explorer", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	_ = cmd.Start()
}
