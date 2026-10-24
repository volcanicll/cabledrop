package device

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/volcanicll/cabledrop/internal/model"
)

// shellQuote makes a path safe to interpolate into a device shell command.
//
// The shell on the device is busybox/toybox `sh`, so a single-quoted string
// with embedded quotes escaped as '\” is the portable form.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// deviceRoots are the only places on the phone this app will read or write.
//
// adb reaches the whole filesystem and these paths arrive from a web page, so
// without a check /api/device/pull will copy anything adb can read -- including
// /etc/passwd -- into the shared folder, which is then served back out.
// Restricting to shared storage also matches what the file browser claims to
// show.
var deviceRoots = []string{"/sdcard", "/storage"}

// CheckDevicePath reports whether a device path is one this app will touch.
func CheckDevicePath(p string) error {
	if p == "" || !strings.HasPrefix(p, "/") {
		return model.ErrBadDevicePath
	}
	// Clean first: "/sdcard/../../etc/passwd" has to be judged by where it
	// actually lands, not by how it reads.
	clean := path.Clean(p)
	for _, root := range deviceRoots {
		if clean == root || strings.HasPrefix(clean, root+"/") {
			return nil
		}
	}
	return model.ErrBadDevicePath
}

// ListDir lists a directory on the device.
//
// One shell invocation covers the whole directory: adb process spawn costs
// tens of milliseconds, so a per-file stat turns a 200-file folder into a
// ten-second wait.
func (a *ADB) ListDir(dir string) ([]model.Entry, error) {
	if dir == "" {
		dir = "/sdcard"
	}
	if err := CheckDevicePath(dir); err != nil {
		return nil, err
	}
	q := shellQuote(dir)
	// %f raw type, %s size, %Y mtime, %n name
	cmd := fmt.Sprintf(
		`for f in %s/*; do [ -e "$f" ] && stat -c '%%f|%%s|%%Y|%%n' "$f" 2>/dev/null; done`,
		q)

	out, err := a.Shell(25*time.Second, cmd)
	if err != nil {
		return nil, err
	}

	entries := make([]model.Entry, 0, 64)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			continue
		}
		rawType, sizeStr, mtimeStr, full := parts[0], parts[1], parts[2], parts[3]

		// An unexpanded glob comes back literally when the directory is empty.
		if strings.HasSuffix(full, "/*") {
			continue
		}
		name := baseName(full)
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}

		size, _ := strconv.ParseInt(sizeStr, 10, 64)
		mtime, _ := strconv.ParseInt(mtimeStr, 10, 64)

		// "81a4": the last four octal digits carry the file type.
		mode := rawType
		if len(mode) > 4 {
			mode = mode[len(mode)-4:]
		}
		isDir := strings.HasPrefix(mode, "4")
		isLink := strings.HasPrefix(mode, "12") || strings.ToLower(mode) == "a1ff"

		entries = append(entries, model.Entry{
			Name:    name,
			Path:    full,
			Dir:     isDir || isLink,
			Size:    size,
			ModTime: mtime,
		})
	}

	// Directories first, then newest first — the order a file browser wants.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return entries[i].ModTime > entries[j].ModTime
	})
	return entries, nil
}

// baseName returns everything after the last slash.
func baseName(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// PushFile copies a local file onto the device. Returns the device path.
//
// Resolves a missing destination, creates it and pushes in one call — the
// form single pushes use. Batches should resolve and create the directory
// once (DefaultUploadDir + EnsureDir) and then use PushPrepared per file:
// a batch of N files must not pay N times for the same adb round trips.
func (a *ADB) PushFile(local, remoteDir string) (string, error) {
	if remoteDir == "" {
		remoteDir = a.defaultUploadDir()
	}
	if err := CheckDevicePath(remoteDir); err != nil {
		return "", err
	}
	if err := a.EnsureDir(remoteDir); err != nil {
		return "", err
	}
	return a.PushPrepared(local, remoteDir)
}

// PushPrepared pushes into a destination directory that EnsureDir has
// already created and validated. It is the per-file half of a batch push.
func (a *ADB) PushPrepared(local, remoteDir string) (string, error) {
	if err := CheckDevicePath(remoteDir); err != nil {
		return "", err
	}
	// adb push wants a trailing slash to mean "into this directory".
	if !strings.HasSuffix(remoteDir, "/") {
		remoteDir += "/"
	}
	_, errOut, code, err := a.Run(30*time.Minute, "push", local, remoteDir)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("发送失败: %s", strings.TrimSpace(errOut))
	}
	return remoteDir + baseName(local), nil
}

