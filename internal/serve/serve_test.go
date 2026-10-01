package serve_test

import (
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volcanicll/cabledrop/internal/model"
	"github.com/volcanicll/cabledrop/internal/serve"
)

// fakeBackend is an in-memory serve.Backend with no adb, no clipboard
// subprocess and no UI behind it.
type fakeBackend struct {
	state      model.State
	refreshes  int
	serving    bool
	clip       string
	clipErr    error
	note       string
	noteAt     int64
	serveDir   string
	entries    []model.Entry
	listErr    error
	badPathErr error
	pulled     []string
	deleted    []string
	pushed     [][]string
	cleared    int
	pickFiles  []string
	pickDir    string
	opened     []string
	hidden     int
	native     bool
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		state: model.State{
			Platform:  "darwin",
			ADBFound:  true,
			Connected: true,
			Device:    "Pixel 8",
			Serial:    "TEST1",
		},
		serveDir: "/tmp/fake-shared",
		native:   true,
		entries: []model.Entry{
			{Name: "a.txt", Path: "/tmp/fake-shared/a.txt"},
			{Name: "sub", Path: "/tmp/fake-shared/sub", Dir: true},
		},
	}
}

func (f *fakeBackend) State() model.State {
	f.state.Serving = f.serving
	f.state.Native = f.native
	return f.state
}
func (f *fakeBackend) Refresh()                            { f.refreshes++ }
func (f *fakeBackend) Connected() bool                     { return f.state.Connected }
func (f *fakeBackend) Native() bool                        { return f.native }
func (f *fakeBackend) ServeDir() string                    { return f.serveDir }
func (f *fakeBackend) SetServeDir(dir string) error        { f.serveDir = dir; return nil }
func (f *fakeBackend) StartServing() error                 { f.serving = true; return nil }
func (f *fakeBackend) StopServing()                        { f.serving = false }
func (f *fakeBackend) ClipboardText() string               { return f.clip }
func (f *fakeBackend) ClipboardSet(text string) error      { f.clip = text; return f.clipErr }
func (f *fakeBackend) Note() (string, int64)               { return f.note, f.noteAt }
func (f *fakeBackend) SetNote(text string)                 { f.note = text; f.noteAt = 42 }
func (f *fakeBackend) ListDeviceDir(dir string) ([]model.Entry, error) {
	return f.entries, f.listErr
}
func (f *fakeBackend) DeviceDirs() []model.Entry { return f.entries }
func (f *fakeBackend) CheckDevicePath(path string) error {
	if f.badPathErr != nil {
		return f.badPathErr
	}
	return model.ErrBadDevicePath
}
func (f *fakeBackend) PullDeviceFile(remote string) { f.pulled = append(f.pulled, remote) }
func (f *fakeBackend) DeleteDevicePath(remote string) error {
	f.deleted = append(f.deleted, remote)
	return nil
}
func (f *fakeBackend) PushFiles(paths []string, destDir string) {
	f.pushed = append(f.pushed, paths)
}
func (f *fakeBackend) ClearTransfers() { f.cleared++ }
func (f *fakeBackend) PickFiles(prompt string) ([]string, error) {
	return f.pickFiles, nil
}
func (f *fakeBackend) PickDir(prompt string) (string, error) { return f.pickDir, nil }
func (f *fakeBackend) OpenPath(path string)                  { f.opened = append(f.opened, path) }
func (f *fakeBackend) HidePanel()                            { f.hidden++ }

