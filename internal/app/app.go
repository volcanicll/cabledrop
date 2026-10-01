// Package app is the state machine: device tracking, transfers, the adb
// reverse tunnel and the shared folder.
//
// It implements serve.Backend — that is the only contract the HTTP layer gets
// — and defines the small PanelUI/TrayUI surfaces the desktop shell (package
// ui) implements. app never imports ui, so the UI can be replaced or run
// headless.
package app

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/volcanicll/cabledrop/internal/clipboard"
	"github.com/volcanicll/cabledrop/internal/device"
	"github.com/volcanicll/cabledrop/internal/model"
	"github.com/volcanicll/cabledrop/internal/picker"
	"github.com/volcanicll/cabledrop/internal/serve"
)

// PanelUI is the visible panel surface app needs (implemented by ui).
type PanelUI interface {
	Visible() bool
	Show()
	Hide()
}

// TrayUI is the menu-bar icon surface (implemented by ui).
type TrayUI interface {
	// SetStatus applies the connection state to the icon and tooltip.
	SetStatus(connected bool, tooltip string)
}

// App holds all the state the panel and the phone both read.
type App struct {
	mu sync.Mutex

	clip     *clipboard.Clipboard
	serveDir string

	listener net.Listener
	port     int

	transfers []model.Transfer
	seq       int

	dev       model.Device
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

	panelUI PanelUI
	trayUI  TrayUI

	// pushFile and pullFile are the device transfer operations. Tests inject
	// fakes here; production leaves them nil and the device package is used.
	pushFile func(local, remoteDir string) (string, error)
	pullFile func(remote, localDir string) (string, error)
	// trackDevices is the persistent device watcher; tests replace it.
	trackDevices func(stop <-chan struct{}, onChange func()) error

	// hiddenAt is when the panel last dismissed itself, in unix millis. The
	// tray click handler needs it to tell "user clicked to close" apart from
	// "the click itself stole focus and the blur handler already closed it".
	hiddenAt atomic.Int64

	stopWatch chan struct{}
}

// Compile-time proof that the app satisfies the HTTP layer's contract.
var _ serve.Backend = (*App)(nil)

// newPhoneServer builds the listener the phone's browser talks to, serving the
// phone layout at "/" over the same Backend the panel uses.
func (a *App) newPhoneServer() *http.Server {
	return serve.NewServer(a, true)
}

const (
	defaultPort  = 8765
	maxTransfers = 40
)

func New() *App {
	return &App{
		clip:      clipboard.New(),
		serveDir:  defaultServeDir(),
		transfers: make([]model.Transfer, 0, 8),
		native:    true,
	}
}

// AttachPanel wires the desktop shell's panel in. Both are safe to call with
// nil or before the shell exists: every use checks.
func (a *App) AttachPanel(p PanelUI) { a.panelUI = p }

// AttachTray wires the desktop shell's tray icon in.
func (a *App) AttachTray(t TrayUI) { a.trayUI = t }

// StartClipboard begins mirroring the desktop clipboard without starting the
// device watcher. The dev server uses it; the real app gets both from Start.
func (a *App) StartClipboard() { a.clip.Start() }

// defaultServeDir is a dedicated folder rather than Downloads: the point of
// this app is a predictable place things land, not more clutter in Downloads.
func defaultServeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return resolveServeDir(home)
}

// resolveServeDir picks the shared folder under home, taking a pre-rename
// install's ~/USBBridge along to the new name. The move happens once, only
// when the new folder does not exist yet, and failing it (permissions, another
// volume) is not fatal: a fresh folder is created instead and the old one is
// left for the user to deal with by hand.
func resolveServeDir(home string) string {
	dir := filepath.Join(home, "CableDrop")
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	old := filepath.Join(home, "USBBridge")
	if fi, err := os.Stat(old); err == nil && fi.IsDir() {
		if os.Rename(old, dir) == nil {
			return dir
		}
	}
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

// fallbackPoll is the watch cadence when the track-devices stream is not
// available — old adb, no adb yet, or a stream that keeps dropping. Variable,
// so tests can speed it up.
var fallbackPoll = 15 * time.Second

// watch drives device detection.
//
// The preferred source is `adb track-devices`: one persistent connection that
// pushes every plug/unplug, so changes are noticed immediately and an idle
// system forks no adb at all — the old two-second poll spent a 19 MB process
// spawn every two seconds for nothing. When the stream cannot run, the watcher
// falls back to a low-frequency poll; when it can, the fallback timer ticks by
// without doing anything.
func (a *App) watch() {
	a.refresh()
	a.pump(a.refresh, a.stopWatch)
}

// pump multiplexes tracker events and the fallback timer into refresh calls.
// It is split out from watch so tests can drive it with a counting refresh
// and a fake tracker instead of real adb.
func (a *App) pump(refresh func(), stop <-chan struct{}) {
	changed := make(chan struct{}, 1)
	trackerFailed := make(chan struct{})
	go func() {
		defer close(trackerFailed)
		track := a.trackDevices
		if track == nil {
			track = device.Default.TrackDevices
		}
		if err := track(stop, func() {
			// Coalesce bursts: one refresh per change is enough, and the
			// channel send from multiple lines collapses into one.
			select {
			case changed <- struct{}{}:
			default:
			}
		}); err != nil {
			log.Printf("设备跟踪不可用，回退到低频轮询: %v", err)
		}
	}()

	t := time.NewTicker(fallbackPoll)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-changed:
			refresh()
		case <-t.C:
			select {
			case <-trackerFailed:
				refresh()
			default:
				// The stream is healthy: nothing to poll for.
			}
		}
	}
}

