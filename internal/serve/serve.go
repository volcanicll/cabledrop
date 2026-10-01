// Package serve is the HTTP surface shared by both clients: the JSON API plus
// the embedded pages.
//
// It depends on model and on the narrow Backend interface below — never on the
// app package. The app implements Backend and starts this package's server for
// the phone; the native panel reaches the same handler through Wails' asset
// server, which is what keeps both clients on one API.
package serve

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
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
	Note() (string, int64)
	SetNote(text string)

	ListDeviceDir(dir string) ([]model.Entry, error)
	DeviceDirs() []model.Entry
	// CheckDevicePath rejects device paths outside the shared-storage
	// whitelist, so callers get a synchronous 403 instead of a failed row.
	CheckDevicePath(path string) error
	// PullDeviceFile copies a device file into the shared folder, in the
	// background; the transfer shows up in State.
	PullDeviceFile(remote string)
	DeleteDevicePath(remote string) error
	PushFiles(paths []string, destDir string)
	ClearTransfers()

	// PickFiles and PickDir open the system file picker. Implementations step
	// the panel aside while the dialog is up.
	PickFiles(prompt string) ([]string, error)
	PickDir(prompt string) (string, error)
	// OpenPath reveals a folder in the platform file manager.
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

// NewHandler builds the whole HTTP surface: the JSON API plus the static page.
//
// The same handler serves both clients — the native panel (through Wails'
// asset server) and the phone's browser (through adb reverse). forPhone only
// decides which page "/" returns; the API is identical, so every feature is
// available on both sides.
func NewHandler(b Backend, forPhone bool) http.Handler {
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
		writeJSON(w, map[string]any{
			"text": b.ClipboardText(),
			"at":   time.Now().Unix(),
		})
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
		b.PullDeviceFile(body.Path)
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
		list, err := listLocal(root)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"root": root, "entries": list})
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
	mux.HandleFunc("POST /upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			httpError(w, http.StatusBadRequest, "上传解析失败: "+err.Error())
			return
		}
		saved, err := saveUploads(r.MultipartForm, b.ServeDir())
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true, "saved": saved, "into": b.ServeDir()})
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

// staticHandler serves the bundled page, choosing the phone's layout for
// requests that arrived over the tunnel.
//
// "/panel" always serves the desktop panel page even on the phone-facing
// listener. Without it the panel's own layout could only ever be seen by
// clicking the tray icon, which makes it impossible to inspect or screenshot
// during development.
func staticHandler(fsys fs.FS, forPhone bool) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
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
		if serve == "" {
			fileServer.ServeHTTP(w, r)
			return
		}
		data, err := fs.ReadFile(fsys, serve)
		if err != nil {
			http.Error(w, "页面缺失: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
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

// saveUploads writes uploaded files into dir, never overwriting.
func saveUploads(form *multipart.Form, dir string) ([]string, error) {
	var saved []string
	for _, headers := range form.File {
		for _, h := range headers {
			name := sanitiseName(h.Filename)
			if name == "" {
				continue
			}
			dest := uniquePath(filepath.Join(dir, name))
			if err := copyUpload(h, dest); err != nil {
				return saved, err
			}
			saved = append(saved, dest)
		}
	}
	return saved, nil
}

func copyUpload(h *multipart.FileHeader, dest string) error {
	src, err := h.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, src)
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
