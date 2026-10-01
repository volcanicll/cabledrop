// Package model holds the data types both the HTTP API and the UI speak.
//
// These are pure data with no dependencies, so every layer — device, serve,
// app, ui — can share them without wiring the whole program together.
package model

import "errors"

// Device is one Android device as `adb devices -l` reports it.
type Device struct {
	Serial  string `json:"serial"`
	Model   string `json:"model"`
	Product string `json:"product"`
}

// Name is what the UI shows: the human model name when adb reported one,
// otherwise the serial.
func (d Device) Name() string {
	if d.Model != "" {
		return d.Model
	}
	return d.Serial
}

// Entry is one file or directory, on either side of the cable.
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mtime"`
}

// Transfer is one file movement, shown in the panel's history.
type Transfer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`  // push | pull | upload
	State  string `json:"state"` // running | done | failed
	Detail string `json:"detail"`
	At     int64  `json:"at"`
}

// State is everything the UI needs in one payload.
type State struct {
	Platform    string     `json:"platform"`
	ADBFound    bool       `json:"adbFound"`
	ADBPath     string     `json:"adbPath"`
	Connected   bool       `json:"connected"`
	Device      string     `json:"device"`
	Serial      string     `json:"serial"`
	StorageFree string     `json:"storageFree"`
	ServeDir    string     `json:"serveDir"`
	Serving     bool       `json:"serving"`
	Port        int        `json:"port"`
	PhoneURL    string     `json:"phoneURL"`
	Transfers   []Transfer `json:"transfers"`
	Error       string     `json:"error,omitempty"`
	Native      bool       `json:"native"`
}

// ErrBadDevicePath is returned for a device path outside shared storage,
// including one that tries to climb out with "..".
//
// It lives in model because it is part of the contract between the device
// layer (which produces it) and serve (which maps it to a 403) — and serve
// deliberately depends on nothing below model and its Backend interface.
var ErrBadDevicePath = errors.New("路径必须在手机的共享存储内（/sdcard）")
