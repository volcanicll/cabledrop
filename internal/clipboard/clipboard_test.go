package clipboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"runtime"
	"testing"
	"time"
)

// newTestClipboard returns a clipboard whose platform commands answer from a
// script: reads come from *reads, writes are recorded.
func newTestClipboard() (*Clipboard, *[]string, *string) {
	writes := []string{}
	current := ""
	c := New()
	c.read = func() (string, error) { return current, nil }
	c.write = func(s string) error { writes = append(writes, s); current = s; return nil }
	c.interval = func() time.Duration { return time.Hour } // no ticking in tests
	return c, &writes, &current
}

func TestSetWritesAndRemembers(t *testing.T) {
	c, writes, _ := newTestClipboard()
	if err := c.Set("你好"); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0] != "你好" {
		t.Fatalf("writes = %v", *writes)
	}
	if c.Text() != "你好" {
		t.Fatalf("Text() = %q", c.Text())
	}
}

func TestObserveDedupesAndAnnounces(t *testing.T) {
	c, _, current := newTestClipboard()
	var announced []string
	c.onChange = func(s string) { announced = append(announced, s) }

	// Same value twice: announced once, the duplicate dropped.
	*current = "abc"
	c.observe("abc")
	c.observe("abc")
	if len(announced) != 1 || announced[0] != "abc" {
		t.Fatalf("announced = %v", announced)
	}
	if c.Text() != "abc" {
		t.Fatalf("text = %q", c.Text())
	}

	// A genuinely new value is announced.
	*current = "def"
	c.observe("def")
	if len(announced) != 2 || announced[1] != "def" {
		t.Fatalf("announced = %v", announced)
	}

	// An empty clipboard never announces, but its state is still tracked.
	*current = ""
	c.observe("")
	if len(announced) != 2 {
		t.Fatalf("empty sample was announced: %v", announced)
	}
	if c.Text() != "" {
		t.Fatalf("text = %q, want empty", c.Text())
	}
}

func TestObserveErrorsAreDropped(t *testing.T) {
	// Errors come through read(); observe only ever sees values, but the
	// watch loop must not panic on them. This pins the loop's contract.
	c, _, _ := newTestClipboard()
	c.read = func() (string, error) { return "", errors.New("no pasteboard") }
	c.Start()
	defer c.Stop()
	// Nothing to assert beyond "did not hang or panic": the poll interval is
	// an hour, so the loop sits idle.
}

func TestPollIntervalIsLongerOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		if pollInterval() != 2*time.Second {
			t.Fatalf("pollInterval = %v", pollInterval())
		}
		return
	}
	if pollInterval() != 1*time.Second {
		t.Fatalf("pollInterval = %v", pollInterval())
	}
}

// newImageTestClipboard returns a clipboard whose text and image reads answer
// from the given vars, so a test can walk the clipboard through the three
// states it can be in: carrying an image, carrying only text, carrying nothing.
func newImageTestClipboard() (*Clipboard, *[]byte) {
	var png []byte
	c := New()
	c.read = func() (string, error) { return "", nil }
	c.readImage = func() ([]byte, error) {
		if png == nil {
			return nil, errNoImage
		}
		return png, nil
	}
	c.interval = func() time.Duration { return time.Hour }
	return c, &png
}

// The three clipboard states, driven through the same observe path the watch
// loop uses: an image replaces nothing and vice versa, and text-only leaves
// the image side empty.
func TestImageFollowsTheThreeClipboardStates(t *testing.T) {
	c, png := newImageTestClipboard()

	// Neither text nor image: nothing to serve.
	c.observeImage(nil)
	if c.Image() != nil || c.ImageSum() != "" {
		t.Fatalf("empty clipboard: image=%v sum=%q", c.Image() != nil, c.ImageSum())
	}

	// Text only: the image side stays empty while the text works as before.
	if err := c.Set("你好"); err != nil {
		t.Fatal(err)
	}
	c.observeImage(nil)
	if c.Image() != nil || c.ImageSum() != "" {
		t.Fatalf("text-only clipboard: image=%v sum=%q", c.Image() != nil, c.ImageSum())
	}

	// An image: bytes round-trip and the fingerprint is the content's sha256.
	sample := []byte("89504E47…png bytes…")
	*png = sample
	c.observeImage(sample)
	if string(c.Image()) != string(sample) {
		t.Fatal("Image() did not round-trip the bytes")
	}
	want := sha256.Sum256(sample)
	if c.ImageSum() != hex.EncodeToString(want[:]) {
		t.Fatalf("sum = %q", c.ImageSum())
	}

	// Copying text over the image clears the image side.
	c.observeImage(nil)
	if c.Image() != nil || c.ImageSum() != "" {
		t.Fatalf("image survived a text copy: sum=%q", c.ImageSum())
	}
	if c.Text() != "你好" {
		t.Fatalf("text = %q", c.Text())
	}
}