// EnsureDir creates a directory on the device, rejecting paths outside the
// whitelist first. Batches call it once, before the per-file pushes.
func (a *ADB) EnsureDir(dir string) error {
	if err := CheckDevicePath(dir); err != nil {
		return err
	}
	_, err := a.Shell(10*time.Second, "mkdir -p "+shellQuote(dir))
	return err
}

// PullFile copies a device file into a local directory.
func (a *ADB) PullFile(remote, localDir string) (string, error) {
	if err := CheckDevicePath(remote); err != nil {
		return "", err
	}
	if localDir == "" {
		localDir = "."
	}
	_, errOut, code, err := a.Run(30*time.Minute, "pull", remote, localDir)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("接收失败: %s", strings.TrimSpace(errOut))
	}
	return filepath.Join(localDir, baseName(remote)), nil
}

// DeletePath removes a file or directory on the device.
func (a *ADB) DeletePath(remote string) error {
	if err := CheckDevicePath(remote); err != nil {
		return err
	}
	_, err := a.Shell(20*time.Second, "rm -rf "+shellQuote(remote))
	return err
}

// DeviceDirs are the places people actually put things, in the order worth
// offering. Only ones that exist on the device are returned.
//
// All six candidates are tested by one shell invocation: each adb process
// spawn costs tens of milliseconds, and this runs every time a file browser
// view opens, so six sequential calls were half a second of dead air.
func (a *ADB) DeviceDirs() []model.Entry {
	candidates := []struct{ name, path string }{
		{"Download", "/sdcard/Download"},
		{"相机", "/sdcard/DCIM/Camera"},
		{"截图", "/sdcard/Pictures/Screenshots"},
		{"图片", "/sdcard/Pictures"},
		{"文档", "/sdcard/Documents"},
		{"根目录", "/sdcard"},
	}
	var b strings.Builder
	for _, c := range candidates {
		// "name|path" on one line, exactly when the directory exists. Quoted
		// constants, no shell expansion — portable across toybox and mksh.
		b.WriteString("[ -d " + shellQuote(c.path) + " ] && echo " +
			shellQuote(c.name+"|"+c.path) + "; ")
	}
	b.WriteString("true")

	out, err := a.Shell(15*time.Second, b.String())
	if err != nil {
		return nil
	}
	entries := make([]model.Entry, 0, len(candidates))
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
		if len(parts) != 2 {
			continue
		}
		entries = append(entries, model.Entry{Name: parts[0], Path: parts[1], Dir: true})
	}
	return entries
}

// defaultUploadDir picks somewhere that exists to receive a pushed file.
// /sdcard/Documents is missing on many devices, and a push into a path that
// doesn't exist fails rather than creating it.
func (a *ADB) defaultUploadDir() string {
	for _, p := range []string{"/sdcard/Download", "/sdcard/Documents", "/sdcard"} {
		if _, err := a.Shell(8*time.Second, "test -d "+shellQuote(p)+" && echo yes"); err == nil {
			return p
		}
	}
	return "/sdcard"
}

// DeviceFileSize reports how many bytes a device file currently holds, or -1
// when it cannot be stat'ed. During a push the file grows in place, so
// sampling this every couple of seconds turns an indeterminate transfer into
// an honest percentage for the price of one shell call per sample.
func (a *ADB) DeviceFileSize(remote string) int64 {
	out, err := a.Shell(8*time.Second, "stat -c %s "+shellQuote(remote))
	if err != nil {
		return -1
	}
	n, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// FreeSpaceOnDevice reports the phone's free storage, for the status line.
func (a *ADB) FreeSpaceOnDevice() string {
	out, err := a.Shell(10*time.Second, "df -h /sdcard")
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 {
		return ""
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) >= 4 {
		return fields[3]
	}
	return ""
}

// Package-level forms operate on Default, the process-wide adb handle.

func ListDir(dir string) ([]model.Entry, error) { return Default.ListDir(dir) }

func PushFile(local, remoteDir string) (string, error) { return Default.PushFile(local, remoteDir) }

func PushPrepared(local, remoteDir string) (string, error) {
	return Default.PushPrepared(local, remoteDir)
}

func EnsureDir(dir string) error { return Default.EnsureDir(dir) }

func PullFile(remote, localDir string) (string, error) { return Default.PullFile(remote, localDir) }

func DeletePath(remote string) error { return Default.DeletePath(remote) }

func DeviceDirs() []model.Entry { return Default.DeviceDirs() }

func DeviceFileSize(remote string) int64 { return Default.DeviceFileSize(remote) }

func FreeSpaceOnDevice() string { return Default.FreeSpaceOnDevice() }

// DefaultUploadDir picks somewhere that exists to receive pushed files.
// Batches resolve this once, not once per file.
func DefaultUploadDir() string { return Default.defaultUploadDir() }
