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
	id1 := a.addTransfer("a.txt", "push")
	id2 := a.addTransfer("b.txt", "pull")
	st := a.State()
	if len(st.Transfers) != 2 || st.Transfers[0].ID != id2 || st.Transfers[1].ID != id1 {
		t.Fatalf("order = %+v", st.Transfers)
	}
	if st.Transfers[0].State != "running" {
		t.Fatalf("new transfer state = %s", st.Transfers[0].State)
	}

	// Finishing writes state and detail; failures also land in Error.
	a.finishTransfer(id1, "done", "已保存到共享目录")
	a.finishTransfer(id2, "failed", "设备离线")
	st = a.State()
	if st.Transfers[1].State != "done" || st.Transfers[1].Detail != "已保存到共享目录" {
		t.Fatalf("done transfer = %+v", st.Transfers[1])
	}
	if st.Transfers[0].State != "failed" || st.Error != "设备离线" {
		t.Fatalf("failed transfer = %+v error = %q", st.Transfers[0], st.Error)
	}

	// Clear keeps running transfers, drops finished ones.
	id3 := a.addTransfer("c.txt", "push")
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
		a.addTransfer("f.bin", "push")
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
	if st.Error != "adb 超时" || st.Transfers[0].Detail != "adb 超时" {
		t.Fatalf("state = %+v", st)
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
	// Hiding an already-hidden panel is a no-op.
	hides := p.hides
	a.PanelFocusLost()
	if p.hides != hides {
		t.Fatal("hidden panel was hidden again")
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
	a.addTransfer("x", "push")
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

// waitFor spins until cond is true — the transfer goroutines are real
// goroutines, and the tests only need "eventually done", not a sleep.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not reached in time")
}
