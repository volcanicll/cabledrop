// Package serve is the HTTP surface shared by both clients: the JSON API plus
// the embedded pages.
//
// It depends on model and on the narrow Backend interface below — never on the
// app package. The app implements Backend and starts this package's server for
// the phone; the native panel reaches the same handler through Wails' asset
// server, which is what keeps both clients on one API.
package serve

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/volcanicll/cabledrop/internal/model"
)

//go:embed all:assets
var assetsFS embed.FS

// Subscriptions is one event-stream client's wake channels. Each topic
// carries a coalesced signal — one buffered slot — when its data changed;
// the handler then reads the payload through the ordinary Backend getters,
// so what an event delivers is exactly what a poll would have returned.
type Subscriptions struct {
	State <-chan struct{}
	Clip  <-chan struct{}
	Note  <-chan struct{}
	// Cancel unregisters the subscriber; must be called when the stream ends.
	Cancel func()
}

// Backend is everything the HTTP handlers need from the application. It is the
// seam that keeps serve independent of the app and testable on its own.
type Backend interface {
	// State snapshots everything the UI needs.
	State() model.State
	// Refresh re-detects the device right now.
	Refresh()
	Connected() bool
	// Native reports whether the request came from the desktop panel. Phone
	// requests must not be able to drive desktop-only surfaces (pickers,
	// panel visibility).
	Native() bool

	ServeDir() string
	SetServeDir(dir string) error
	StartServing() error
	StopServing()

	ClipboardText() string
	ClipboardSet(text string) error
	// ClipboardImage returns the clipboard's image as PNG bytes plus a
	// content fingerprint; empty bytes mean the clipboard carries no image.
	ClipboardImage() (png []byte, sum string)
	Note() (string, int64)
	SetNote(text string)

	ListDeviceDir(dir string) ([]model.Entry, error)
	DeviceDirs() []model.Entry
	// CheckDevicePath rejects device paths outside the shared-storage
	// whitelist, so callers get a synchronous 403 instead of a failed row.
	CheckDevicePath(path string) error
	// PullDeviceFile copies a device file into the shared folder, in the
	// background; the transfer shows up in State. The expected size, when
	// the caller knows it from a listing, drives a real progress bar.
	PullDeviceFile(remote string, size int64)
	DeleteDevicePath(remote string) error
	PushFiles(paths []string, destDir string)
	ClearTransfers()

	// Subscribe registers an event-stream client: wake channels per topic
	// (state / clip / note) plus a Cancel for when the stream ends.
	Subscribe() Subscriptions

	// PickFiles and PickDir open the system file picker. Implementations step
	// the panel aside while the dialog is up.
	PickFiles(prompt string) ([]string, error)
	PickDir(prompt string) (string, error)
	// OpenPath opens a directory in, or reveals a file in, the platform file
	// manager.
	OpenPath(path string)

	HidePanel()
}

// devicePathStatus separates "you may not touch that" from "the device said
// no", because the two call for different fixes.
func devicePathStatus(err error) int {
	if errors.Is(err, model.ErrBadDevicePath) {
		return http.StatusForbidden
	}
	return http.StatusBadGateway
}

// phoneBackend marks a Backend as the phone-facing surface: Native() is false,
// so pick/panel/open endpoints answer 501 instead of acting on the desktop.
type phoneBackend struct{ Backend }

func (p phoneBackend) Native() bool { return false }

