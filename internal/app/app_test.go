package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/volcanicll/cabledrop/internal/model"
)

func TestResolveServeDir(t *testing.T) {
	t.Run("creates a fresh folder when neither exists", func(t *testing.T) {
		home := t.TempDir()
		got := resolveServeDir(home)
		want := filepath.Join(home, "CableDrop")
		if got != want {
			t.Fatalf("resolveServeDir = %q, want %q", got, want)
		}
		fi, err := os.Stat(want)
		if err != nil || !fi.IsDir() {
			t.Fatalf("folder was not created: %v", err)
		}
	})

	t.Run("keeps the folder when it already exists", func(t *testing.T) {
		home := t.TempDir()
		dir := filepath.Join(home, "CableDrop")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(dir, "keep.txt")
		if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := resolveServeDir(home); got != dir {
			t.Fatalf("resolveServeDir = %q, want %q", got, dir)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("existing content disturbed: %v", err)
		}
	})

	t.Run("migrates the pre-rename USBBridge folder once", func(t *testing.T) {
		home := t.TempDir()
		old := filepath.Join(home, "USBBridge")
		if err := os.MkdirAll(old, 0o755); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(old, "file.txt")
		if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		got := resolveServeDir(home)
		want := filepath.Join(home, "CableDrop")
		if got != want {
			t.Fatalf("resolveServeDir = %q, want %q", got, want)
		}
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Fatalf("old folder still there: %v", err)
		}
		if _, err := os.Stat(filepath.Join(want, "file.txt")); err != nil {
			t.Fatalf("content did not move: %v", err)
		}

		// A second pass must be a no-op: the migration is a rename, not a copy.
		if got := resolveServeDir(home); got != want {
			t.Fatalf("second call = %q, want %q", got, want)
		}
	})

	t.Run("leaves both alone when the new folder already exists", func(t *testing.T) {
		home := t.TempDir()
		old := filepath.Join(home, "USBBridge")
		dir := filepath.Join(home, "CableDrop")
		for _, d := range []string{old, dir} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if got := resolveServeDir(home); got != dir {
			t.Fatalf("resolveServeDir = %q, want %q", got, dir)
		}
		if _, err := os.Stat(old); err != nil {
			t.Fatalf("pre-existing old folder was removed: %v", err)
		}
	})
}

func TestTrayNeedsUpdate(t *testing.T) {
	// First observation always updates; after that only real changes do.
	if !trayNeedsUpdate(false, false, "", false, "") {
		t.Fatal("first observation must update")
	}
	if trayNeedsUpdate(true, true, "Pixel", true, "Pixel") {
		t.Fatal("unchanged state must not update")
	}
	if !trayNeedsUpdate(true, true, "Pixel", false, "Pixel") {
		t.Fatal("connection change must update")
	}
	if !trayNeedsUpdate(true, true, "Pixel", true, "Pixel 9") {
		t.Fatal("name change must update")
	}
}

// fakePanel and fakeTray record what the app drove.
type fakePanel struct {
	mu      sync.Mutex
	visible bool
	shows   int
	hides   int
}

func (p *fakePanel) Visible() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.visible
}
func (p *fakePanel) Show() {
	p.mu.Lock()
	p.visible = true
	p.shows++
	p.mu.Unlock()
}
func (p *fakePanel) Hide() {
	p.mu.Lock()
	p.visible = false
	p.hides++
	p.mu.Unlock()
}

type fakeTray struct {
	mu    sync.Mutex
	calls [][2]string // connected, tooltip
}

func (t *fakeTray) SetStatus(connected bool, tooltip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := "off"
	if connected {
		c = "on"
	}
	t.calls = append(t.calls, [2]string{c, tooltip})
}

