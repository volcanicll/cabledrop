package picker

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// The system file picker is implemented here rather than through the toolkit's
// own dialog API.
//
// Wails v3's open-file dialog is unsafe to call from a request handler. Its
// implementation returns a channel that the caller ranges over, and the macOS
// side hands paths to that channel from the main thread over an unbuffered
// send, closing it only in a completion handler that may never run. A miss in
// its internal map is a panic rather than an error. Any of those leaves the
// handler goroutine waiting forever, and the wait happens behind a synchronous
// main-thread dispatch, so the panel stops responding too.
//
// Asking the platform's own picker (osascript, PowerShell, zenity) from a
// subprocess sidesteps all of it, and costs nothing we care about: the dialog
// is the same one the user already knows.

// errPickerUnavailable means the platform helper is missing, not that the user
// cancelled.
var errPickerUnavailable = errors.New("系统文件选择器不可用")

// PickFiles asks for one or more files. A cancelled dialog returns an empty
// slice and no error, so callers do not report a failure the user caused.
func PickFiles(prompt string) ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := runPicker("osascript", "-e", `
on run
	set theFiles to choose file with prompt "`+prompt+`" with multiple selections allowed
	set acc to ""
	repeat with f in theFiles
		set acc to acc & (POSIX path of f) & linefeed
	end repeat
	return acc
end run`)
		if err != nil {
			return nil, err
		}
		return splitLines(out), nil

	case "windows":
		out, err := runPicker("powershell", "-NoProfile", "-STA", "-Command", `
Add-Type -AssemblyName System.Windows.Forms | Out-Null
$d = New-Object System.Windows.Forms.OpenFileDialog
$d.Multiselect = $true
$d.Title = '`+prompt+`'
if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { $d.FileNames }`)
		if err != nil {
			return nil, err
		}
		return splitLines(out), nil

	default:
		out, err := runPicker("zenity", "--file-selection", "--multiple",
			"--separator=\n", "--title="+prompt)
		if err != nil {
			return nil, err
		}
		return splitLines(out), nil
	}
}

// PickDir asks for a single directory.
func PickDir(prompt string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := runPicker("osascript", "-e", `
on run
	set theFolder to choose folder with prompt "`+prompt+`"
	return POSIX path of theFolder
end run`)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(out), nil

	case "windows":
		out, err := runPicker("powershell", "-NoProfile", "-STA", "-Command", `
Add-Type -AssemblyName System.Windows.Forms | Out-Null
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = '`+prompt+`'
if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { $d.SelectedPath }`)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(out), nil

	default:
		out, err := runPicker("zenity", "--file-selection", "--directory",
			"--title="+prompt)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(out), nil
	}
}

// pickerNoise are stderr lines macOS emits for reasons that have nothing to
// do with the dialog -- notably IMKClient/IMKInputSession chatter, which every
// GUI helper produces on startup. Treating those as failures would report an
// error for a picker that worked.
var pickerNoise = []string{"imkclient", "imkinputsession"}

// meaningfulStderr drops blank lines and known-harmless chatter so what is
// left can be classified as a real failure.
func meaningfulStderr(s string) string {
	var keep []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		noisy := false
		lower := strings.ToLower(line)
		for _, n := range pickerNoise {
			if strings.Contains(lower, n) {
				noisy = true
				break
			}
		}
		if !noisy {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}

// runPicker runs the helper and treats "user pressed Cancel" as success with
// no output. Every shell here reports a cancellation as a non-zero exit, so
// the distinction has to be made on the message: reporting a cancel as an
// error would surface a scary alert for a normal action.
func runPicker(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return stdout.String(), nil
	}

	if errors.Is(err, exec.ErrNotFound) {
		return "", errPickerUnavailable
	}

	msg := strings.ToLower(meaningfulStderr(stderr.String()))
	for _, cancelled := range []string{"user canceled", "user cancelled", "(-128)", "-128"} {
		if strings.Contains(msg, cancelled) {
			return "", nil
		}
	}

	// Nothing but noise on stderr means the dialog was dismissed without a
	// result (or the helper was killed). That is a cancel in every way the
	// caller cares about, so don't invent an error the user cannot act on.
	if msg == "" {
		return "", nil
	}
	return "", errors.New(meaningfulStderr(stderr.String()))
}

// splitLines drops the empty trailing entry every one of these helpers leaves
// behind, and ignores blank lines.
func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
