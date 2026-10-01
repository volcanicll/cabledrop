package serve_test

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/volcanicll/cabledrop/internal/app"
	"github.com/volcanicll/cabledrop/internal/model"
	"github.com/volcanicll/cabledrop/internal/serve"
)

// TestDevServer keeps the real HTTP handler up so the pages can be inspected
// and screenshotted in a browser while working on them. Skipped unless asked
// for, so plain `go test ./...` is unaffected:
//
//	CABLEDROP_DEVSERVER=1 go test -run TestDevServer -timeout 1h ./internal/serve/
//
// Serves the phone page on http://127.0.0.1:18765/ and the panel on /panel.
// No GUI, no tray, no adb: a bare App with the same handler both clients see.
func TestDevServer(t *testing.T) {
	if os.Getenv("CABLEDROP_DEVSERVER") == "" {
		t.Skip("set CABLEDROP_DEVSERVER=1 to run the in-test dev server")
	}

	a := app.New()
	ln, err := net.Listen("tcp", "127.0.0.1:18765")
	if err != nil {
		t.Fatal(err)
	}
	a.StartClipboard()

	srv := serve.NewServer(a, true)
	go func() {
		_ = srv.Serve(ln)
	}()

	t.Logf("dev server on http://127.0.0.1:18765 (panel: /panel)")
	select {
	case <-time.After(time.Hour):
	case <-stopDevServer:
	}
}

// TestScreenshotServer serves the same pages with sample data, for README
// screenshots. It lives only in this test file: nothing demo-shaped ever
// reaches the production binary.
//
//	CABLEDROP_SHOTS=1 go test -run TestScreenshotServer -timeout 1h ./internal/serve/
//
// http://127.0.0.1:18766/ is the phone page, /panel the desktop panel.
func TestScreenshotServer(t *testing.T) {
	if os.Getenv("CABLEDROP_SHOTS") == "" {
		t.Skip("set CABLEDROP_SHOTS=1 to run the screenshot server")
	}

	a := app.New()
	ln, err := net.Listen("tcp", "127.0.0.1:18766")
	if err != nil {
		t.Fatal(err)
	}

	srv := &http.Server{Handler: demoBackend{a}.handler()}
	go func() {
		_ = srv.Serve(ln)
	}()

	t.Logf("screenshot server on http://127.0.0.1:18766 (panel: /panel)")
	select {
	case <-time.After(time.Hour):
	case <-stopDevServer:
	}
}

// demoBackend overlays sample answers on a real App: everything the pages
// render comes back populated, so screenshots look like a working session.
type demoBackend struct {
	*app.App
}

var demoTransfers = []model.Transfer{
	{ID: "t3", Name: "屏幕录像-最终版.mov", Kind: "push", State: "running",
		Detail: "", Indeterminate: true, StartedAt: time.Now().UnixMilli() - 41_000, At: 3},
	{ID: "t2", Name: "项目验收报告-终稿.pdf", Kind: "pull", State: "done",
		Detail: "已保存到共享目录", At: 2},
	{ID: "t1", Name: "IMG_20261001_周日夜跑合影.jpg", Kind: "push", State: "done",
		Detail: "已发送到手机 Download", At: 1},
}

func (d demoBackend) State() model.State {
	st := d.App.State()
	st.ADBFound = true
	st.ADBPath = "/Users/demo/Library/Android/sdk/platform-tools/adb"
	st.Connected = true
	st.Device = "PJF110"
	st.Serial = "OP5CFBL1"
	st.StorageFree = "77G"
	st.Serving = true
	st.Port = 8765
	st.PhoneURL = "http://localhost:8765"
	st.Native = true
	st.Transfers = demoTransfers
	st.Error = ""
	return st
}

func (d demoBackend) ClipboardText() string {
	return `部署步骤(今晚要用的):

1. make build && make app
2. 插线,手机选「传输文件」
3. 手机打开 http://localhost:8765
4. 上传验收材料到 共享目录`
}

func (d demoBackend) Note() (string, int64) {
	return "会议改到 15:30,会议室 B7-02", time.Now().Unix()
}

var demoEntries = []model.Entry{
	{Name: "验收材料", Path: "/Users/demo/CableDrop/验收材料", Dir: true, ModTime: time.Now().Unix() - 3600},
	{Name: "项目截图", Path: "/Users/demo/CableDrop/项目截图", Dir: true, ModTime: time.Now().Unix() - 7200},
	{Name: "IMG_20261001_周日夜跑合影.jpg", Path: "/Users/demo/CableDrop/IMG.jpg", Size: 3_842_150, ModTime: time.Now().Unix() - 90},
	{Name: "部署手册-v3.pdf", Path: "/Users/demo/CableDrop/部署手册-v3.pdf", Size: 941_002, ModTime: time.Now().Unix() - 400},
	{Name: "屏幕录像-最终版.mov", Path: "/Users/demo/CableDrop/屏幕录像-最终版.mov", Size: 1_204_338_001, ModTime: time.Now().Unix() - 60},
}

func (d demoBackend) ServeDir() string { return "/Users/demo/CableDrop" }

func writeDemoJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

// WrapHTTP lets the demo overlay ride on the real handler without reaching
// into unexported plumbing.
func (d demoBackend) handler() http.Handler {
	next := serve.NewHandler(d, true)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/shared":
			writeDemoJSON(w, map[string]any{
				"root": d.ServeDir(), "path": d.ServeDir(), "sub": "", "entries": demoEntries,
			})
			return
		case "/api/device/dirs":
			writeDemoJSON(w, map[string]any{"dirs": []model.Entry{
				{Name: "Download", Path: "/sdcard/Download", Dir: true},
				{Name: "相机", Path: "/sdcard/DCIM/Camera", Dir: true},
				{Name: "文档", Path: "/sdcard/Documents", Dir: true},
				{Name: "根目录", Path: "/sdcard", Dir: true},
			}})
			return
		}
		next.ServeHTTP(w, r)
	})
}

var stopDevServer = make(chan struct{})