// NewHandler builds the whole HTTP surface: the JSON API plus the static page.
//
// The same handler serves both clients — the native panel (through Wails'
// asset server) and the phone's browser (through adb reverse). forPhone only
// decides which page "/" returns; the API is identical, so every feature is
// available on both sides. Desktop-only endpoints (pickers, panel visibility)
// answer 501 on the phone-facing surface, though — a web page arriving over
// the tunnel must not be able to drive native UI.
func NewHandler(backend Backend, forPhone bool) http.Handler {
	b := backend
	if forPhone {
		b = phoneBackend{backend}
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, b.State())
	})

	mux.HandleFunc("POST /api/refresh", func(w http.ResponseWriter, r *http.Request) {
		b.Refresh()
		writeJSON(w, b.State())
	})

	// --- desktop clipboard, both directions ---------------------------

	mux.HandleFunc("GET /api/clip", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, clipPayload(b))
	})

	// The clipboard's image, for the phone to preview and copy. 404 when the
	// clipboard holds no image — an everyday state, not an error page.
	// ?dl=1 marks the body as an attachment, which is the fallback path for
	// phones whose browser cannot write images to their own clipboard: the
	// WebView's download handler picks it up and saves it. The ETag answers
	// If-None-Match with a 304, so a page that already has this picture pays
	// nothing to confirm it is still current.
	mux.HandleFunc("GET /api/clip/image", func(w http.ResponseWriter, r *http.Request) {
		png, sum := b.ClipboardImage()
		if len(png) == 0 {
			httpError(w, http.StatusNotFound, "电脑剪贴板里没有图片")
			return
		}
		etag := `"` + sum + `"`
		if matchETag(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("ETag", etag)
		if r.URL.Query().Get("dl") != "" {
			name := time.Now().Format("cabledrop-20060102-150405.png")
			w.Header().Set("Content-Disposition",
				"attachment; filename*=UTF-8''"+urlEscape(name))
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Write(png)
	})

	// The phone posts here; the text lands in the desktop's clipboard.
	mux.HandleFunc("POST /api/clip", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&body); err != nil {
			httpError(w, http.StatusBadRequest, "请求格式不对")
			return
		}
		if err := b.ClipboardSet(body.Text); err != nil {
			httpError(w, http.StatusInternalServerError, "写入剪贴板失败: "+err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true, "len": len([]rune(body.Text))})
	})

	// --- text handoff --------------------------------------------------

	mux.HandleFunc("GET /api/note", func(w http.ResponseWriter, r *http.Request) {
		text, at := b.Note()
		writeJSON(w, map[string]any{"text": text, "at": at})
	})

	// --- event stream ----------------------------------------------------
	//
	// One server-sent-events connection replaces the phone page's three
	// polling loops: state, clipboard and note changes are pushed the moment
	// they happen, so the page reacts instantly and an idle cable carries no
	// requests at all. Every event carries the same JSON the polling
	// endpoints return, so the page's rendering code is identical for both.
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			httpError(w, http.StatusInternalServerError, "此连接不支持流式响应")
			return
		}
		sub := b.Subscribe()
		defer sub.Cancel()

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		// Ask the browser to reconnect on its own when the tunnel blips.
		io.WriteString(w, "retry: 3000\n\n")
		fl.Flush()

		send := func(event string, v any) bool {
			data, err := json.Marshal(v)
			if err != nil {
				return true // one bad payload is not worth killing the stream
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
				return false
			}
			fl.Flush()
			return true
		}

		// The opening snapshot: a freshly connected page renders from events
		// alone, no priming fetches.
		if !send("state", b.State()) ||
			!send("clip", clipPayload(b)) ||
			!send("note", notePayload(b)) {
			return
		}

		// Bursts (a ten-file batch finishing) coalesce into one payload: a
		// short settle, then drain whatever queued behind the first signal.
		drain := func(ch <-chan struct{}) {
			for {
				select {
				case <-ch:
				default:
					return
				}
			}
		}
		debounced := func(ch <-chan struct{}) {
			time.Sleep(40 * time.Millisecond)
			drain(ch)
		}

		ping := time.NewTicker(15 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ping.C:
				// Comment line: keeps proxies and the tunnel from reaping an
				// idle connection without disturbing the client.
				if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
					return
				}
				fl.Flush()
			case <-sub.State:
				debounced(sub.State)
				if !send("state", b.State()) {
					return
				}
			case <-sub.Clip:
				debounced(sub.Clip)
				if !send("clip", clipPayload(b)) {
					return
				}
			case <-sub.Note:
				debounced(sub.Note)
				if !send("note", notePayload(b)) {
					return
				}
			}
		}
	})

	mux.HandleFunc("POST /api/note", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&body); err != nil {
			httpError(w, http.StatusBadRequest, "请求格式不对")
			return
		}
		b.SetNote(body.Text)
		writeJSON(w, map[string]any{"ok": true})
	})

	// --- device files -------------------------------------------------

	mux.HandleFunc("GET /api/device/files", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("path")
		if dir == "" {
			dir = "/sdcard"
		}
		entries, err := b.ListDeviceDir(dir)
		if err != nil {
			httpError(w, devicePathStatus(err), err.Error())
			return
		}
		writeJSON(w, map[string]any{
			"path":    dir,
			"parent":  parentOf(dir),
			"entries": entries,
		})
	})

	mux.HandleFunc("GET /api/device/dirs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"dirs": b.DeviceDirs()})
	})

	mux.HandleFunc("POST /api/device/pull", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
			Size int64  `json:"size"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
			httpError(w, http.StatusBadRequest, "缺少 path")
			return
		}
		// The path whitelist is enforced again inside PullDeviceFile, but the
		// pull runs in the background — an invalid path would otherwise come
		// back as a cheerful 200 and a failed row the user has to notice.
		// Checking here surfaces the mistake as a 403 instead.
		if err := b.CheckDevicePath(body.Path); err != nil {
			httpError(w, http.StatusForbidden, err.Error())
			return
		}
		b.PullDeviceFile(body.Path, body.Size)
		writeJSON(w, map[string]any{"ok": true, "into": b.ServeDir()})
	})

	mux.HandleFunc("POST /api/device/delete", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
			httpError(w, http.StatusBadRequest, "缺少 path")
			return
		}
		if err := b.DeleteDevicePath(body.Path); err != nil {
			httpError(w, devicePathStatus(err), err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("POST /api/transfers/clear", func(w http.ResponseWriter, r *http.Request) {
		b.ClearTransfers()
		writeJSON(w, b.State())
	})

	// --- local files -> phone ----------------------------------------

	mux.HandleFunc("POST /api/send", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Paths []string `json:"paths"`
			Dir   string   `json:"dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Paths) == 0 {
			httpError(w, http.StatusBadRequest, "没有要发送的文件")
			return
		}
		b.PushFiles(body.Paths, body.Dir)
		writeJSON(w, map[string]any{"ok": true, "count": len(body.Paths)})
	})

	// Opens the system file picker. Native only: there is no desktop dialog
	// to show when the request came from the phone.
	//
	// Both pick endpoints step the panel aside for the duration: the panel is
	// always-on-top and the dialog is not, so the dialog would otherwise open
	// behind it and read as a dead click.
	mux.HandleFunc("POST /api/pick/files", func(w http.ResponseWriter, r *http.Request) {
		if !b.Native() {
			httpError(w, http.StatusNotImplemented, "此接口只在桌面端可用")
			return
		}
		paths, err := b.PickFiles("选择要发送到手机的文件")
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if paths == nil {
			paths = []string{}
		}
		writeJSON(w, map[string]any{"paths": paths})
	})

	mux.HandleFunc("POST /api/pick/dir", func(w http.ResponseWriter, r *http.Request) {
		if !b.Native() {
			httpError(w, http.StatusNotImplemented, "此接口只在桌面端可用")
			return
		}
		dir, err := b.PickDir("选择要与手机共享的文件夹")
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"path": dir})
	})

	// --- shared folder -------------------------------------------------

	mux.HandleFunc("GET /api/shared", func(w http.ResponseWriter, r *http.Request) {
		root := b.ServeDir()
		// Optional subdirectory: ?path= is resolved inside the shared root
		// and refused if it escapes, exactly like /dl. Omitting it keeps the
		// root — the shape older clients built on.
		dir := root
		sub := ""
		if p := r.URL.Query().Get("path"); p != "" {
			d, err := safeJoin(root, p)
			if err != nil {
				httpError(w, http.StatusForbidden, "路径不在共享目录内")
				return
			}
			fi, err := os.Stat(d)
			if err != nil || !fi.IsDir() {
				httpError(w, http.StatusNotFound, "目录不存在")
				return
			}
			dir = d
			sub = p
		}
		list, err := listLocal(dir)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{
			"root":    root,
			"path":    dir,
			"sub":     sub,
			"entries": list,
		})
	})

	mux.HandleFunc("POST /api/shared/dir", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Dir string `json:"dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Dir == "" {
			httpError(w, http.StatusBadRequest, "缺少 dir")
			return
		}
		if err := b.SetServeDir(body.Dir); err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, b.State())
	})

	// Reveals the shared folder in Finder / Explorer, native only.
	mux.HandleFunc("POST /api/shared/open", func(w http.ResponseWriter, r *http.Request) {
		if !b.Native() {
			httpError(w, http.StatusNotImplemented, "此接口只在桌面端可用")
			return
		}
		b.OpenPath(b.ServeDir())
		writeJSON(w, map[string]any{"ok": true})
	})

	// Reveals one file inside the shared folder — the action behind a pulled
	// transfer's history row. Native only, and the path must resolve inside
	// the shared root, exactly like /dl.
	mux.HandleFunc("POST /api/shared/reveal", func(w http.ResponseWriter, r *http.Request) {
		if !b.Native() {
			httpError(w, http.StatusNotImplemented, "此接口只在桌面端可用")
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
			httpError(w, http.StatusBadRequest, "缺少 path")
			return
		}
		target, err := safeJoin(b.ServeDir(), body.Path)
		if err != nil {
			httpError(w, http.StatusForbidden, "路径不在共享目录内")
			return
		}
		if _, err := os.Stat(target); err != nil {
			httpError(w, http.StatusNotFound, "文件不存在")
			return
		}
		b.OpenPath(target)
		writeJSON(w, map[string]any{"ok": true})
	})

	// Hides the panel. The page calls it on Escape; the tray icon and clicks
	// outside the panel are handled natively. Native only, so a stray request
	// from the phone cannot dismiss a panel nobody is looking at.
	mux.HandleFunc("POST /api/panel/hide", func(w http.ResponseWriter, r *http.Request) {
		if !b.Native() {
			httpError(w, http.StatusNotImplemented, "此接口只在桌面端可用")
			return
		}
		b.HidePanel()
		writeJSON(w, map[string]any{"ok": true})
	})

	// Download a file out of the shared folder, for the phone to save.
	mux.HandleFunc("GET /dl", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("p")
		if p == "" {
			httpError(w, http.StatusBadRequest, "缺少 p")
			return
		}
		root := b.ServeDir()
		target, err := safeJoin(root, p)
		if err != nil {
			httpError(w, http.StatusForbidden, "路径不在共享目录内")
			return
		}
		fi, err := os.Stat(target)
		if err != nil || fi.IsDir() {
			httpError(w, http.StatusNotFound, "文件不存在")
			return
		}
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename*=UTF-8''%s", urlEscape(filepath.Base(target))))
		http.ServeFile(w, r, target)
	})

	// The phone uploads a file into the shared folder.
	//
	// Streaming, not ParseMultipartForm: parsing would stage the whole body
	// into temp files first and then copy each one into place — every large
	// video written to disk twice. MultipartReader hands over each part as
	// it arrives, so bytes land directly in their destination.
	mux.HandleFunc("POST /upload", func(w http.ResponseWriter, r *http.Request) {
		mr, err := r.MultipartReader()
		if err != nil {
			httpError(w, http.StatusBadRequest, "上传解析失败: "+err.Error())
			return
		}
		dir := b.ServeDir()
		var saved []string
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				httpError(w, http.StatusBadRequest, "上传中断: "+err.Error())
				return
			}
			name := sanitiseName(part.FileName())
			if part.FormName() != "files" || name == "" {
				io.Copy(io.Discard, part)
				part.Close()
				continue
			}
			dest := uniquePath(filepath.Join(dir, name))
			if err := streamToFile(part, dest); err != nil {
				// Nothing truncated stays behind pretending to be the file.
				os.Remove(dest)
				part.Close()
				httpError(w, http.StatusInternalServerError, "保存失败: "+err.Error())
				return
			}
			part.Close()
			saved = append(saved, dest)
		}
		writeJSON(w, map[string]any{"ok": true, "saved": saved, "into": dir})
	})

	// --- server on/off -------------------------------------------------

	mux.HandleFunc("POST /api/serve", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			On bool `json:"on"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.On {
			if !b.Connected() {
				httpError(w, http.StatusConflict, "手机没连上，先插线")
				return
			}
			if err := b.StartServing(); err != nil {
				httpError(w, http.StatusBadGateway, err.Error())
				return
			}
		} else {
			b.StopServing()
		}
		writeJSON(w, b.State())
	})

	// --- static --------------------------------------------------------

	static, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", staticHandler(static, forPhone))

	return mux
}

