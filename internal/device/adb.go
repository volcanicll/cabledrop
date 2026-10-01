// Package device wraps the adb command line and the device file operations
// built on it.
//
// Everything that reaches the phone goes through here, and every path coming
// back from a web page has to pass CheckDevicePath before this package will
// touch it.
package device

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/volcanicll/cabledrop/internal/model"
)

// ErrNoADB is returned when no adb binary can be found on this machine.
var ErrNoADB = errors.New("找不到 adb：请安装 Android Platform Tools，或把它放到本程序同目录下")

// Default is the process-wide adb handle.
var Default = &ADB{}

// ADB wraps the adb command line.
type ADB struct {
	mu     sync.Mutex
	path   string
	looked bool

	// runHook, when set, replaces process execution entirely. Tests inject
	// here to fake devices; production leaves it nil.
	runHook func(timeout time.Duration, args []string) (string, string, int, error)
}

// Path is the adb binary to use, located once. Empty when there is none.
func (a *ADB) Path() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.looked {
		a.path = findADB()
		a.looked = true
	}
	return a.path
}

// Refresh forgets the cached path, so a user who installs adb while the app is
// running doesn't have to restart it.
func (a *ADB) Refresh() {
	a.mu.Lock()
	a.looked = false
	a.mu.Unlock()
}

// findADB looks where adb actually is, in order of likelihood.
//
// A packaged app is launched from Finder or Explorer with none of the PATH a
// terminal has, so PATH alone is not enough — and Android Studio's SDK path is
// by far the most common place people already have it.
func findADB() string {
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	home, _ := os.UserHomeDir()

	name := "adb"
	if runtime.GOOS == "windows" {
		name = "adb.exe"
	}

	var candidates []string

	// 1. Explicit override, for anyone whose setup is unusual.
	if v := os.Getenv("CABLEDROP_ADB"); v != "" {
		candidates = append(candidates, v)
	}
	// 2. Shipped next to the executable — the portable install.
	candidates = append(candidates,
		filepath.Join(exeDir, name),
		filepath.Join(exeDir, "platform-tools", name),
	)
	// 3. Android Studio's SDK.
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			filepath.Join(home, "Library/Android/sdk/platform-tools", name),
			"/opt/homebrew/bin/"+name,
			"/usr/local/bin/"+name,
		)
	case "windows":
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			candidates = append(candidates, filepath.Join(la, "Android", "Sdk", "platform-tools", name))
		}
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			candidates = append(candidates, filepath.Join(pf, "Android", "platform-tools", name))
		}
	case "linux":
		candidates = append(candidates,
			filepath.Join(home, "Android/Sdk/platform-tools", name),
			"/usr/bin/"+name,
			"/usr/local/bin/"+name,
		)
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	// 4. Whatever PATH is in effect.
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// Run executes adb and returns stdout, stderr and the exit code.
//
// Both pipes are drained concurrently with the wait: a child that fills a pipe
// buffer while we block on Wait would otherwise deadlock.
func (a *ADB) Run(timeout time.Duration, args ...string) (string, string, int, error) {
	var stdout, stderr bytes.Buffer

	if a.runHook != nil {
		// The hook replaces only process execution; the exit-code and stderr
		// post-processing below is part of the contract and still applies.
		stdoutStr, stderrStr, code, err := a.runHook(timeout, args)
		stdout.WriteString(stdoutStr)
		stderr.WriteString(stderrStr)
		if err != nil {
			return stdout.String(), stderr.String(), code, err
		}
		return a.classify(stdout.String(), stderr.String(), code)
	}

	bin := a.Path()
	if bin == "" {
		return "", "", -1, ErrNoADB
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	// Android Studio's adb lives beside several DLLs on Windows; adding its
	// directory keeps those resolvable.
	cmd.Env = append(os.Environ(), "PATH="+prependPath(filepath.Dir(bin)))

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = nil

	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		switch {
		case errors.As(err, &ee):
			code = ee.ExitCode()
		case ctx.Err() == context.DeadlineExceeded:
			return stdout.String(), stderr.String(), -1,
				fmt.Errorf("adb 超时（%s）", timeout)
		default:
			return stdout.String(), stderr.String(), -1, err
		}
	}
	return a.classify(stdout.String(), stderr.String(), code)
}

// classify applies the exit-code contract: adb reports some device-side
// failures on stderr while still exiting 0, and those come back as code 1
// with the message as the error.
func (a *ADB) classify(stdout, stderr string, code int) (string, string, int, error) {
	if code == 0 && isFatalStderr(stderr) {
		return stdout, stderr, 1, errors.New(strings.TrimSpace(stderr))
	}
	return stdout, stderr, code, nil
}

