package serve_test

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
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

	srv := &http.Server{Handler: demoBackend{App: a}.handler()}
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
	lang string
}

// The fixture is language-aware because it is where the screenshots come from.
// The pages ship bilingual, and an English page whose file list reads
// 屏幕录像-最终版.mov looks like a bug rather than a demo. The capture script
// asks for a language with ?lang=; the pages themselves still pick theirs from
// the browser, exactly as they do in production.
func (d demoBackend) withLang(lang string) demoBackend {
	d.lang = lang
	return d
}

func (d demoBackend) zh() bool { return d.lang == "zh" }

func demoTransfers(lang string) []model.Transfer {
	now := time.Now()
	if lang == "zh" {
		return []model.Transfer{
			{ID: "t3", Name: "屏幕录像-最终版.mov", Kind: "push", State: "running",
				Indeterminate: true, StartedAt: now.UnixMilli() - 41_000, At: 3, Size: 1_204_338_001},
			{ID: "t2", Name: "项目验收报告-终稿.pdf", Kind: "pull", State: "done",
				Detail: "从手机取回 · 共享目录", At: now.Unix() - 120, Size: 5_872_450},
			{ID: "t1", Name: "IMG_20261001_周日夜跑合影.jpg", Kind: "push", State: "done",
				Detail: "发送到手机 · Download", At: now.Unix() - 25, Size: 3_842_150},
		}
	}
	return []model.Transfer{
		{ID: "t3", Name: "Screen Recording - Final.mov", Kind: "push", State: "running",
			Indeterminate: true, StartedAt: now.UnixMilli() - 41_000, At: 3, Size: 1_204_338_001},
		{ID: "t2", Name: "Acceptance Report - Final.pdf", Kind: "pull", State: "done",
			Detail: "Retrieved from phone · shared folder", At: now.Unix() - 120, Size: 5_872_450},
		{ID: "t1", Name: "IMG_20261001_Sunday run group.jpg", Kind: "push", State: "done",
			Detail: "Sent to phone · Download", At: now.Unix() - 25, Size: 3_842_150},
	}
}

func (d demoBackend) State() model.State {
	st := d.App.State()
	st.ADBFound = true
	st.ADBPath = "/Users/demo/Library/Android/sdk/platform-tools/adb"
	st.Connected = true
	st.Device = "Pixel 9 Pro"
	st.Serial = "OP5CFBL1"
	st.StorageFree = "77G"
	st.Serving = true
	st.Port = 8765
	st.PhoneURL = "http://localhost:8765"
	st.Native = true
	st.Transfers = demoTransfers(d.lang)
	st.Error = ""
	return st
}

func (d demoBackend) ClipboardText() string {
	if d.zh() {
		return `部署步骤(今晚要用的):

1. make build && make app
2. 插线,手机选「传输文件」
3. 手机打开 http://localhost:8765
4. 上传验收材料到 共享目录`
	}
	return `Deploy steps (for tonight):

1. make build && make app
2. Plug in, pick "File transfer" on the phone
3. Open http://localhost:8765 on the phone
4. Upload the acceptance files to the shared folder`
}

func (d demoBackend) Note() (string, int64) {
	if d.zh() {
		return "会议改到 15:30,会议室 B7-02", time.Now().Unix()
	}
	return "Meeting moved to 15:30, room B7-02", time.Now().Unix()
}

func demoEntries(lang string) []model.Entry {
	now := time.Now().Unix()
	if lang == "zh" {
		return []model.Entry{
			{Name: "验收材料", Path: "/Users/demo/CableDrop/验收材料", Dir: true, ModTime: now - 3600},
			{Name: "项目截图", Path: "/Users/demo/CableDrop/项目截图", Dir: true, ModTime: now - 7200},
			{Name: "IMG_20261001_周日夜跑合影.jpg", Path: "/Users/demo/CableDrop/IMG.jpg", Size: 3_842_150, ModTime: now - 90},
			{Name: "部署手册-v3.pdf", Path: "/Users/demo/CableDrop/部署手册-v3.pdf", Size: 941_002, ModTime: now - 400},
			{Name: "屏幕录像-最终版.mov", Path: "/Users/demo/CableDrop/屏幕录像-最终版.mov", Size: 1_204_338_001, ModTime: now - 60},
		}
	}
	return []model.Entry{
		{Name: "Acceptance materials", Path: "/Users/demo/CableDrop/acceptance", Dir: true, ModTime: now - 3600},
		{Name: "Project screenshots", Path: "/Users/demo/CableDrop/screenshots", Dir: true, ModTime: now - 7200},
		{Name: "IMG_20261001_Sunday run group.jpg", Path: "/Users/demo/CableDrop/IMG.jpg", Size: 3_842_150, ModTime: now - 90},
		{Name: "Deployment guide-v3.pdf", Path: "/Users/demo/CableDrop/deployment-guide-v3.pdf", Size: 941_002, ModTime: now - 400},
		{Name: "Screen Recording - Final.mov", Path: "/Users/demo/CableDrop/screen-recording-final.mov", Size: 1_204_338_001, ModTime: now - 60},
	}
}

func demoDirs(lang string) []model.Entry {
	if lang == "zh" {
		return []model.Entry{
			{Name: "Download", Path: "/sdcard/Download", Dir: true},
			{Name: "相机", Path: "/sdcard/DCIM/Camera", Dir: true},
			{Name: "文档", Path: "/sdcard/Documents", Dir: true},
			{Name: "根目录", Path: "/sdcard", Dir: true},
		}
	}
	return []model.Entry{
		{Name: "Download", Path: "/sdcard/Download", Dir: true},
		{Name: "Camera", Path: "/sdcard/DCIM/Camera", Dir: true},
		{Name: "Documents", Path: "/sdcard/Documents", Dir: true},
		{Name: "Root", Path: "/sdcard", Dir: true},
	}
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The language-aware backend has to be built per request: /api/state,
		// /api/clipboard and /api/note all answer from it, and the handler that
		// serves them is otherwise fixed at construction time — so a language
		// chosen per request never reached the sample data.
		lang := demoLang(r)
		next := serve.NewHandler(d.withLang(lang), true)
		switch r.URL.Path {
		case "/api/shared":
			writeDemoJSON(w, map[string]any{
				"root": d.ServeDir(), "path": d.ServeDir(), "sub": "", "entries": demoEntries(lang),
			})
			return
		case "/api/device/dirs":
			writeDemoJSON(w, map[string]any{"dirs": demoDirs(lang)})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// demoLang picks the language for the fixture's sample data. The page's own
// fetches carry no query string, so Accept-Language is what actually decides
// it — the capture script forces that header, which is why an English
// screenshot no longer comes back listing 屏幕录像-最终版.mov. ?lang= stays as
// an explicit override, so the fixture can be driven by hand with curl.
func demoLang(r *http.Request) string {
	if q := r.URL.Query().Get("lang"); q != "" {
		return q
	}
	if c, err := r.Cookie("cabledrop.demo.lang"); err == nil && c.Value != "" {
		return c.Value
	}
	for _, tag := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(tag)), "zh") {
			return "zh"
		}
	}
	return "en"
}

var stopDevServer = make(chan struct{})