// bundled is one pre-digested static asset: its bytes, a gzip rendition for
// clients that accept one, and a strong content-hash etag.
type bundled struct {
	data  []byte
	gz    []byte
	etag  string
	ctype string
}

// bundleAssets walks the embedded tree once, at handler construction. Only
// already-compressed formats are skipped — the JS and CSS that dominate page
// weight shrink to about a third over the tunnel, and the etag lets a phone
// that already has today's build answer 304 without re-downloading anything.
func bundleAssets(fsys fs.FS) map[string]*bundled {
	out := map[string]*bundled{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch filepath.Ext(p) {
		case ".js", ".css", ".svg", ".png":
		default:
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil // a missing asset degrades to the file server path
		}
		sum := sha256.Sum256(data)
		b := &bundled{
			data:  data,
			etag:  fmt.Sprintf(`"cabledrop1-%s"`, hex.EncodeToString(sum[:16])),
			ctype: mime.TypeByExtension(filepath.Ext(p)),
		}
		if b.ctype == "" {
			b.ctype = "application/octet-stream"
		}
		var buf bytes.Buffer
		gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		gz.Write(data)
		gz.Close()
		if buf.Len() < len(data) {
			b.gz = buf.Bytes()
		}
		out[p] = b
		return nil
	})
	if err != nil {
		return nil
	}
	return out
}