func TestTransferStateMachine(t *testing.T) {
	a := New()

	// Newest first.
	id1 := a.addTransfer("a.txt", "push", 0)
	id2 := a.addTransfer("b.txt", "pull", 0)
	st := a.State()
	if len(st.Transfers) != 2 || st.Transfers[0].ID != id2 || st.Transfers[1].ID != id1 {
		t.Fatalf("order = %+v", st.Transfers)
	}
	if st.Transfers[0].State != "running" {
		t.Fatalf("new transfer state = %s", st.Transfers[0].State)
	}

	// Finishing writes state and detail; failures also land in Error.
	a.finishTransfer(id1, "done", "从手机取回 · 共享目录")
	a.finishTransfer(id2, "failed", "设备离线")
	st = a.State()
	if st.Transfers[1].State != "done" || st.Transfers[1].Detail != "从手机取回 · 共享目录" {
		t.Fatalf("done transfer = %+v", st.Transfers[1])
	}
	if st.Transfers[0].State != "failed" || st.Error != "设备离线" {
		t.Fatalf("failed transfer = %+v error = %q", st.Transfers[0], st.Error)
	}

	// Clear keeps running transfers, drops finished ones.
	id3 := a.addTransfer("c.txt", "push", 0)
	a.ClearTransfers()
	st = a.State()
	if len(st.Transfers) != 1 || st.Transfers[0].ID != id3 {
		t.Fatalf("after clear = %+v", st.Transfers)
	}

	// Finishing an unknown id is a no-op, not a panic.
	a.finishTransfer("t999", "done", "x")
}

func TestTransferHistoryIsCapped(t *testing.T) {
	a := New()
	for i := 0; i < maxTransfers+20; i++ {
		a.addTransfer("f.bin", "push", 0)
	}
	st := a.State()
	if len(st.Transfers) != maxTransfers {
		t.Fatalf("len = %d, want %d", len(st.Transfers), maxTransfers)
	}
	// The newest survive: the first-added twenty are gone.
	if st.Transfers[0].ID != "t60" {
		t.Fatalf("newest id = %s, want t60", st.Transfers[0].ID)
	}
}

func TestPushFilesRunsThroughTheFakeDevice(t *testing.T) {
	a := New()
	var calls []string
	a.pushFile = func(local, remoteDir string) (string, error) {
		calls = append(calls, local+"->"+remoteDir)
		return "/sdcard/Download/" + filepath.Base(local), nil
	}

	a.PushFiles([]string{"/tmp/a.txt", "/tmp/b.txt"}, "")
	// Both run in the background; wait for both transfer rows to settle.
	waitFor(t, func() bool {
		st := a.State()
		return len(st.Transfers) == 2 && st.Transfers[0].State == "done" && st.Transfers[1].State == "done"
	})

	st := a.State()
	if !strings.Contains(st.Transfers[0].Detail, "Download") {
		t.Fatalf("transfer = %+v", st.Transfers[0])
	}
}

func TestPushFailureWritesTheError(t *testing.T) {
	a := New()
	a.pushFile = func(local, remoteDir string) (string, error) {
		return "", errors.New("adb 超时")
	}
	a.PushFiles([]string{"/tmp/a.txt"}, "")
	waitFor(t, func() bool {
		st := a.State()
		return len(st.Transfers) == 1 && st.Transfers[0].State == "failed"
	})
	st := a.State()
	// Timeout is one of the errors the panel translates for the user; the
	// adb wording rides along as the secondary detail line.
	if !strings.Contains(st.Error, "USB 传输超时") || !strings.Contains(st.Error, "adb 超时") {
		t.Fatalf("state = %+v", st)
	}
	if !strings.Contains(st.Transfers[0].Detail, "重新插拔") {
		t.Fatalf("detail = %+v", st.Transfers[0])
	}
}

func TestPushSizeIsRecorded(t *testing.T) {
	a := New()
	a.pushFile = func(local, remoteDir string) (string, error) {
		return "/sdcard/Download/" + filepath.Base(local), nil
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "sized.bin"), make([]byte, 1234), 0o644)
	a.PushFiles([]string{filepath.Join(dir, "sized.bin")}, "")
	waitFor(t, func() bool {
		st := a.State()
		return len(st.Transfers) == 1 && st.Transfers[0].State == "done"
	})
	st := a.State()
	if st.Transfers[0].Size != 1234 {
		t.Fatalf("size = %d, want 1234", st.Transfers[0].Size)
	}
	if st.Transfers[0].Detail != "发送到手机 · Download" {
		t.Fatalf("detail = %q", st.Transfers[0].Detail)
	}
}

func TestHumanDeviceErrorMapsTheCommonFailures(t *testing.T) {
	if got := humanDeviceError("error: device unauthorized."); !strings.Contains(got, "允许 USB 调试") {
		t.Fatalf("unauthorized mapping broken: %q", got)
	}
	if got := humanDeviceError("error: device offline"); !strings.Contains(got, "重新插拔") {
		t.Fatalf("offline mapping broken: %q", got)
	}
	if got := humanDeviceError("some odd failure"); got != "some odd failure" {
		t.Fatalf("unknown error should pass through: %q", got)
	}
}