func TestAPIStateShape(t *testing.T) {
	b := newFakeBackend()
	h := serve.NewHandler(b, false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/state", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	// The contract both clients build on: these keys must always be present.
	for _, k := range []string{
		"platform", "adbFound", "adbPath", "connected", "device", "serial",
		"storageFree", "serveDir", "serving", "port", "phoneURL",
		"transfers", "native",
	} {
		if _, ok := raw[k]; !ok {
			t.Errorf("/api/state missing key %q", k)
		}
	}
	if raw["device"] != "Pixel 8" {
		t.Errorf("device = %v", raw["device"])
	}
}

func TestAPIClipRoundTrip(t *testing.T) {
	b := newFakeBackend()
	h := serve.NewHandler(b, false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/clip", nil))
	var got struct {
		Text string `json:"text"`
		At   int64  `json:"at"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Text != "" {
		t.Fatalf("initial clipboard = %q, want empty", got.Text)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/clip", strings.NewReader(`{"text":"你好"}`)))
	if rec.Code != 200 {
		t.Fatalf("POST /api/clip status = %d", rec.Code)
	}
	if b.clip != "你好" {
		t.Fatalf("backend clipboard = %q", b.clip)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/clip", nil))
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Text != "你好" {
		t.Fatalf("GET after POST = %q", got.Text)
	}

	// Malformed body is a 400 with the JSON error shape, not a 500.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/clip", strings.NewReader("{oops")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "error") {
		t.Fatalf("bad body response = %s", rec.Body.String())
	}

	// A backend failure is a 500.
	b.clipErr = errFake
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/clip", strings.NewReader(`{"text":"x"}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failing set status = %d", rec.Code)
	}
}

var errFake = &fakeError{}

type fakeError struct{}

func (*fakeError) Error() string { return "fake clipboard failure" }

func TestAPINote(t *testing.T) {
	b := newFakeBackend()
	h := serve.NewHandler(b, false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/note", strings.NewReader(`{"text":"hello"}`)))
	if rec.Code != 200 {
		t.Fatalf("POST /api/note = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/note", nil))
	var got struct {
		Text string `json:"text"`
		At   int64  `json:"at"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Text != "hello" || got.At != 42 {
		t.Fatalf("note = %+v", got)
	}
}

func TestAPISharedListsServeDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644)

	b := newFakeBackend()
	b.serveDir = dir
	h := serve.NewHandler(b, false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/shared", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Root    string        `json:"root"`
		Entries []model.Entry `json:"entries"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Root != dir {
		t.Fatalf("root = %q", got.Root)
	}
	// Directories first, dotfiles skipped.
	if len(got.Entries) != 2 || got.Entries[0].Name != "sub" {
		t.Fatalf("entries = %+v", got.Entries)
	}
}

func TestAPIDevicePullChecksTheWhitelistSynchronously(t *testing.T) {
	b := newFakeBackend()
	h := serve.NewHandler(b, false)

	// The fake rejects every path by default: a pull must be refused with 403
	// here, not accepted with 200 and failed later in the background.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/device/pull",
		strings.NewReader(`{"path":"/sdcard/x.jpg"}`)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("rejected path status = %d", rec.Code)
	}
	if len(b.pulled) != 0 {
		t.Fatalf("background pull started anyway: %v", b.pulled)
	}

	// With the whitelist satisfied the pull is handed to the backend.
	b2 := newFakeBackend()
	h2 := serve.NewHandler(allowAll{b2}, false)
	rec = httptest.NewRecorder()
	h2.ServeHTTP(rec, httptest.NewRequest("POST", "/api/device/pull",
		strings.NewReader(`{"path":"/sdcard/x.jpg"}`)))
	if rec.Code != 200 {
		t.Fatalf("valid pull status = %d", rec.Code)
	}
	if len(b2.pulled) != 1 || b2.pulled[0] != "/sdcard/x.jpg" {
		t.Fatalf("pulled = %v", b2.pulled)
	}
}

// allowAll whitelists every path, standing in for a real device backend.
type allowAll struct{ *fakeBackend }

func (allowAll) CheckDevicePath(string) error { return nil }

func TestAPIDeviceDelete(t *testing.T) {
	b := newFakeBackend()
	h := serve.NewHandler(b, false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/device/delete",
		strings.NewReader(`{"path":"/sdcard/old.txt"}`)))
	if rec.Code != 200 || len(b.deleted) != 1 {
		t.Fatalf("delete status=%d deleted=%v", rec.Code, b.deleted)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/device/delete", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing path status = %d", rec.Code)
	}
}

func TestAPISend(t *testing.T) {
	b := newFakeBackend()
	h := serve.NewHandler(b, false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/send",
		strings.NewReader(`{"paths":["/tmp/a","/tmp/b"],"dir":"/sdcard/Download"}`)))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(b.pushed) != 1 || len(b.pushed[0]) != 2 {
		t.Fatalf("pushed = %v", b.pushed)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/send", strings.NewReader(`{"paths":[]}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty paths status = %d", rec.Code)
	}
}

func TestNativeOnlyEndpointsRefuseThePhone(t *testing.T) {
	b := newFakeBackend()
	// forPhone=true gates the backend even though it reports native.
	h := serve.NewHandler(b, true)

	for _, path := range []string{"/api/pick/files", "/api/pick/dir", "/api/shared/open", "/api/panel/hide"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader("{}")))
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("POST %s on phone surface = %d, want 501", path, rec.Code)
		}
	}
	if b.hidden != 0 || len(b.opened) != 0 {
		t.Errorf("phone surface reached native behaviour: hidden=%d opened=%v", b.hidden, b.opened)
	}

	// The desktop surface keeps them working.
	h = serve.NewHandler(b, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/panel/hide", strings.NewReader("{}")))
	if rec.Code != 200 || b.hidden != 1 {
		t.Fatalf("panel hide on desktop = %d, hidden=%d", rec.Code, b.hidden)
	}
}

