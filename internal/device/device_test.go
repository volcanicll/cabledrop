package device

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcanicll/cabledrop/internal/model"
)

// fakeADB is an ADB whose shell answers from a script instead of a process.
type fakeADB struct {
	ADB
	commands []string
	// reply decides what a shell command returns.
	reply func(cmd string) (string, error)
	// pushReply, when set, is what `push` returns.
	pushReply func(local, remote string) (string, string, int, error)
}

func newFakeADB(reply func(cmd string) (string, error)) *fakeADB {
	f := &fakeADB{reply: reply}
	f.path = "/fake/adb"
	f.looked = true
	f.runHook = func(timeout time.Duration, args []string) (string, string, int, error) {
		f.commands = append(f.commands, strings.Join(args, " "))
		if len(args) >= 2 && args[0] == "shell" {
			out, err := f.reply(strings.Join(args[1:], " "))
			return out, "", 0, err
		}
		if args[0] == "push" && f.pushReply != nil {
			out, errOut, code, err := f.pushReply(args[1], args[2])
			return out, errOut, code, err
		}
		// adb subcommands like `devices -l` reach the script too.
		out, err := f.reply(strings.Join(args, " "))
		return out, "", 0, err
	}
	return f
}

func TestShellQuoteSurvivesEmbeddedQuotes(t *testing.T) {
	sq := func(s string) string { return "'" + s + "'" }
	escaped := `'\''`
	cases := []struct{ in, want string }{
		{"/sdcard", sq("/sdcard")},
		{"/sdcard/a b", sq("/sdcard/a b")},
		{"/sdcard/it's", sq("/sdcard/it" + escaped + "s")},
		{"/sdcard/中文", sq("/sdcard/中文")},
		{"", sq("")},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestListDirParsesStatOutput(t *testing.T) {
	f := newFakeADB(func(cmd string) (string, error) {
		if !strings.HasPrefix(cmd, "for f in '/sdcard/DCIM'") {
			t.Fatalf("unexpected command: %s", cmd)
		}
		// Directories first by mtime, a dotfile, an unexpanded glob for an
		// empty subdirectory, and a symlink.
		return strings.Join([]string{
			"41a4|4096|1700000100|/sdcard/DCIM/Camera",
			"81a4|2048|1700000200|/sdcard/DCIM/note.txt",
			"a1ff|0|1700000000|/sdcard/DCIM/link",
			"81a4|10|1700000000|/sdcard/DCIM/.nomedia",
			"81a4|10|1700000000|/sdcard/DCIM/*",
			"garbage-without-pipes",
			"",
		}, "\n"), nil
	})

	entries, err := f.ListDir("/sdcard/DCIM")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
	// Directories first, then newest first. Symlinks count as directories so
	// the browser can enter them.
	if entries[0].Name != "Camera" || !entries[0].Dir {
		t.Errorf("entries[0] = %+v, want Camera dir", entries[0])
	}
	if entries[1].Name != "link" || !entries[1].Dir {
		t.Errorf("entries[1] = %+v, want link dir", entries[1])
	}
	if entries[2].Name != "note.txt" || entries[2].Dir || entries[2].Size != 2048 {
		t.Errorf("entries[2] = %+v", entries[2])
	}
}

func TestListDirRejectsPathsOutsideTheWhitelist(t *testing.T) {
	f := newFakeADB(func(string) (string, error) { return "", nil })
	for _, dir := range []string{"/etc", "relative", "/sdcard/../data", "/storageemulated"} {
		if _, err := f.ListDir(dir); !errors.Is(err, model.ErrBadDevicePath) {
			t.Errorf("ListDir(%q) err = %v, want ErrBadDevicePath", dir, err)
		}
	}
	if len(f.commands) != 0 {
		t.Errorf("adb was invoked for rejected paths: %v", f.commands)
	}

	// An empty path means the default root, which is whitelisted.
	if _, err := f.ListDir(""); err != nil {
		t.Errorf("ListDir(\"\") = %v, want the /sdcard listing", err)
	}
	if len(f.commands) != 1 || !strings.Contains(f.commands[0], "'/sdcard'") {
		t.Errorf("empty path did not fall back to /sdcard: %v", f.commands)
	}
}

func TestPushFileCreatesDestinationThenPushes(t *testing.T) {
	f := newFakeADB(func(cmd string) (string, error) {
		if strings.HasPrefix(cmd, "test -d ") {
			// Only /sdcard/Download exists of the default upload candidates.
			if strings.Contains(cmd, "/sdcard/Download") {
				return "yes", nil
			}
			return "", errors.New("missing")
		}
		return "", nil
	})
	f.pushReply = func(local, remote string) (string, string, int, error) {
		return "", "", 0, nil
	}

	remote, err := f.PushFile("/tmp/photo.jpg", "")
	if err != nil {
		t.Fatal(err)
	}
	if remote != "/sdcard/Download/photo.jpg" {
		t.Fatalf("remote = %q", remote)
	}
	// mkdir -p ran for the destination before the push. (The fake's first
	// command is the defaultUploadDir probe, which is expected.)
	mkdir, push := -1, -1
	for i, c := range f.commands {
		switch {
		case strings.Contains(c, "mkdir -p '/sdcard/Download/'"):
			mkdir = i
		case strings.Contains(c, "push /tmp/photo.jpg /sdcard/Download/"):
			push = i
		}
	}
	if mkdir == -1 || push == -1 || mkdir > push {
		t.Fatalf("commands = %v, want mkdir before push", f.commands)
	}
}

func TestPushFileRefusesADestinedPathOutsideTheWhitelist(t *testing.T) {
	f := newFakeADB(func(string) (string, error) { return "", nil })
	if _, err := f.PushFile("/tmp/x", "/data/local"); !errors.Is(err, model.ErrBadDevicePath) {
		t.Fatalf("err = %v", err)
	}
	if len(f.commands) != 0 {
		t.Fatalf("commands ran: %v", f.commands)
	}
}

func TestPullFileChecksThePath(t *testing.T) {
	f := newFakeADB(func(string) (string, error) { return "", nil })
	if _, err := f.PullFile("/etc/passwd", t.TempDir()); !errors.Is(err, model.ErrBadDevicePath) {
		t.Fatalf("err = %v", err)
	}
	if len(f.commands) != 0 {
		t.Fatalf("commands ran: %v", f.commands)
	}
}

func TestDeviceDirsOnlyReportsWhatExists(t *testing.T) {
	f := newFakeADB(func(cmd string) (string, error) {
		if strings.Contains(cmd, "/sdcard/Download") || strings.Contains(cmd, "/sdcard/DCIM/Camera") {
			return "yes", nil
		}
		return "", errors.New("missing")
	})
	dirs := f.DeviceDirs()
	if len(dirs) != 2 || dirs[0].Name != "Download" || dirs[1].Name != "相机" {
		t.Fatalf("dirs = %+v", dirs)
	}
}

func TestFreeSpaceOnDeviceReadsTheDfOutput(t *testing.T) {
	f := newFakeADB(func(cmd string) (string, error) {
		return "/dev/block/dfu 107G 49G 58G 46% /data/media", nil
	})
	if got := f.FreeSpaceOnDevice(); got != "58G" {
		t.Fatalf("free = %q, want 58G", got)
	}
}

func TestADBReportsStderrFailuresWrappedInZeroExit(t *testing.T) {
	f := &ADB{path: "/fake/adb", looked: true}
	f.runHook = func(time.Duration, []string) (string, string, int, error) {
		return "", "error: device unauthorized.\n", 0, nil
	}
	if _, _, code, err := f.Run(time.Second, "devices"); code != 1 || err == nil {
		t.Fatalf("code=%d err=%v, want 1 + error", code, err)
	}
}

func TestADBWithoutBinaryFailsFast(t *testing.T) {
	f := &ADB{path: "", looked: true}
	if _, _, _, err := f.Run(time.Second, "devices"); !errors.Is(err, ErrNoADB) {
		t.Fatalf("err = %v, want ErrNoADB", err)
	}
}

// TrackDevices is the one operation that needs a real process (it reads a
// pipe), so it runs against a shell script standing in for adb.
func TestTrackDevicesFiresOnEachBlock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stands in for adb with a POSIX shell script")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "adb")
	code := `#!/bin/sh
if [ "$1" != "track-devices" ]; then
  echo "unexpected: $1" >&2; exit 9
fi
echo "List of devices attached"
echo "SERIAL1	device"
# exec so the killed process is the sleeper itself: a plain sleep child
# would inherit the stdout pipe and outlive the kill.
exec sleep 30
`
	if err := os.WriteFile(script, []byte(code), 0o755); err != nil {
		t.Fatal(err)
	}

	f := &ADB{path: script, looked: true}
	stop := make(chan struct{})
	var changes int32
	done := make(chan error, 1)
	go func() { done <- f.TrackDevices(stop, func() { atomic.AddInt32(&changes, 1) }) }()

	waitForCond(t, func() bool { return atomic.LoadInt32(&changes) >= 1 }, 5*time.Second)
	close(stop)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("TrackDevices = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TrackDevices did not stop")
	}
}