// isFatalStderr catches the wrapper errors adb prints while exiting zero.
func isFatalStderr(s string) bool {
	for _, m := range []string{
		"error: device unauthorized",
		"error: device offline",
		"error: no devices/emulators found",
		"error: device not found",
	} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

func prependPath(dir string) string {
	if dir == "" {
		return os.Getenv("PATH")
	}
	sep := string(os.PathListSeparator)
	return dir + sep + os.Getenv("PATH")
}

// Devices lists the Android devices currently attached and authorised.
func (a *ADB) Devices() ([]model.Device, error) {
	out, _, code, err := a.Run(15*time.Second, "devices", "-l")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, errors.New("adb devices 失败")
	}

	var devices []model.Device
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		// "<serial>  device  usb:3-1 product:PJF110 model:PJF110 device:OP5CFBL1"
		if len(fields) < 2 || fields[1] != "device" {
			continue
		}
		// The header line has "device" in column 1 too; a serial never does.
		if fields[0] == "List" {
			continue
		}
		d := model.Device{Serial: fields[0]}
		for _, kv := range fields[2:] {
			switch {
			case strings.HasPrefix(kv, "model:"):
				d.Model = strings.TrimPrefix(kv, "model:")
			case strings.HasPrefix(kv, "product:"):
				d.Product = strings.TrimPrefix(kv, "product:")
			}
		}
		devices = append(devices, d)
	}
	return devices, nil
}

// FirstDevice returns the only device, or an error explaining what's wrong.
func (a *ADB) FirstDevice() (model.Device, error) {
	if a.Path() == "" {
		return model.Device{}, ErrNoADB
	}
	devices, err := a.Devices()
	if err != nil {
		return model.Device{}, err
	}
	if len(devices) == 0 {
		return model.Device{}, errors.New("没有检测到手机。请插上 USB 线，并在手机上把 USB 用途改成「传输文件」。")
	}
	return devices[0], nil
}

// Shell runs a command on the device.
func (a *ADB) Shell(timeout time.Duration, cmd string) (string, error) {
	out, errOut, code, err := a.Run(timeout, "shell", cmd)
	if err != nil {
		return out, err
	}
	if code != 0 {
		msg := strings.TrimSpace(errOut)
		if msg == "" {
			msg = strings.TrimSpace(out)
		}
		if msg == "" {
			msg = "设备命令失败"
		}
		return out, errors.New(msg)
	}
	return out, nil
}

// TrackDevices keeps a persistent `adb track-devices` connection and calls
// onChange every time the device list changes (coalescing is the caller's
// job — one change may emit several lines).
//
// This is how the watcher avoids forking adb on a timer: the stream pushes
// each plug/unplug over one connection, so a change is noticed immediately
// and idle costs nothing. A dropped stream is retried with a short backoff
// until stop is closed; an error return means the stream could not be started
// at all — no adb binary, or one too old to know track-devices — and the
// caller should fall back to polling.
func (a *ADB) TrackDevices(stop <-chan struct{}, onChange func()) error {
	if a.Path() == "" {
		return ErrNoADB
	}

	for attempt := 0; ; attempt++ {
		bin := a.Path()
		ctx, cancel := context.WithCancel(context.Background())
		cmd := exec.CommandContext(ctx, bin, "track-devices")
		// Same env widening as Run: Android Studio's adb needs its DLLs.
		cmd.Env = append(os.Environ(), "PATH="+prependPath(filepath.Dir(bin)))
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			cancel()
			return err
		}
		if err := cmd.Start(); err != nil {
			cancel()
			// An adb without track-devices fails instantly on every attempt;
			// only the first start error is reported so the caller can poll.
			if attempt == 0 {
				return err
			}
		} else {
			go func() {
				select {
				case <-stop:
					cancel()
				case <-ctx.Done():
				}
			}()

			sawOutput := false
			sc := bufio.NewScanner(stdout)
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if trackLineMeansChange(line) {
					sawOutput = true
					onChange()
				}
			}
			_ = cmd.Wait()
			cancel()

			select {
			case <-stop:
				return nil
			default:
			}
			// A first attempt that produced nothing and failed usually means
			// an adb without track-devices support. Report it once so the
			// caller polls instead of waiting on a dead stream.
			if attempt == 0 && !sawOutput {
				return errors.New("adb track-devices 不可用")
			}
		}

		// Stream dropped (adb server restarted, USB stack hiccup): retry.
		select {
		case <-stop:
			return nil
		case <-time.After(2 * time.Second):
		}
	}
}

// trackLineMeansChange decides whether one line of track-devices output is a
// device-list change worth a refresh: the "List of devices attached" header
// and every "<serial>\t<state>" line are; daemon chatter and blanks are not.
//
// Firing on the header matters: an unplug re-prints the header alone, and
// that is the only signal the last device went away.
func trackLineMeansChange(line string) bool {
	if line == "" {
		return false
	}
	if strings.Contains(line, "daemon") {
		return false
	}
	return true
}