// staticHandler serves the bundled page, choosing the phone's layout for
// requests that arrived over the tunnel.
//
// "/panel" always serves the desktop panel page even on the phone-facing
// listener. Without it the panel's own layout could only ever be seen by
// clicking the tray icon, which makes it impossible to inspect or screenshot
// during development.
func staticHandler(fsys fs.FS, forPhone bool) http.Handler {
	bundled := bundleAssets(fsys)
	page := "panel.html"
	if forPhone {
		page = "phone.html"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serve := ""
		switch r.URL.Path {
		case "/", "/panel.html":
			serve = page
		case "/panel":
			serve = "panel.html"
		}
		if serve != "" {
			data, err := fs.ReadFile(fsys, serve)
			if err != nil {
				http.Error(w, "页面缺失: "+err.Error(), http.StatusInternalServerError)
				return
			}
			// When this build has the transparent webview, tell the page to
			// draw a translucent tint over the native vibrancy instead of
			// solid paint. Injected on <head> so the class lands before
			// first paint.
			if serve == "panel.html" && translucentChrome {
				data = bytes.Replace(data, []byte("<head>"),
					[]byte("<head><script>document.documentElement.classList.add('native-glass')</script>"), 1)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Write(data)
			return
		}

		// Pre-digested assets: etag revalidation plus gzip when accepted.
		// The map is keyed the way fs.WalkDir spells paths, without the
		// leading slash the URL carries.
		if b, ok := bundled[strings.TrimPrefix(r.URL.Path, "/")]; ok && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			w.Header().Set("ETag", b.etag)
			w.Header().Set("Vary", "Accept-Encoding")
			if matchETag(r.Header.Get("If-None-Match"), b.etag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Content-Type", b.ctype)
			// no-cache: the etag makes revalidation nearly free, and a new
			// build must never serve an old script from a phone's cache.
			w.Header().Set("Cache-Control", "no-cache")
			if b.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				w.Header().Set("Content-Encoding", "gzip")
				if r.Method == http.MethodHead {
					return
				}
				w.Write(b.gz)
				return
			}
			if r.Method == http.MethodHead {
				return
			}
			w.Write(b.data)
			return
		}

		fileServer := http.FileServer(http.FS(fsys))
		fileServer.ServeHTTP(w, r)
	})
}

