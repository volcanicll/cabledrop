// Package ui is the desktop shell: the Wails application, the panel window,
// the tray icon and its menu.
//
// It owns every Wails API call, and drives the app only through its exported
// methods — the Controller interface below documents exactly what it needs.
// All touchy Wails rules live here in one place:
//
//   - no Wails file dialogs (the picker subprocess in package picker is the
//     replacement; see picker.go for why)
//   - nothing on the request path may call SetSize or other InvokeSync-backed
//     APIs: a panic inside one wedges the main thread for good
//   - every tray/window touch happens on the main thread via InvokeAsync
package ui

import (
	"net/http"
	"os/exec"
	"runtime"

	"github.com/volcanicll/cabledrop/internal/app"
	"github.com/volcanicll/cabledrop/internal/icon"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Controller is what the shell needs from the app to do its job.
type Controller interface {
	Start()
	Stop()
	StartServing() error
	Refresh()
	ServeDir() string
	PushFiles(paths []string, destDir string)
	TogglePanel()
	PanelFocusLost()
	AttachPanel(panel app.PanelUI)
	AttachTray(tray app.TrayUI)
}

// Options configures Run.
type Options struct {
	Name        string
	Description string
	Version     string
	Handler     http.Handler
	Controller  Controller
	// ServeOnLaunch starts the phone listener as soon as the app is up
	// (--serve), instead of waiting for a device.
	ServeOnLaunch bool
}

// Run builds the shell, wires it to the controller, and blocks until the app
// quits.
func Run(opts Options) error {
	core := application.New(application.Options{
		Name:        opts.Name,
		Description: opts.Description,
		// The frontend is these embedded files: no build step, no bundled
		// browser. The system webview renders them.
		Assets: application.AssetOptions{
			Handler: opts.Handler,
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

	panel := core.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:          opts.Name,
		Width:         500,
		Height:        740,
		Frameless:     true,
		AlwaysOnTop:   true,
		Hidden:        true,
		DisableResize: true,
		// Both hides are native now: Escape and focus loss dismiss the panel
		// without a round trip through the page. The app still listens for
		// blur separately — it needs the timestamp to keep the tray-click
		// grace window honest.
		HideOnEscape:    true,
		HideOnFocusLost: true,
		// Dropping files onto the panel hands us real filesystem paths, which
		// a browser File object cannot provide.
		EnableFileDrop: true,
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
		Mac: application.MacWindow{
			// A dedicated NSPanel: showing or clicking it must not activate
			// the app or steal the current app's menu bar.
			WindowClass: application.MacWindowClassPanel,
			PanelPreferences: application.MacPanelPreferences{
				NonActivating: true,
				FloatingPanel: true,
			},
			// The panel appears wherever the user is, including over
			// fullscreen apps, and never joins the cmd-` window cycle.
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary |
				application.MacWindowCollectionBehaviorStationary |
				application.MacWindowCollectionBehaviorIgnoresCycle,
			// Frosted vibrancy behind a tint the page draws itself
			// (html.native-glass in panel.css). With the private-APIs build
			// tag the webview is transparent; without it everything stays
			// opaque and looks as before.
			Backdrop:     application.MacBackdropTranslucent,
			CornerType:   application.MacWindowCornerTypeRounded,
			CornerRadius: 14,
		},
	})

	// Dropping files on the panel sends them to the phone.
	panel.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		files := e.Context().DroppedFiles()
		if len(files) == 0 {
			return
		}
		opts.Controller.PushFiles(files, "")
	})

	// The panel is dismissed, never destroyed: building a webview per click
	// costs far more than hiding one.
	panel.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		panel.Hide()
		e.Cancel()
	})

	tray := core.SystemTray.New()
	tray.SetTemplateIcon(icon.TrayIcon(false))
	tray.SetTooltip(opts.Name + " · 未连接手机")
	tray.SetMenu(buildMenu(core, opts))
	// The window is attached for positioning under the icon, but the show/
	// hide toggle is handled by TogglePanel: Wails' own toggle cannot tell
	// "the click that opens" apart from "the blur this click just caused".
	tray.AttachWindow(panel).WindowOffset(4).WindowDebounce(120)
	tray.OnClick(opts.Controller.TogglePanel)

	// Clicking anywhere outside the panel dismisses it — how every menu-bar
	// panel behaves. AttachWindow's doc claims focus loss hides the window;
	// the implementation never does, so it is wired up from the per-platform
	// blur events here. Registering all three is safe: the other two never
	// fire on a given platform.
	dismissOnBlur := func(*application.WindowEvent) { opts.Controller.PanelFocusLost() }
	panel.OnWindowEvent(events.Mac.WindowDidResignKey, dismissOnBlur)
	panel.OnWindowEvent(events.Windows.WindowKillFocus, dismissOnBlur)
	panel.OnWindowEvent(events.Linux.WindowFocusOut, dismissOnBlur)

	// Hand the native surfaces to the app before it starts: the device watcher
	// touches the tray on its very first tick.
	opts.Controller.AttachPanel(&panelWindow{w: panel, tray: tray})
	opts.Controller.AttachTray(&trayIconItem{tray: tray, name: opts.Name})

	// Everything that touches the tray must wait for the event loop.
	//
	// Wails' main-thread dispatcher does not exist until the app is running,
	// so an InvokeAsync issued before that is a nil-pointer crash — and the
	// device watcher issues one on its very first tick. The framework's own
	// "started" event is the earliest moment it is safe to begin.
	core.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		opts.Controller.Start()
		if opts.ServeOnLaunch {
			if err := opts.Controller.StartServing(); err != nil {
				println("启动服务失败: " + err.Error())
			}
		}
	})

	defer opts.Controller.Stop()

	return core.Run()
}