func TestPullDeviceFileGoesThroughTheFake(t *testing.T) {
	a := New()
	a.pullFile = func(remote, localDir string) (string, error) {
		return localDir + "/x.jpg", nil
	}
	a.PullDeviceFile("/sdcard/DCIM/x.jpg")
	waitFor(t, func() bool {
		st := a.State()
		return len(st.Transfers) == 1 && st.Transfers[0].State == "done"
	})
}

func TestPanelToggleAndGrace(t *testing.T) {
	a := New()
	p := &fakePanel{}
	a.AttachPanel(p)

	// Hidden → toggling shows.
	a.TogglePanel()
	if !p.Visible() {
		t.Fatal("toggle did not show the panel")
	}

	// Visible → toggling hides and stamps hiddenAt.
	a.TogglePanel()
	if p.Visible() {
		t.Fatal("toggle did not hide the panel")
	}

	// Within the grace window a second "open" click is swallowed: that is the
	// click whose own blur already closed the panel.
	a.TogglePanel()
	if p.Visible() {
		t.Fatal("grace window did not swallow the immediate re-open")
	}

	// Outside the grace window the panel opens again.
	time.Sleep(panelHideGrace + 20*time.Millisecond)
	a.TogglePanel()
	if !p.Visible() {
		t.Fatal("panel did not reopen after the grace window")
	}
}

func TestPanelFocusLostHides(t *testing.T) {
	a := New()
	p := &fakePanel{}
	a.AttachPanel(p)
	p.Show()
	a.PanelFocusLost()
	if p.Visible() {
		t.Fatal("focus loss did not hide the panel")
	}

	// A native blur-hide usually wins the race, so focus loss on an
	// already-hidden panel must STILL stamp the grace timestamp: the tray
	// click that stole focus has to read as "close it" a beat later.
	a.hidePanel()
	stamp := a.hiddenAt.Load()
	time.Sleep(2 * time.Millisecond)
	a.PanelFocusLost()
	if a.hiddenAt.Load() == stamp {
		t.Fatal("grace timestamp not refreshed on blur")
	}
}

func TestTrayDedupesBeforeTouchingTheIcon(t *testing.T) {
	a := New()
	tr := &fakeTray{}
	a.AttachTray(tr)

	a.mu.Lock()
	a.dev = model.Device{Serial: "S1", Model: "Pixel"}
	a.connected = true
	a.mu.Unlock()

	a.syncTray()
	a.syncTray() // no change: no second call
	if len(tr.calls) != 1 {
		t.Fatalf("calls = %v, want exactly one", tr.calls)
	}
	if tr.calls[0][0] != "on" || !strings.Contains(tr.calls[0][1], "Pixel") {
		t.Fatalf("call = %v", tr.calls[0])
	}

	// Disconnect changes the tooltip and the icon.
	a.mu.Lock()
	a.connected = false
	a.mu.Unlock()
	a.syncTray()
	if len(tr.calls) != 2 || tr.calls[1][0] != "off" {
		t.Fatalf("calls = %v", tr.calls)
	}
	if !strings.Contains(tr.calls[1][1], "未连接") {
		t.Fatalf("tooltip = %q", tr.calls[1][1])
	}
}

func TestStateSnapshotIsACopy(t *testing.T) {
	a := New()
	a.addTransfer("x", "push", 0)
	st := a.State()
	st.Transfers[0].Name = "mutated"
	if a.State().Transfers[0].Name == "mutated" {
		t.Fatal("State() exposed the internal slice")
	}
}

func TestNoteRoundTrip(t *testing.T) {
	a := New()
	a.SetNote("hello 你好")
	text, at := a.Note()
	if text != "hello 你好" || at == 0 {
		t.Fatalf("note = %q at %d", text, at)
	}
}

func TestPumpRefreshesOnTrackEvents(t *testing.T) {
	a := New()
	refreshes := make(chan struct{}, 100)
	stop := make(chan struct{})
	a.trackDevices = func(stop <-chan struct{}, onChange func()) error {
		onChange()
		onChange()
		onChange()
		<-stop
		return nil
	}
	done := make(chan struct{})
	go func() { a.pump(func() { refreshes <- struct{}{} }, stop); close(done) }()

	// The burst coalesces, but at least one refresh lands.
	select {
	case <-refreshes:
	case <-time.After(time.Second):
		t.Fatal("tracker event never triggered a refresh")
	}
	close(stop)
	<-done
}