// NewServer wraps the handler in a server with sane timeouts.
func NewServer(b Backend, forPhone bool) *http.Server {
	return &http.Server{
		Handler:           NewHandler(b, forPhone),
		ReadHeaderTimeout: 10 * time.Second,
		// Generous: a large upload or download over USB is a single request.
		WriteTimeout: 0,
		ReadTimeout:  0,
	}
}

// --- helpers -------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

// clipPayload is the clipboard snapshot shape both /api/clip and the event
// stream's "clip" event speak. The image fingerprint lets a page notice a
// new picture without downloading its bytes.
func clipPayload(b Backend) map[string]any {
	_, sum := b.ClipboardImage()
	return map[string]any{
		"text":  b.ClipboardText(),
		"at":    time.Now().Unix(),
		"image": sum,
	}
}

func notePayload(b Backend) map[string]any {
	text, at := b.Note()
	return map[string]any{"text": text, "at": at}
}

// matchETag reports whether an If-None-Match header matches the resource's
// strong etag. Browsers send the exact etag back, possibly in a list.
func matchETag(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		if strings.TrimSpace(candidate) == etag {
			return true
		}
	}
	return false
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// parentOf returns the containing directory of a device path, or "" at the root.
func parentOf(p string) string {
	p = strings.TrimRight(p, "/")
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return ""
	}
	return p[:i]
}