func (a *App) refresh() {
	if device.Default.Path() == "" {
		device.Default.Refresh()
	}

	a.mu.Lock()
	// The very first pass has no previous state to compare against, and it can
	// run concurrently with the server starting up. Treating it as a
	// transition would tear down a listener that was just created.
	first := !a.everRefreshed
	a.everRefreshed = true
	prevConnected := a.connected
	a.mu.Unlock()

	dev, err := device.Default.FirstDevice()
	a.mu.Lock()
	switch {
	case err != nil:
		a.connected = false
		a.free = ""
		a.dev = model.Device{}
		if len(a.transfers) == 0 {
			a.lastErr = ""
		}
	default:
		a.connected = true
		a.dev = dev
		a.mu.Unlock()
		free := device.FreeSpaceOnDevice()
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

// Refresh re-detects the device right now. The tray menu and /api/refresh
// both land here.
func (a *App) Refresh() {
	device.Default.Refresh()
	a.refresh()
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
	device.Default.Run(15*time.Second, "reverse", remote, remote)
}

func (a *App) isConnected() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.connected
}

// Connected reports whether a phone is attached right now.
func (a *App) Connected() bool { return a.isConnected() }

// Native is always true here: the app struct is the desktop side. A phone
// request reaches the same handler but Backend through a listener that was
// built for it — native-gated endpoints are gated per-listener, not here.
func (a *App) Native() bool { return a.native }

func (a *App) Serving() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.listener != nil
}

// trayNeedsUpdate is the dedupe behind syncTray, as a pure function: the
// watcher calls syncTray every two seconds, and re-setting an NSStatusItem
// image each time makes AppKit reload its preferences and redraw for nothing.
func trayNeedsUpdate(set bool, lastConnected bool, lastName string, connected bool, name string) bool {
	return !set || lastConnected != connected || lastName != name
}

// syncTray updates the menu bar icon and tooltip. It is a no-op unless
// something it shows actually changed — see trayNeedsUpdate.
func (a *App) syncTray() {
	if a.trayUI == nil {
		return
	}
	connected := a.isConnected()

	a.mu.Lock()
	name := a.dev.Name()
	changed := trayNeedsUpdate(a.traySet, a.trayConn, a.trayName, connected, name)
	a.traySet, a.trayConn, a.trayName = true, connected, name
	a.mu.Unlock()

	if !changed {
		return
	}

	tooltip := "CableDrop · 未连接手机"
	if connected {
		tooltip = "CableDrop · " + name
	}
	a.trayUI.SetStatus(connected, tooltip)
}

// --- panel ----------------------------------------------------------------

// panelHideGrace is how long, after the panel dismissed itself, a tray click
// still counts as "close it" rather than "open it".
const panelHideGrace = 350 * time.Millisecond

// TogglePanel is the tray icon click: show the panel when hidden, hide it
// when shown.
//
// The grace window exists because the very click that lands on the tray icon
// can take key focus away from the panel first (on macOS the status item
// window becomes key before the action fires). The blur handler then dismisses
// the panel a beat before this handler reads visibility, and a naive toggle
// would immediately re-open what the user just asked to close.
func (a *App) TogglePanel() {
	if a.panelVisible() {
		a.hidePanel()
		return
	}
	if time.Since(time.UnixMilli(a.hiddenAt.Load())) < panelHideGrace {
		return
	}
	a.showPanel()
}

// PanelFocusLost dismisses the panel whenever it stops being the key window —
// a click on the desktop, another app, anything. This is what makes the panel
// behave like a menu instead of a stubborn always-on-top overlay.
func (a *App) PanelFocusLost() {
	if !a.panelVisible() {
		return
	}
	a.hidePanel()
}

func (a *App) panelVisible() bool {
	if a.panelUI == nil {
		return false
	}
	return a.panelUI.Visible()
}

func (a *App) showPanel() {
	if a.panelUI == nil {
		return
	}
	a.panelUI.Show()
}

func (a *App) hidePanel() {
	a.hiddenAt.Store(time.Now().UnixMilli())
	if a.panelUI == nil {
		return
	}
	a.panelUI.Hide()
}

