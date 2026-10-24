package clipboard

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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
	png    []byte // the clipboard's image as PNG bytes; nil when there is none
	sum    string // sha256 of png, "" when there is none; the change fingerprint
	stopCh chan struct{}
	once   sync.Once

	// onChange is called with new text whenever the clipboard changes.
	onChange func(string)

	// onChangeAny fires when either the text or the image changed — the
	// change signal the event stream pushes to open pages.
	onChangeAny func()

	// read, readImage and write are the platform commands; interval how often
	// to poll. Tests replace them, so none of the behaviour above depends on a
	// real pasteboard being present.
	read      func() (string, error)
	readImage func() ([]byte, error)
	write     func(string) error
	interval  func() time.Duration

	// active gates polling. Every tick forks the platform's pasteboard tools
	// (on macOS one pbpaste plus one osascript), so an idle app must not
	// watch at all: polling only runs while someone can read the clipboard —
	// a phone page over the tunnel, or the panel's text view. False until
	// SetActive(true).
	active atomic.Bool
	wake   chan struct{}
}

func New() *Clipboard {
	return &Clipboard{
		read:      readClipboard,
		readImage: readClipboardImage,
		write:     writeClipboard,
		interval:  pollInterval,
	}
}

// Text returns the last known clipboard contents.
func (c *Clipboard) Text() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.text
}

// Image returns the clipboard's image as PNG bytes, or nil when the clipboard
// holds no image. The slice is replaced wholesale on every change and never
// mutated in place, so callers may keep it.
func (c *Clipboard) Image() []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.png
}

// ImageSum returns a fingerprint of the current image — sha256 hex, or "" when
// there is none. The phone page compares it to notice "the picture changed"
// without re-downloading it every poll.
func (c *Clipboard) ImageSum() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sum
}

// Set replaces the clipboard and remembers it, so the watcher doesn't report
// our own write back as a fresh copy.
func (c *Clipboard) Set(s string) error {
	if err := c.write(s); err != nil {
		return err
	}
	c.mu.Lock()
	c.text = s
	c.mu.Unlock()
	return nil
}

// SetOnChangeAny installs the change hook for either side of the clipboard.
// Call before Start.
func (c *Clipboard) SetOnChangeAny(f func()) {
	c.mu.Lock()
	c.onChangeAny = f
	c.mu.Unlock()
}