// buildMenu is the right-click menu, the fallback for anything the panel
// can't reach.
func buildMenu(core *application.App, opts Options) *application.Menu {
	menu := application.Get().NewMenu()

	menu.Add(opts.Name + " " + opts.Version).SetEnabled(false)
	menu.AddSeparator()

	menu.Add("打开面板").OnClick(func(*application.Context) {
		opts.Controller.TogglePanel()
	})

	menu.Add("打开共享文件夹").OnClick(func(*application.Context) {
		openInFileManager(opts.Controller.ServeDir())
	})

	menu.Add("重新检测手机").OnClick(func(*application.Context) {
		opts.Controller.Refresh()
	})

	menu.AddSeparator()

	menu.Add("退出").OnClick(func(*application.Context) {
		application.Get().Quit()
	})

	return menu
}

// openInFileManager reveals a folder in Finder, Explorer or the desktop's
// file manager.
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

// panelWindow adapts the Wails window to app.PanelUI. Every call lands on the
// main thread: Wails window methods are not safe from arbitrary goroutines.
type panelWindow struct {
	w    *application.WebviewWindow
	tray *application.SystemTray
}

func (p *panelWindow) Visible() bool { return p.w.IsVisible() }

func (p *panelWindow) Show() {
	if p.tray != nil {
		// ShowWindow positions the panel under the icon before showing it,
		// which matters if the user has changed displays since last time.
		application.InvokeAsync(func() { p.tray.ShowWindow() })
		return
	}
	application.InvokeAsync(func() { p.w.Show().Focus() })
}

func (p *panelWindow) Hide() {
	application.InvokeAsync(func() { p.w.Hide() })
}

// trayIconItem adapts the Wails tray to app.TrayUI.
type trayIconItem struct {
	tray *application.SystemTray
	name string
}

func (t *trayIconItem) SetStatus(connected bool, tooltip string) {
	ic := icon.TrayIcon(connected)
	application.InvokeAsync(func() {
		t.tray.SetTemplateIcon(ic)
		t.tray.SetTooltip(tooltip)
	})
}