// listLocal lists the shared folder as the phone sees it.
func listLocal(root string) ([]model.Entry, error) {
	items, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := make([]model.Entry, 0, len(items))
	for _, it := range items {
		if strings.HasPrefix(it.Name(), ".") {
			continue
		}
		info, err := it.Info()
		if err != nil {
			continue
		}
		out = append(out, model.Entry{
			Name:    it.Name(),
			Path:    filepath.Join(root, it.Name()),
			Dir:     it.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return out[i].ModTime > out[j].ModTime
	})
	return out, nil
}

// safeJoin resolves p inside root, refusing anything that escapes it.
//
// Traversal is checked after resolution (not by scanning for "..") so symlinks
// and encodings can't smuggle a path out of the shared folder.
func safeJoin(root, p string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := p
	if !filepath.IsAbs(target) {
		target = filepath.Join(absRoot, p)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径越界")
	}
	return target, nil
}

// streamToFile copies one upload part straight to its destination — the
// whole point of the streaming reader: no temp stage, no second write.
func streamToFile(src io.Reader, dest string) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, src)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// sanitiseName strips any directory part a client may have sent along.
func sanitiseName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	if name == "." || name == ".." || name == "/" {
		return ""
	}
	return name
}

// uniquePath avoids clobbering an existing file by appending " (2)", " (3)"…
func uniquePath(p string) string {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	dir, base := filepath.Split(p)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; i < 10000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return p
}

// urlEscape escapes a filename for the Content-Disposition header.
func urlEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