func TestPumpFallsBackToPollingWhenTrackerFails(t *testing.T) {
	oldPoll := fallbackPoll
	fallbackPoll = 15 * time.Millisecond
	t.Cleanup(func() { fallbackPoll = oldPoll })

	a := New()
	a.trackDevices = func(stop <-chan struct{}, onChange func()) error {
		return errors.New("adb track-devices 不可用")
	}

	refreshes := 0
	var mu sync.Mutex
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		a.pump(func() {
			mu.Lock()
			refreshes++
			mu.Unlock()
		}, stop)
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := refreshes
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	mu.Lock()
	n := refreshes
	mu.Unlock()
	if n < 2 {
		t.Fatalf("refreshes = %d, want the fallback poll to fire repeatedly", n)
	}
	close(stop)
	<-done
}

func TestPumpSkipsTheTimerWhileTheStreamIsHealthy(t *testing.T) {
	oldPoll := fallbackPoll
	fallbackPoll = 15 * time.Millisecond
	t.Cleanup(func() { fallbackPoll = oldPoll })

	a := New()
	stop := make(chan struct{})
	a.trackDevices = func(stop <-chan struct{}, onChange func()) error {
		<-stop // healthy stream: runs until stopped, fires nothing
		return nil
	}
	refreshes := 0
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		a.pump(func() {
			mu.Lock()
			refreshes++
			mu.Unlock()
		}, stop)
		close(done)
	}()

	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	n := refreshes
	mu.Unlock()
	if n != 0 {
		t.Fatalf("refreshes = %d while the stream was healthy, want 0", n)
	}
	close(stop)
	<-done
}

func TestBatchPushReportsByteWeightedProgress(t *testing.T) {
	a := New()
	// The small file (25%) finishes immediately; the big one (75%) blocks on
	// release until the test has read the mid-flight state.
	release := make(chan struct{})
	blocked := make(chan struct{})
	a.pushFile = func(local, remoteDir string) (string, error) {
		if strings.Contains(local, "small") {
			return "/sdcard/Download/small.bin", nil
		}
		close(blocked)
		<-release
		return "/sdcard/Download/big.bin", nil
	}
	dir := t.TempDir()
	big := filepath.Join(dir, "big.bin")
	small := filepath.Join(dir, "small.bin")
	os.WriteFile(big, make([]byte, 75), 0o644)
	os.WriteFile(small, make([]byte, 25), 0o644)

	a.PushFiles([]string{big, small}, "")
	<-blocked
	waitFor(t, func() bool {
		st := a.State()
		for _, tr := range st.Transfers {
			if strings.HasSuffix(tr.Name, "small.bin") && tr.State == "done" {
				return true
			}
		}
		return false
	})

	// The still-running big row must show the batch's byte-weighted progress:
	// the 25% file is done, so 25 — not an indeterminate bar, not 100.
	var row model.Transfer
	waitFor(t, func() bool {
		st := a.State()
		for _, tr := range st.Transfers {
			if strings.HasSuffix(tr.Name, "big.bin") && tr.Percent != nil {
				row = tr
				return true
			}
		}
		return false
	})
	if row.Percent == nil || *row.Percent < 24 || *row.Percent > 26 {
		t.Fatalf("big row mid-flight = %+v, want percent ~25", row)
	}

	close(release)
	waitFor(t, func() bool {
		st := a.State()
		for _, tr := range st.Transfers {
			if strings.HasSuffix(tr.Name, "big.bin") {
				return tr.State == "done" && tr.Percent != nil && *tr.Percent == 100
			}
		}
		return false
	})
}

// waitFor spins until cond is true — the transfer goroutines are real
// goroutines, and the tests only need "eventually done", not a sleep.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not reached in time")
}

func TestPullRecordsTheSizeItLearned(t *testing.T) {
	a := New()
	dir := t.TempDir()
	a.serveDir = dir
	a.pullFile = func(remote, localDir string) (string, error) {
		dest := filepath.Join(localDir, "pulled.bin")
		os.WriteFile(dest, make([]byte, 4321), 0o644)
		return dest, nil
	}
	a.PullDeviceFile("/sdcard/pulled.bin")
	waitFor(t, func() bool {
		st := a.State()
		return len(st.Transfers) == 1 && st.Transfers[0].State == "done"
	})
	if got := a.State().Transfers[0].Size; got != 4321 {
		t.Fatalf("size = %d, want 4321 (stat of the local copy)", got)
	}
}