func TestImageObservationDedupesByContent(t *testing.T) {
	c, png := newImageTestClipboard()
	sample := []byte("png-bytes")
	*png = sample
	c.observeImage(sample)
	c.observeImage(sample) // same content again: no churn worth noticing, but must stay correct
	if string(c.Image()) != string(sample) {
		t.Fatal("re-observation lost the image")
	}

	// A different image of the same length must still register as a change.
	other := []byte("png-bytEz")
	*png = other
	c.observeImage(other)
	if string(c.Image()) != string(other) {
		t.Fatal("a changed image was dropped as a duplicate")
	}
}

func TestWatchClearsTheImageWhenTheClipboardLosesIt(t *testing.T) {
	// The watch loop treats an image-read error as "no image" — macOS answers
	// that way whenever there is no PNG — so a clipboard that goes from image
	// to text must end up image-free after one poll.
	var png []byte
	c := New()
	c.read = func() (string, error) { return "some text", nil }
	c.readImage = func() ([]byte, error) {
		if png == nil {
			return nil, errNoImage
		}
		return png, nil
	}
	c.interval = func() time.Duration { return time.Hour }

	png = []byte("png-bytes")
	c.pollOnce()
	if c.Image() == nil || c.ImageSum() == "" {
		t.Fatalf("image not picked up: sum=%q", c.ImageSum())
	}

	png = nil
	c.pollOnce()
	if c.Image() != nil || c.ImageSum() != "" {
		t.Fatalf("image survived an empty clipboard: sum=%q", c.ImageSum())
	}
	if c.Text() != "some text" {
		t.Fatalf("text = %q", c.Text())
	}
}

// The real platform command table, without running a clipboard: every GOOS
// must name a tool for both directions.
func TestPlatformCommandsExistForEveryGOOS(t *testing.T) {
	readName, _ := clipboardReadCmd()
	writeName, _ := clipboardWriteCmd()
	if readName == "" || writeName == "" {
		t.Fatalf("read=%q write=%q", readName, writeName)
	}
}

// decodePNGfHex is the one piece of the image path that does not go through a
// fake, so it is the piece that can silently rot. osascript renders the
// pasteboard's PNG as `«data PNGf<hex>»`; the closing » is U+00BB, two UTF-8
// bytes, so a byte-wise search for it finds the trailing 0xBB and leaves the
// leading 0xC2 stuck to the hex payload — which hex.Decode then rejects. That
// made every image read fail, and because a failed read means "no image on the
// clipboard", it looked exactly like an empty clipboard.
func TestDecodePNGfHexHandlesTheMultibyteTerminator(t *testing.T) {
	want := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0xFF}
	out := "«data PNGf" + hex.EncodeToString(want) + "»\n"

	got, err := decodePNGfHex(out)
	if err != nil {
		t.Fatalf("decodePNGfHex(%q) = %v, want no error", out, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded % X, want % X", got, want)
	}
}

// The terminator may be absent (a truncated render), and the bytes before the
// marker are osascript's own noise. Both must still decode.
func TestDecodePNGfHexToleratesNoiseAndAMissingTerminator(t *testing.T) {
	want := []byte{0x01, 0x02, 0x03}
	got, err := decodePNGfHex("«data PNGf" + hex.EncodeToString(want))
	if err != nil {
		t.Fatalf("no terminator: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded % X, want % X", got, want)
	}
	if _, err := decodePNGfHex("no marker here"); err == nil {
		t.Fatal("expected an error when the marker is absent")
	}
}
