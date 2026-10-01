package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Transfer is one file movement, shown in the panel's history.
type Transfer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`  // push | pull | upload
	State  string `json:"state"` // running | done | failed
	Detail string `json:"detail"`
	At     int64  `json:"at"`
}

// State is everything the UI needs in one payload.
type State struct {
	Platform    string     `json:"platform"`
	ADBFound    bool       `json:"adbFound"`
	ADBPath     string     `json:"adbPath"`
	Connected   bool       `json:"connected"`
	Device      string     `json:"device"`
	Serial      string     `json:"serial"`
	StorageFree string     `json:"storageFree"`
	ServeDir    string     `json:"serveDir"`
	Serving     bool       `json:"serving"`
	Port        int        `json:"port"`
	PhoneURL    string     `json:"phoneURL"`
	Transfers   []Transfer `json:"transfers"`
	Error       string     `json:"error,omitempty"`
	Native      bool       `json:"native"`
}

// App holds all the state the panel and the phone both read.
type App struct {
	mu sync.Mutex

	clip     *Clipboard
	serveDir string

	listener net.Listener
	port     int

	transfers []Transfer
	seq       int

	dev       Device
	connected bool
	free      string

	// Last values pushed to the status item. Re-setting the icon every poll
	// churns AppKit for no reason, so the tray is only touched on a change.
	traySet  bool
	trayConn bool
	trayName string

	// everRefreshed guards the connect/disconnect transition so the first
	// poll, which has nothing to compare against, can't be mistaken for one.
	everRefreshed bool

	// note is text pushed from the desktop to be picked up on the phone. It is
	// separate from the clipboard mirror so sending something doesn't clobber
	// whatever the user currently has copied.
	note   string
	noteAt int64

	lastErr string
	native  bool

	tray  *application.SystemTray
	panel *application.WebviewWindow
	core  *application.App

	// hiddenAt is when the panel last dismissed itself, in unix millis. The
	// tray click handler needs it to tell "user clicked to close" apart from
	// "the click itself stole focus and the blur handler already closed it".
	hiddenAt atomic.Int64

	stopWatch chan struct{}
}

const (
	defaultPort  = 8765
	maxTransfers = 40
)

func NewApp() *App {
	return &App{
		clip:      NewClipboard(),
		serveDir:  defaultServeDir(),
		transfers: make([]Transfer, 0, 8),
		native:    true,
	}
}

// defaultServeDir is a dedicated folder rather than Downloads: the point of
// this app is a predictable place things land, not more clutter in Downloads.
func defaultServeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	dir := filepath.Join(home, "USBBridge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return home
	}
	return dir
}

// Start begins watching for the phone and for clipboard changes.
func (a *App) Start() {
	a.clip.Start()
	a.stopWatch = make(chan struct{})
	go a.watch()
}

func (a *App) Stop() {
	if a.stopWatch != nil {
		close(a.stopWatch)
	}
	a.StopServing()
}

// watch polls for the device. Two seconds keeps a plug-in feeling immediate
// without spawning adb constantly.
func (a *App) watch() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	a.refresh()
	for {
		select {
		case <-a.stopWatch:
			return
		case <-t.C:
			a.refresh()
		}
	}
}

func (a *App) refresh() {
	if adb.Path() == "" {
		adb.Refresh()
	}

	a.mu.Lock()
	// The very first pass has no previous state to compare against, and it can
	// run concurrently with the server starting up. Treating it as a
	// transition would tear down a listener that was just created.
	first := !a.everRefreshed
	a.everRefreshed = true
	prevConnected := a.connected
	a.mu.Unlock()

	dev, err := adb.FirstDevice()
	a.mu.Lock()
	switch {
	case err != nil:
		a.connected = false
		a.free = ""
		a.dev = Device{}
		if len(a.transfers) == 0 {
			a.lastErr = ""
		}
	default:
		a.connected = true
		a.dev = dev
		a.mu.Unlock()
		free := FreeSpaceOnDevice()
		a.mu.Lock()
		a.free = free
		a.lastErr = ""
	}
	a.mu.Unlock()

	// A device that comes back needs its tunnel re-established: adb reverse
	// dies with the connection, and the phone's bookmarked URL would silently
	// stop resolving. This re-issues the reverse rather than restarting the
	// listener — the port must not change under the phone's open page.
	if a.isConnected() && !a.Serving() {
		// Bring the server up as soon as a phone is present, so plugging the
		// cable in is the whole setup — there is nothing to click before the
		// page on the phone works.
		if err := a.StartServing(); err != nil {
			log.Printf("自动启动服务失败: %v", err)
		}
	} else if !first && a.isConnected() && !prevConnected {
		a.rebuildTunnel()
	}

	a.syncTray()
}

// rebuildTunnel re-points the device's loopback at the running listener.
func (a *App) rebuildTunnel() {
	a.mu.Lock()
	port := a.port
	running := a.listener != nil
	a.mu.Unlock()

	if !running || port == 0 {
		return
	}
	remote := fmt.Sprintf("tcp:%d", port)
	adb.Run(15*time.Second, "reverse", remote, remote)
}

func (a *App) isConnected() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.connected
}

func (a *App) Serving() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.listener != nil
}

// syncTray updates the menu bar icon and tooltip. It is a no-op unless
// something it shows actually changed: the device watcher calls it every two
// seconds, and re-setting an NSStatusItem image each time makes AppKit reload
// its preferences and redraw for nothing.
func (a *App) syncTray() {
	if a.tray == nil {
		return
	}
	connected := a.isConnected()

	a.mu.Lock()
	name := a.dev.Name()
	changed := !a.traySet || a.trayConn != connected || a.trayName != name
	a.traySet, a.trayConn, a.trayName = true, connected, name
	a.mu.Unlock()

	if !changed {
		return
	}

	tooltip := "USBBridge · 未连接手机"
	if connected {
		tooltip = "USBBridge · " + name
	}
	icon := trayIcon(connected)

	application.InvokeAsync(func() {
		a.tray.SetTemplateIcon(icon)
		a.tray.SetTooltip(tooltip)
	})
}

// --- panel ----------------------------------------------------------------

// panelHideGrace is how long, after the panel dismissed itself, a tray click
// still counts as "close it" rather than "open it".
const panelHideGrace = 350 * time.Millisecond

// togglePanel is the tray icon click: show the panel when hidden, hide it
// when shown.
//
// The grace window exists because the very click that lands on the tray icon
// can take key focus away from the panel first (on macOS the status item
// window becomes key before the action fires). The blur handler then dismisses
// the panel a beat before this handler reads visibility, and a naive toggle
// would immediately re-open what the user just asked to close.
func (a *App) togglePanel() {
	if a.panelVisible() {
		a.hidePanel()
		return
	}
	if time.Since(time.UnixMilli(a.hiddenAt.Load())) < panelHideGrace {
		return
	}
	a.showPanel()
}

// panelFocusLost dismisses the panel whenever it stops being the key window —
// a click on the desktop, another app, anything. This is what makes the panel
// behave like a menu instead of a stubborn always-on-top overlay.
func (a *App) panelFocusLost() {
	if !a.panelVisible() {
		return
	}
	a.hidePanel()
}

func (a *App) panelVisible() bool {
	if a.panel == nil {
		return false
	}
	return a.panel.IsVisible()
}

func (a *App) showPanel() {
	if a.panel == nil {
		return
	}
	if a.tray != nil {
		// ShowWindow positions the panel under the icon before showing it,
		// which matters if the user has changed displays since last time.
		a.tray.ShowWindow()
		return
	}
	application.InvokeAsync(func() { a.panel.Show().Focus() })
}

func (a *App) hidePanel() {
	a.hiddenAt.Store(time.Now().UnixMilli())
	if a.panel == nil {
		return
	}
	application.InvokeAsync(func() { a.panel.Hide() })
}

// beginModal tucks the panel away for the duration of a native dialog.
//
// The panel is always-on-top; a system file dialog is not, so without this it
// opens behind the panel and looks like a dead click. Returns whether the
// panel was visible, for endModal to restore things exactly as they were.
func (a *App) beginModal() bool {
	wasVisible := a.panelVisible()
	if wasVisible {
		a.hidePanel()
	}
	return wasVisible
}

func (a *App) endModal(wasVisible bool) {
	if wasVisible {
		a.showPanel()
	}
}

// State snapshots everything for the UI.
func (a *App) State() State {
	a.mu.Lock()
	defer a.mu.Unlock()

	tr := make([]Transfer, len(a.transfers))
	copy(tr, a.transfers)

	return State{
		Platform:    goos(),
		ADBFound:    adb.Path() != "",
		ADBPath:     adb.Path(),
		Connected:   a.connected,
		Device:      a.dev.Name(),
		Serial:      a.dev.Serial,
		StorageFree: a.free,
		ServeDir:    a.serveDir,
		Serving:     a.listener != nil,
		Port:        a.port,
		PhoneURL:    fmt.Sprintf("http://localhost:%d", a.portOf()),
		Transfers:   tr,
		Error:       a.lastErr,
		Native:      a.native,
	}
}

func (a *App) portOf() int {
	if a.port == 0 {
		return defaultPort
	}
	return a.port
}

// phoneURL is what the user opens on the device.
func (a *App) phoneURL() string {
	return fmt.Sprintf("http://localhost:%d", a.portOf())
}

// --- serving -------------------------------------------------------------

// StartServing listens on loopback only and points the device at it.
//
// Loopback is deliberate: the listener is reached from the phone through
// `adb reverse`, so nothing is exposed on the local network and no firewall
// prompt appears.
func (a *App) StartServing() error {
	a.mu.Lock()
	if a.listener != nil {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", defaultPort))
	if err != nil {
		// Port taken: fall back to whatever the OS gives us.
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			a.setErr(fmt.Sprintf("无法启动本地服务: %v", err))
			return err
		}
	}
	port := ln.Addr().(*net.TCPAddr).Port

	a.mu.Lock()
	a.listener = ln
	a.port = port
	dir := a.serveDir
	a.mu.Unlock()

	srv := newHTTPServer(a, dir, true)
	go func() {
		if err := srv.Serve(ln); err != nil {
			log.Printf("本地服务结束: %v", err)
		}
	}()

	// Point the device's own loopback at us. A failure here is reported but
	// doesn't tear the listener down: the panel can still be reached from this
	// machine, which is what makes the app diagnosable when adb misbehaves.
	remote := fmt.Sprintf("tcp:%d", port)
	if _, errOut, code, err := adb.Run(15*time.Second, "reverse", remote, remote); err != nil || code != 0 {
		msg := "服务已启动，但手机隧道建立失败"
		if errOut != "" {
			msg += ": " + strings.TrimSpace(errOut)
		} else if err != nil {
			msg += ": " + err.Error()
		}
		a.setErr(msg)
	} else {
		a.setErr("")
	}
	return nil
}

func (a *App) StopServing() {
	a.mu.Lock()
	ln := a.listener
	port := a.port
	a.listener = nil
	a.mu.Unlock()

	if ln != nil {
		ln.Close()
	}
	if port != 0 {
		adb.Run(10*time.Second, "reverse", "--remove", fmt.Sprintf("tcp:%d", port))
	}
}

func (a *App) setErr(msg string) {
	a.mu.Lock()
	a.lastErr = msg
	a.mu.Unlock()
}

// SetServeDir changes the shared folder, restarting the server if it runs.
func (a *App) SetServeDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("目录不存在: %s", dir)
	}
	a.mu.Lock()
	a.serveDir = dir
	a.mu.Unlock()
	return nil
}

func (a *App) ServeDir() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.serveDir
}

// --- transfers -----------------------------------------------------------

func (a *App) addTransfer(name, kind string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq++
	id := fmt.Sprintf("t%d", a.seq)
	a.transfers = append([]Transfer{{
		ID: id, Name: name, Kind: kind, State: "running", At: time.Now().Unix(),
	}}, a.transfers...)
	if len(a.transfers) > maxTransfers {
		a.transfers = a.transfers[:maxTransfers]
	}
	return id
}

func (a *App) finishTransfer(id, state, detail string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.transfers {
		if a.transfers[i].ID == id {
			a.transfers[i].State = state
			a.transfers[i].Detail = detail
			if state == "failed" {
				a.lastErr = detail
			}
			return
		}
	}
}

// PushFiles sends local files to the phone, one transfer record each.
func (a *App) PushFiles(paths []string, destDir string) {
	for _, p := range paths {
		p := p
		id := a.addTransfer(filepath.Base(p), "push")
		go func() {
			remote, err := PushFile(p, destDir)
			if err != nil {
				a.finishTransfer(id, "failed", err.Error())
				return
			}
			// The full device path is noise in an 11px line; the folder is
			// what the user actually wants to know.
			a.finishTransfer(id, "done", "已发送到手机 "+filepath.Base(filepath.Dir(remote)))
		}()
	}
}

// PullFile brings a device file into the shared folder.
func (a *App) PullFile(remote string) {
	id := a.addTransfer(baseName(remote), "pull")
	go func() {
		if _, err := PullFile(remote, a.ServeDir()); err != nil {
			a.finishTransfer(id, "failed", err.Error())
			return
		}
		// The destination is already shown under 共享目录, so naming it again
		// just fills the row.
		a.finishTransfer(id, "done", "已保存到共享目录")
	}()
}

// ClearTransfers empties the finished history.
func (a *App) ClearTransfers() {
	a.mu.Lock()
	defer a.mu.Unlock()
	kept := a.transfers[:0]
	for _, t := range a.transfers {
		if t.State == "running" {
			kept = append(kept, t)
		}
	}
	a.transfers = kept
}

// --- text handoff --------------------------------------------------------

// SetNote stores text for the phone to pick up.
//
// This exists alongside the clipboard mirror because sending a snippet
// shouldn't overwrite whatever the user has copied right now.
func (a *App) SetNote(text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.note = text
	a.noteAt = time.Now().Unix()
}

func (a *App) Note() (string, int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.note, a.noteAt
}
