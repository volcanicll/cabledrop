package main

import (
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Clipboard mirrors the desktop's clipboard.
//
// It shells out to the platform's own tool (pbcopy/pbpaste, PowerShell,
// xclip) rather than binding to a native API: that keeps the binary free of
// cgo on Windows and Linux, which is what lets the whole app cross-compile.
//
// Why this matters for the product: Android 10 and later refuse to let
// `adb shell` write the device clipboard, so "copy here, paste there" cannot
// be done over adb. The desktop's clipboard is instead published through the
// local page, and the phone's browser copies it via the Clipboard API.
type Clipboard struct {
	mu     sync.RWMutex
	text   string
	stopCh chan struct{}
	once   sync.Once

	// onChange is called with new text whenever the clipboard changes.
	onChange func(string)
}

func NewClipboard() *Clipboard {
	return &Clipboard{}
}

// Text returns the last known clipboard contents.
func (c *Clipboard) Text() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.text
}

// Set replaces the clipboard and remembers it, so the watcher doesn't report
// our own write back as a fresh copy.
func (c *Clipboard) Set(s string) error {
	if err := writeClipboard(s); err != nil {
		return err
	}
	c.mu.Lock()
	c.text = s
	c.mu.Unlock()
	return nil
}

// Start begins watching. Safe to call more than once.
func (c *Clipboard) Start() {
	c.once.Do(func() {
		c.stopCh = make(chan struct{})
		// Seed once so the first page load has something to show.
		if s, err := readClipboard(); err == nil {
			c.mu.Lock()
			c.text = s
			c.mu.Unlock()
		}
		go c.watch()
	})
}

func (c *Clipboard) Stop() {
	if c.stopCh != nil {
		close(c.stopCh)
	}
}

// pollInterval is short enough that copying something and switching to the
// phone feels immediate, and long enough that the subprocess spawn is
// negligible. Windows is slower to answer, so it polls less often.
func pollInterval() time.Duration {
	if runtime.GOOS == "windows" {
		return 2 * time.Second
	}
	return 1 * time.Second
}

func (c *Clipboard) watch() {
	t := time.NewTicker(pollInterval())
	defer t.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-t.C:
			s, err := readClipboard()
			if err != nil {
				continue
			}
			c.mu.RLock()
			same := s == c.text
			c.mu.RUnlock()
			if same {
				continue
			}
			c.mu.Lock()
			c.text = s
			c.mu.Unlock()
			if c.onChange != nil && s != "" {
				c.onChange(s)
			}
		}
	}
}

// --- platform implementations -------------------------------------------

func readClipboard() (string, error) {
	name, args := clipboardReadCmd()
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func writeClipboard(s string) error {
	name, args := clipboardWriteCmd()
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

func clipboardReadCmd() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "pbpaste", nil
	case "windows":
		return "powershell", []string{
			"-NoProfile", "-NonInteractive", "-Command", "Get-Clipboard -Raw",
		}
	default:
		if _, err := exec.LookPath("xclip"); err == nil {
			return "xclip", []string{"-selection", "clipboard", "-o"}
		}
		if _, err := exec.LookPath("wl-paste"); err == nil {
			return "wl-paste", []string{"--no-newline"}
		}
		return "xsel", []string{"--clipboard", "--output"}
	}
}

func clipboardWriteCmd() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "pbcopy", nil
	case "windows":
		// clip.exe takes the text on stdin, which is far cheaper than a
		// PowerShell round trip.
		return "cmd", []string{"/c", "clip"}
	default:
		if _, err := exec.LookPath("xclip"); err == nil {
			return "xclip", []string{"-selection", "clipboard", "-i"}
		}
		if _, err := exec.LookPath("wl-copy"); err == nil {
			return "wl-copy", nil
		}
		return "xsel", []string{"--clipboard", "--input"}
	}
}