// An adb too old for track-devices prints nothing and exits: the watcher must
// hear about it once, so it can poll instead.
func TestTrackDevicesReportsUnsupportedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stands in for adb with a POSIX shell script")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "adb")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"unknown command\" >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	f := &ADB{path: script, looked: true}
	err := f.TrackDevices(make(chan struct{}), func() {})
	if err == nil {
		t.Fatal("expected an error from an adb without track-devices")
	}
}

func TestTrackLineMeansChange(t *testing.T) {
	yes := []string{"List of devices attached", "SERIAL1\tdevice", "SERIAL2\tunauthorized"}
	no := []string{"", "* daemon not running; starting now at tcp:5037", "* daemon started successfully"}
	for _, line := range yes {
		if !trackLineMeansChange(line) {
			t.Errorf("trackLineMeansChange(%q) = false", line)
		}
	}
	for _, line := range no {
		if trackLineMeansChange(line) {
			t.Errorf("trackLineMeansChange(%q) = true", line)
		}
	}
}

func waitForCond(t *testing.T, cond func() bool, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not reached in time")
}

// Some vendors ship no model: column — FirstDevice then asks the phone
// itself. marketname wins; an empty marketname falls through to model.
func TestFirstDeviceAsksThePhoneForItsName(t *testing.T) {
	f := newFakeADB(func(cmd string) (string, error) {
		if strings.Contains(cmd, "devices -l") {
			return "List of devices attached\nSERIAL1\tdevice usb:1-1 product:PX8 device:PX8", nil
		}
		if strings.Contains(cmd, "getprop ro.product.marketname") {
			return "一加 12\nPJF110", nil
		}
		return "", nil
	})
	dev, err := f.FirstDevice()
	if err != nil {
		t.Fatal(err)
	}
	if dev.Model != "一加 12" {
		t.Fatalf("model = %q, want the marketing name", dev.Model)
	}
}

func TestFirstDeviceFallsBackWhenGetpropFails(t *testing.T) {
	f := newFakeADB(func(cmd string) (string, error) {
		if strings.Contains(cmd, "devices -l") {
			return "List of devices attached\nSERIAL1\tdevice", nil
		}
		return "", errors.New("closed")
	})
	dev, err := f.FirstDevice()
	if err != nil {
		t.Fatal(err)
	}
	if dev.Model != "" || dev.Name() != "SERIAL1" {
		t.Fatalf("fallback broken: %+v name=%q", dev, dev.Name())
	}
}

func TestDeviceNameUnderscoresBecomeSpaces(t *testing.T) {
	if got := (model.Device{Model: "Pixel_8_Pro"}).Name(); got != "Pixel 8 Pro" {
		t.Fatalf("Name = %q", got)
	}
	if got := (model.Device{Serial: "0123456789ABCDEF"}).Name(); got != "0123456789ABCDEF" {
		t.Fatalf("serial fallback broken: %q", got)
	}
}