// SetActive turns polling on or off. Turning it on wakes the loop for an
// immediate sample, so a page that just connected sees the current clipboard
// without waiting out the next tick. False by default.
func (c *Clipboard) SetActive(on bool) {
	if c.active.Swap(on) == on {
		return
	}
	if !on {
		return
	}
	// Rising edge: skip the sample if the loop has not started yet (Start
	// seeds once itself); the non-blocking send means a shutdown race cannot
	// block the caller.
	if c.wake == nil {
		return
	}
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Start begins watching. Safe to call more than once.
func (c *Clipboard) Start() {
	c.once.Do(func() {
		c.stopCh = make(chan struct{})
		c.wake = make(chan struct{}, 1)
		// Seed once so the first page load has something to show.
		if s, err := c.read(); err == nil {
			c.mu.Lock()
			c.text = s
			c.mu.Unlock()
		}
		if png, err := c.readImage(); err == nil {
			c.observeImage(png)
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

// imageEvery is how many ticks pass between image reads. The image read is
// the expensive half on macOS — one osascript spawn — while the text read is
// one pbpaste. Halving the image cadence halves that cost; a screenshot
// still reaches an open page within two ticks.
const imageEvery = 2

func (c *Clipboard) watch() {
	t := time.NewTicker(c.interval())
	defer t.Stop()
	tick := 0
	for {
		select {
		case <-c.stopCh:
			return
		case <-c.wake:
			c.pollOnce()
		case <-t.C:
			// No consumer: touch nothing. The ticker keeps the loop alive but
			// idle — no subprocess, no I/O, just a channel receive.
			if !c.active.Load() {
				continue
			}
			tick++
			c.pollText()
			if tick%imageEvery == 0 {
				c.pollImage()
			}
		}
	}
}

// pollOnce samples the platform clipboard once, folding both the text and the
// image into state. Split out of watch so tests can drive a tick without
// waiting on the clock.
func (c *Clipboard) pollOnce() {
	c.pollText()
	c.pollImage()
}

// pollText samples the clipboard's text. An error means "nothing readable" —
// kept as-is rather than clearing, unlike the image side below.
func (c *Clipboard) pollText() {
	if s, err := c.read(); err == nil {
		c.observe(s)
	}
}

// pollImage samples the clipboard's image. An error from the image read means
// "no image on the clipboard" — that is how macOS answers when there is no PNG
// to fetch — so it clears rather than skips.
func (c *Clipboard) pollImage() {
	if png, err := c.readImage(); err == nil {
		c.observeImage(png)
	} else {
		c.observeImage(nil)
	}
}

// observe folds one clipboard sample into the state: errors are ignored, an
// unchanged value is dropped, and a genuinely new non-empty value is stored
// and announced.
func (c *Clipboard) observe(s string) {
	c.mu.RLock()
	same := s == c.text
	c.mu.RUnlock()
	if same {
		return
	}
	c.mu.Lock()
	c.text = s
	hook := c.onChangeAny
	c.mu.Unlock()
	if c.onChange != nil && s != "" {
		c.onChange(s)
	}
	if hook != nil {
		hook()
	}
}

// observeImage folds one image sample into the state, deduping by content
// fingerprint the way observe does for text.
func (c *Clipboard) observeImage(png []byte) {
	sum := ""
	if len(png) > 0 {
		h := sha256.Sum256(png)
		sum = hex.EncodeToString(h[:])
	}
	c.mu.RLock()
	same := sum == c.sum
	c.mu.RUnlock()
	if same {
		return
	}
	c.mu.Lock()
	c.png = png
	c.sum = sum
	hook := c.onChangeAny
	c.mu.Unlock()
	if hook != nil {
		hook()
	}
}

// --- platform implementations -------------------------------------------

// cmdEnv builds the environment for a pasteboard subprocess. It is the
// parent's environment plus a UTF-8 locale when the parent has no locale at
// all — the situation of a GUI app launched from Finder or Dock, whose
// environment carries no LANG/LC_*.
//
// Without it, macOS's pbcopy and pbpaste treat stdin/stdout as non-UTF-8:
// every multibyte character dies. Text sent from the phone landed as nothing
// on the desktop, and desktop text reached the phone as mojibake, while pure
// ASCII round-tripped fine. An environment that already names a locale —
// LANG, LC_ALL or LC_CTYPE, however unusual — is left exactly as it is.
//
// On other platforms the commands do not consult the locale, so the given
// environment passes through untouched.
func cmdEnv(parent []string) []string {
	if runtime.GOOS != "darwin" || hasLocale(parent) {
		return parent
	}
	return append(parent, "LC_CTYPE=UTF-8")
}

// hasLocale reports whether the environment already pins character
// classification somewhere: LANG, LC_ALL or LC_CTYPE.
func hasLocale(env []string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, "LC_ALL=") ||
			strings.HasPrefix(kv, "LC_CTYPE=") ||
			strings.HasPrefix(kv, "LANG=") {
			return true
		}
	}
	return false
}

func readClipboard() (string, error) {
	name, args := clipboardReadCmd()
	cmd := exec.Command(name, args...)
	cmd.Env = cmdEnv(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

// errNoImage reports "the clipboard holds no image (or none we can read)".
// It is the everyday answer, not a failure: most of the time the clipboard
// carries text, and the watcher clears the image on it.
var errNoImage = errors.New("剪贴板里没有图片")

// readClipboardImage fetches the clipboard's image as PNG bytes.
//
// macOS asks osascript for the pasteboard's PNG flavour: osascript prints the
// data as `«data PNGf89504E47…»` — hex text we decode — which keeps the
// command-subprocess, no-cgo design that lets the app cross-compile. The
// AppleScript errors out when there is no PNG, so the no-image case comes back
// as a plain error. Windows and Linux have no shelled equivalent yet: their
// clipboards answer "no image" until someone writes their reader.
func readClipboardImage() ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, errNoImage
	}
	cmd := exec.Command("osascript", "-e", `the clipboard as «class PNGf»`)
	cmd.Env = cmdEnv(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		return nil, errNoImage
	}
	return decodePNGfHex(string(out))
}

// decodePNGfHex parses osascript's rendering of the pasteboard's PNG data back
// into bytes. Anything that is not that one shape is a bug worth reporting,
// not an empty clipboard.
func decodePNGfHex(s string) ([]byte, error) {
	const prefix = "«data PNGf"
	i := strings.Index(s, prefix)
	if i < 0 {
		return nil, fmt.Errorf("osascript 输出里没有 PNG 数据: %.80q", s)
	}
	rest := s[i+len(prefix):]
	// "»" is U+00BB — two UTF-8 bytes (C2 BB). Search for the *string*, not a
	// byte: IndexByte would land on the trailing 0xBB and leave the leading
	// 0xC2 in the payload, which then fails to hex-decode.
	if j := strings.Index(rest, "»"); j >= 0 {
		rest = rest[:j]
	}
	png := make([]byte, len(rest)/2)
	if _, err := hex.Decode(png, []byte(rest)); err != nil {
		return nil, fmt.Errorf("PNG 数据解码失败: %w", err)
	}
	return png, nil
}

func writeClipboard(s string) error {
	name, args := clipboardWriteCmd()
	cmd := exec.Command(name, args...)
	cmd.Env = cmdEnv(os.Environ())
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
