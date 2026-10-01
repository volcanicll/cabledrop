package main

import (
	"net"
	"os"
	"testing"
	"time"
)

// TestDevServer keeps the real HTTP handler up so the pages can be inspected
// and screenshotted in a browser while working on them. Skipped unless asked
// for, so plain `go test ./...` is unaffected:
//
//	USBBRIDGE_DEVSERVER=1 go test -run TestDevServer -timeout 1h ./...
//
// Serves the phone page on http://127.0.0.1:18765/ and the panel on /panel.
// No GUI, no tray, no adb: a bare App with the same handler both clients see.
func TestDevServer(t *testing.T) {
	if os.Getenv("USBBRIDGE_DEVSERVER") == "" {
		t.Skip("set USBBRIDGE_DEVSERVER=1 to run the in-test dev server")
	}

	a := NewApp()
	ln, err := net.Listen("tcp", "127.0.0.1:18765")
	if err != nil {
		t.Fatal(err)
	}
	a.clip.Start()

	srv := newHTTPServer(a, a.ServeDir(), true)
	go func() {
		_ = srv.Serve(ln)
	}()

	t.Logf("dev server on http://127.0.0.1:18765 (panel: /panel)")
	select {
	case <-time.After(time.Hour):
	case <-stopDevServer:
	}
}

var stopDevServer = make(chan struct{})
