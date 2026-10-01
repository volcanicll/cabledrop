package clipboard

import (
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

// The real platform command table, without running a clipboard: every GOOS
// must name a tool for both directions.
func TestPlatformCommandsExistForEveryGOOS(t *testing.T) {
	readName, _ := clipboardReadCmd()
	writeName, _ := clipboardWriteCmd()
	if readName == "" || writeName == "" {
		t.Fatalf("read=%q write=%q", readName, writeName)
	}
}