func TestStaticRouting(t *testing.T) {
	b := newFakeBackend()

	// The phone-facing listener serves the phone page at "/".
	h := serve.NewHandler(b, true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "phone.js") || strings.Contains(body, `id="txCard"`) {
		t.Errorf("phone / did not serve the phone page")
	}

	// /panel serves the desktop page everywhere, so the panel layout can be
	// inspected without the tray.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/panel", nil))
	if !strings.Contains(rec.Body.String(), "panel.js") {
		t.Errorf("/panel did not serve the panel page")
	}

	// The panel-facing listener serves the panel page at "/".
	h = serve.NewHandler(b, false)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rec.Body.String(), "panel.js") {
		t.Errorf("desktop / did not serve the panel page")
	}

	// Static assets resolve through the same handler.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/js/api.js", nil))
	if rec.Code != 200 {
		t.Errorf("js/api.js = %d", rec.Code)
	}
}

func TestDownloadRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := newFakeBackend()
	b.serveDir = dir
	h := serve.NewHandler(b, false)

	for _, p := range []string{"../secret", "..%2Fsecret", "/etc/passwd", "a/../../b"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/dl?p="+url.QueryEscape(p), nil))
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
			t.Errorf("/dl?p=%q = %d, want 403/404", p, rec.Code)
		}
	}

	// Normal download: right body, RFC 5987 filename for non-ASCII names.
	name := "文件 测试.txt"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("nihao"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/dl?p="+url.QueryEscape(name), nil))
	if rec.Code != 200 {
		t.Fatalf("download status = %d", rec.Code)
	}
	bodyBytes, _ := io.ReadAll(rec.Body)
	if string(bodyBytes) != "nihao" {
		t.Fatalf("body = %q", bodyBytes)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment; filename*=UTF-8''") {
		t.Fatalf("content-disposition = %q", cd)
	}
	if !strings.Contains(cd, urlEscapePath(name)) {
		t.Fatalf("content-disposition %q lacks the encoded name", cd)
	}
}

func urlEscapePath(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), "+", "%20")
}

func TestUpload(t *testing.T) {
	dir := t.TempDir()
	b := newFakeBackend()
	b.serveDir = dir
	h := serve.NewHandler(b, false)

	// A multipart body with two files, one with a directory component in the
	// client-side filename and one empty.
	var buf strings.Builder
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("files", "报告 final.txt")
	part.Write([]byte("内容一"))
	part, _ = w.CreateFormFile("files", "../../evil/名字.txt")
	part.Write([]byte("内容二"))
	w.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/upload", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("upload status = %d: %s", rec.Code, rec.Body.String())
	}

	// Both files land flat in the shared dir, nothing overwrites, and the
	// traversal attempt is stripped to a basename.
	for _, name := range []string{"报告 final.txt", "名字.txt"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("expected %s in shared dir: %v", name, err)
		}
		_ = data
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); !os.IsNotExist(err) {
		t.Fatalf("upload created directories: %v", err)
	}

	// Uploading the same name again dedupes instead of overwriting.
	buf.Reset()
	w = multipart.NewWriter(&buf)
	part, _ = w.CreateFormFile("files", "报告 final.txt")
	part.Write([]byte("内容三"))
	w.Close()
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/upload", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("second upload status = %d", rec.Code)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "报告 final.txt"))
	if string(data) != "内容一" {
		t.Fatalf("first file was overwritten: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "报告 final (2).txt")); err != nil {
		t.Fatalf("duplicate not saved alongside: %v", err)
	}

	// A non-multipart body is a 400.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/upload", strings.NewReader("nope")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad upload status = %d", rec.Code)
	}
}

func TestServeOnOff(t *testing.T) {
	b := newFakeBackend()
	b.state.Connected = false
	h := serve.NewHandler(b, false)

	// Not connected: 409, and nothing starts.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/serve", strings.NewReader(`{"on":true}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("serve without device = %d", rec.Code)
	}
	if b.serving {
		t.Fatal("server started without a device")
	}

	b.state.Connected = true
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/serve", strings.NewReader(`{"on":true}`)))
	if rec.Code != 200 || !b.serving {
		t.Fatalf("serve with device = %d serving=%v", rec.Code, b.serving)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/serve", strings.NewReader(`{"on":false}`)))
	if rec.Code != 200 || b.serving {
		t.Fatalf("serve off = %d serving=%v", rec.Code, b.serving)
	}
}
