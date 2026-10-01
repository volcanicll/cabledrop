package main

import (
	"errors"
	"testing"
)

func TestCheckDevicePath(t *testing.T) {
	ok := []string{
		"/sdcard",
		"/sdcard/",
		"/sdcard/Download",
		"/sdcard/DCIM/Camera/IMG_0001.jpg",
		"/storage/emulated/0/Documents",
		"/storage/1A2B-3C4D/backup",
		"/sdcard/Download/../Documents", // resolves inside
		"/sdcard/.",
	}
	for _, p := range ok {
		if err := CheckDevicePath(p); err != nil {
			t.Errorf("CheckDevicePath(%q) = %v, want nil", p, err)
		}
	}

	bad := []string{
		"",
		"relative/path",
		"/etc/passwd",
		"/data/data/com.example/db",
		"/system/build.prop",
		"/sdcard/../../etc/passwd", // climbs out before the root check
		"/sdcard/../data/data",     // lands outside
		"/sdcardx",                 // a prefix is not a parent
		"/sdcards",
		"/storagex/foo",
	}
	for _, p := range bad {
		err := CheckDevicePath(p)
		if !errors.Is(err, ErrBadDevicePath) {
			t.Errorf("CheckDevicePath(%q) = %v, want ErrBadDevicePath", p, err)
		}
	}
}
