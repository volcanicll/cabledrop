package main

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Entry is one file or directory, on either side of the cable.
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mtime"`
}

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

// ErrBadDevicePath is returned for a device path outside shared storage,
// including one that tries to climb out with "..".
var ErrBadDevicePath = errors.New("路径必须在手机的共享存储内（/sdcard）")

// CheckDevicePath reports whether a device path is one this app will touch.
func CheckDevicePath(p string) error {
	if p == "" || !strings.HasPrefix(p, "/") {
		return ErrBadDevicePath
	}
	// Clean first: "/sdcard/../../etc/passwd" has to be judged by where it
	// actually lands, not by how it reads.
	clean := path.Clean(p)
	for _, root := range deviceRoots {
		if clean == root || strings.HasPrefix(clean, root+"/") {
			return nil
		}
	}
	return ErrBadDevicePath
}

// ListDir lists a directory on the device.
//
// One shell invocation covers the whole directory: adb process spawn costs
// tens of milliseconds, so a per-file stat turns a 200-file folder into a
// ten-second wait.
func ListDir(dir string) ([]Entry, error) {
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

	out, err := adb.Shell(25*time.Second, cmd)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, 64)
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

		entries = append(entries, Entry{
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

// parentDir returns the containing directory, or "" at the root.
func parentDir(p string) string {
	p = strings.TrimRight(p, "/")
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return ""
	}
	return p[:i]
}

// PushFile copies a local file onto the device. Returns the device path.
func PushFile(local, remoteDir string) (string, error) {
	if remoteDir == "" {
		remoteDir = defaultUploadDir()
	}
	if err := CheckDevicePath(remoteDir); err != nil {
		return "", err
	}
	// adb push wants a trailing slash to mean "into this directory".
	if !strings.HasSuffix(remoteDir, "/") {
		remoteDir += "/"
	}
	// A missing destination directory fails the push outright.
	if _, err := adb.Shell(10*time.Second, "mkdir -p "+shellQuote(remoteDir)); err != nil {
		return "", err
	}
	_, errOut, code, err := adb.Run(30*time.Minute, "push", local, remoteDir)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("发送失败: %s", strings.TrimSpace(errOut))
	}
	return remoteDir + baseName(local), nil
}

// PullFile copies a device file into a local directory.
func PullFile(remote, localDir string) (string, error) {
	if err := CheckDevicePath(remote); err != nil {
		return "", err
	}
	if localDir == "" {
		localDir = "."
	}
	_, errOut, code, err := adb.Run(30*time.Minute, "pull", remote, localDir)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("接收失败: %s", strings.TrimSpace(errOut))
	}
	return filepath.Join(localDir, baseName(remote)), nil
}

// DeletePath removes a file or directory on the device.
func DeletePath(remote string) error {
	if err := CheckDevicePath(remote); err != nil {
		return err
	}
	_, err := adb.Shell(20*time.Second, "rm -rf "+shellQuote(remote))
	return err
}

// DeviceDirs are the places people actually put things, in the order worth
// offering. Only ones that exist on the device are returned.
func DeviceDirs() []Entry {
	candidates := []struct{ name, path string }{
		{"Download", "/sdcard/Download"},
		{"相机", "/sdcard/DCIM/Camera"},
		{"截图", "/sdcard/Pictures/Screenshots"},
		{"图片", "/sdcard/Pictures"},
		{"文档", "/sdcard/Documents"},
		{"根目录", "/sdcard"},
	}
	out := make([]Entry, 0, len(candidates))
	for _, c := range candidates {
		if _, err := adb.Shell(8*time.Second, "test -d "+shellQuote(c.path)+" && echo yes"); err == nil {
			out = append(out, Entry{Name: c.name, Path: c.path, Dir: true})
		}
	}
	return out
}

// defaultUploadDir picks somewhere that exists to receive a pushed file.
// /sdcard/Documents is missing on many devices, and a push into a path that
// doesn't exist fails rather than creating it.
func defaultUploadDir() string {
	for _, p := range []string{"/sdcard/Download", "/sdcard/Documents", "/sdcard"} {
		if _, err := adb.Shell(8*time.Second, "test -d "+shellQuote(p)+" && echo yes"); err == nil {
			return p
		}
	}
	return "/sdcard"
}

// FreeSpaceOnDevice reports the phone's free storage, for the status line.
func FreeSpaceOnDevice() string {
	out, err := adb.Shell(10*time.Second, "df -h /sdcard")
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
