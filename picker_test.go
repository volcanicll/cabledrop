package main

import (
	"errors"
	"runtime"
	"testing"
)

func TestSplitLinesIgnoresBlankAndTrailingEntries(t *testing.T) {
	cases := map[string][]string{
		"":                           nil,
		"/tmp/a\n":                   {"/tmp/a"},
		"/tmp/a\n/tmp/b\n":           {"/tmp/a", "/tmp/b"},
		"\n/tmp/a\n\n/tmp/b\n\n":     {"/tmp/a", "/tmp/b"},
		"/tmp/with space/файл.txt\n": {"/tmp/with space/файл.txt"},
		"/tmp/a\r\n/tmp/b\r\n":       {"/tmp/a", "/tmp/b"},
	}
	for in, want := range cases {
		got := splitLines(in)
		if len(got) != len(want) {
			t.Fatalf("splitLines(%q) = %q, want %q", in, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("splitLines(%q)[%d] = %q, want %q", in, i, got[i], want[i])
			}
		}
	}
}

func TestRunPickerTreatsCancellationAsEmptyNotError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell to fake the helper's cancel output")
	}
	// Every picker reports Cancel as a non-zero exit. Surfacing that as an
	// error would put an alert in front of the user for a normal action.
	out, err := runPicker("sh", "-c", `echo "execution error: User canceled. (-128)" >&2; exit 1`)
	if err != nil {
		t.Fatalf("cancel returned an error: %v", err)
	}
	if out != "" {
		t.Fatalf("cancel returned output %q, want empty", out)
	}
}

func TestRunPickerReportsAMissingHelper(t *testing.T) {
	_, err := runPicker("cabledrop-definitely-not-installed")
	if !errors.Is(err, errPickerUnavailable) {
		t.Fatalf("got %v, want errPickerUnavailable", err)
	}
}

func TestRunPickerSurfacesARealFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	_, err := runPicker("sh", "-c", `echo "boom: something broke" >&2; exit 3`)
	if err == nil {
		t.Fatal("a genuine failure should be an error")
	}
	if err.Error() != "boom: something broke" {
		t.Fatalf("error message = %q, want the helper's stderr", err.Error())
	}
}

func TestRunPickerIgnoresStderrChatterForAWorkingDialog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	// macOS logs IMKClient/IMKInputSession noise on every GUI helper. Seeing
	// that alone must not turn a dismissed dialog into a reported failure.
	_, err := runPicker("sh", "-c",
		`echo "2026-10-01 22:57:08.061 osascript[1:2] +[IMKClient subclass]: chose IMKClient_Modern" >&2; exit 1`)
	if err != nil {
		t.Fatalf("stderr chatter was reported as an error: %v", err)
	}
}

func TestRunPickerKeepsTheRealMessageAndDropsTheNoise(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	_, err := runPicker("sh", "-c",
		`echo "+[IMKClient subclass]: chose IMKClient_Modern" >&2; echo "boom: something broke" >&2; exit 3`)
	if err == nil {
		t.Fatal("want an error")
	}
	if err.Error() != "boom: something broke" {
		t.Fatalf("error = %q, want only the meaningful line", err.Error())
	}
}

// The real AppleScript cancel signature. `error number -128` is exactly what
// osascript raises when the user presses Cancel on a choose dialog.
func TestAppleScriptCancelIsNotAnError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("osascript is macOS-only")
	}
	out, err := runPicker("osascript", "-e", "error number -128")
	if err != nil {
		t.Fatalf("a real osascript cancel returned an error: %v", err)
	}
	if out != "" {
		t.Fatalf("got output %q, want empty", out)
	}
}

// The dialog itself cannot be automated, but the AppleScript plumbing can:
// this runs the same code path against a script that returns fixed paths, so a
// quoting or parsing mistake fails here instead of in the user's face.
func TestAppleScriptPickerPlumbing(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("osascript is macOS-only")
	}
	out, err := runPicker("osascript", "-e",
		`return "/tmp/a b.txt" & linefeed & "/tmp/файл.txt" & linefeed`)
	if err != nil {
		t.Fatalf("runPicker: %v", err)
	}
	got := splitLines(out)
	want := []string{"/tmp/a b.txt", "/tmp/файл.txt"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}