// HidePanel is the API-facing form: the page's Escape key lands here.
func (a *App) HidePanel() { a.hidePanel() }

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
func (a *App) State() model.State {
	a.mu.Lock()
	defer a.mu.Unlock()

	tr := make([]model.Transfer, len(a.transfers))
	copy(tr, a.transfers)

	return model.State{
		Platform:    runtime.GOOS,
		ADBFound:    device.Default.Path() != "",
		ADBPath:     device.Default.Path(),
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
	a.mu.Unlock()

	srv := a.newPhoneServer()
	go func() {
		if err := srv.Serve(ln); err != nil {
			log.Printf("本地服务结束: %v", err)
		}
	}()

	// Point the device's own loopback at us. A failure here is reported but
	// doesn't tear the listener down: the panel can still be reached from this
	// machine, which is what makes the app diagnosable when adb misbehaves.
	remote := fmt.Sprintf("tcp:%d", port)
	if _, errOut, code, err := device.Default.Run(15*time.Second, "reverse", remote, remote); err != nil || code != 0 {
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
		device.Default.Run(10*time.Second, "reverse", "--remove", fmt.Sprintf("tcp:%d", port))
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
	a.transfers = append([]model.Transfer{{
		ID: id, Name: name, Kind: kind, State: "running", At: time.Now().Unix(),
		// Indeterminate by default: a single adb push or pull exposes no
		// stream progress, so the UI shows a moving bar plus elapsed time.
		Indeterminate: true,
		StartedAt:     time.Now().UnixMilli(),
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
			a.transfers[i].Indeterminate = false
			if state == "done" {
				pct := 100.0
				a.transfers[i].Percent = &pct
			}
			if state == "failed" {
				a.lastErr = detail
			}
			return
		}
	}
}

// setTransferPercent records how far a multi-file batch has come. Unknown
// sizes degrade silently: without a total there is no honest percentage and
// the row stays indeterminate.
func (a *App) setTransferPercent(id string, pct float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.transfers {
		if a.transfers[i].ID == id {
			a.transfers[i].Indeterminate = false
			p := pct
			a.transfers[i].Percent = &p
			return
		}
	}
}

// PushFiles sends local files to the phone, one transfer record each.
//
// A batch of known sizes shares one byte-weighted percentage: each finished
// file bumps every still-running row by its own share, so the panel shows one
// honest number for the drag instead of forty separate unknowns.
func (a *App) PushFiles(paths []string, destDir string) {
	push := a.pushFile
	if push == nil {
		push = device.PushFile
	}

	total := int64(0)
	sizes := make(map[string]int64, len(paths))
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			sizes[p] = fi.Size()
			total += fi.Size()
		}
	}
	batch := len(paths) > 1 && total > 0
	ids := make(map[string]string, len(paths))
	idOf := func(p string) string { return ids[p] }
	var done int64
	var mu sync.Mutex
	bump := func(justFinished string) {
		if !batch {
			return
		}
		mu.Lock()
		done += sizes[justFinished]
		pct := 100 * float64(done) / float64(total)
		mu.Unlock()
		for _, p := range paths {
			if p != justFinished {
				a.setTransferPercent(idOf(p), pct)
			}
		}
	}

	for _, p := range paths {
		p := p
		id := a.addTransfer(filepath.Base(p), "push")
		ids[p] = id
		go func() {
			remote, err := push(p, destDir)
			if err != nil {
				a.finishTransfer(id, "failed", err.Error())
				return
			}
			bump(p)
			// The full device path is noise in an 11px line; the folder is
			// what the user actually wants to know.
			a.finishTransfer(id, "done", "已发送到手机 "+filepath.Base(filepath.Dir(remote)))
		}()
	}
}

// PullDeviceFile brings a device file into the shared folder.
func (a *App) PullDeviceFile(remote string) {
	pull := a.pullFile
	if pull == nil {
		pull = device.PullFile
	}
	id := a.addTransfer(baseName(remote), "pull")
	go func() {
		if _, err := pull(remote, a.ServeDir()); err != nil {
			a.finishTransfer(id, "failed", err.Error())
			return
		}
		// The destination is already shown under 共享目录, so naming it again
		// just fills the row.
		a.finishTransfer(id, "done", "已保存到共享目录")
	}()
}

// baseName is device-agnostic: the last path segment, either separator.
func baseName(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
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

// --- serve.Backend glue --------------------------------------------------

func (a *App) ClipboardText() string { return a.clip.Text() }

func (a *App) ClipboardSet(text string) error { return a.clip.Set(text) }

func (a *App) ListDeviceDir(dir string) ([]model.Entry, error) { return device.ListDir(dir) }

func (a *App) DeviceDirs() []model.Entry { return device.DeviceDirs() }

func (a *App) CheckDevicePath(path string) error { return device.CheckDevicePath(path) }

func (a *App) DeleteDevicePath(remote string) error { return device.DeletePath(remote) }

// PickFiles opens the system file picker, stepping the panel aside for the
// duration so the dialog is not hidden behind the always-on-top panel.
func (a *App) PickFiles(prompt string) ([]string, error) {
	wasVisible := a.beginModal()
	defer a.endModal(wasVisible)
	return picker.PickFiles(prompt)
}

// PickDir is PickFiles for a single directory.
func (a *App) PickDir(prompt string) (string, error) {
	wasVisible := a.beginModal()
	defer a.endModal(wasVisible)
	return picker.PickDir(prompt)
}

// OpenPath reveals a folder in Finder, Explorer or the desktop's file manager,
// so the user doesn't have to remember where it is.
func (a *App) OpenPath(path string) {
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
