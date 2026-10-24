package serve_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
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
	"time"

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
	clipImage  []byte
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
	subs       []map[string]chan struct{}
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
func (f *fakeBackend) Refresh()                       { f.refreshes++ }
func (f *fakeBackend) Connected() bool                { return f.state.Connected }
func (f *fakeBackend) Native() bool                   { return f.native }
func (f *fakeBackend) ServeDir() string               { return f.serveDir }
func (f *fakeBackend) SetServeDir(dir string) error   { f.serveDir = dir; return nil }
func (f *fakeBackend) StartServing() error            { f.serving = true; return nil }
func (f *fakeBackend) StopServing()                   { f.serving = false }
func (f *fakeBackend) ClipboardText() string          { return f.clip }
func (f *fakeBackend) ClipboardSet(text string) error { f.clip = text; return f.clipErr }
func (f *fakeBackend) ClipboardImage() ([]byte, string) {
	if len(f.clipImage) == 0 {
		return nil, ""
	}
	sum := sha256.Sum256(f.clipImage)
	return f.clipImage, hex.EncodeToString(sum[:])
}
func (f *fakeBackend) Note() (string, int64) { return f.note, f.noteAt }
func (f *fakeBackend) SetNote(text string)   { f.note = text; f.noteAt = 42 }
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
func (f *fakeBackend) PullDeviceFile(remote string, size int64) {
	f.pulled = append(f.pulled, remote)
}
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

// Subscribe satisfies the event-stream half of the contract. Each
// subscription's channels are kept so a test can fire them the way the real
// app fires when its state changes.
func (f *fakeBackend) Subscribe() serve.Subscriptions {
	topics := map[string]chan struct{}{
		"state": make(chan struct{}, 1),
		"clip":  make(chan struct{}, 1),
		"note":  make(chan struct{}, 1),
	}
	f.subs = append(f.subs, topics)
	return serve.Subscriptions{
		State:  topics["state"],
		Clip:   topics["clip"],
		Note:   topics["note"],
		Cancel: func() {},
	}
}

// emit fires one signal on the given topic for every live subscription.
func (f *fakeBackend) emit(topic string) {
	for _, s := range f.subs {
		select {
		case s[topic] <- struct{}{}:
		default:
		}
	}
}

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

// TestAPIClipImageEndpoint pins the image side of the clipboard contract: the
// text endpoint gains an image fingerprint, the binary endpoint serves the
// bytes with the right type and no image means a 404 carrying the JSON error
// shape the pages parse.
func TestAPIClipImageEndpoint(t *testing.T) {
	b := newFakeBackend()
	h := serve.NewHandler(b, true) // the phone-facing surface, like real use

	// No image: the fingerprint is empty and the binary endpoint answers 404
	// with a JSON error body, not a text 404 page.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/clip", nil))
	var clip struct {
		Text  string `json:"text"`
		Image string `json:"image"`
	}
	json.Unmarshal(rec.Body.Bytes(), &clip)
	if clip.Image != "" {
		t.Fatalf("empty clipboard image fingerprint = %q", clip.Image)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/clip/image", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("image endpoint without image = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("404 body is not the JSON error shape: %s", rec.Body.String())
	}

	// With an image: bytes round-trip, the type and fingerprint match, and the
	// text endpoint's fingerprint points at it.
	b.clipImage = []byte("89504E47-fake-png-bytes")
	sum := sha256.Sum256(b.clipImage)
	wantSum := hex.EncodeToString(sum[:])

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/clip/image", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("image endpoint = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type = %q", got)
	}
	if got := rec.Header().Get("ETag"); got != `"`+wantSum+`"` {
		t.Fatalf("etag = %q, want %q", got, wantSum)
	}
	if rec.Body.String() != string(b.clipImage) {
		t.Fatalf("body does not match the clipboard image")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/clip", nil))
	clip = struct {
		Text  string `json:"text"`
		Image string `json:"image"`
	}{}
	json.Unmarshal(rec.Body.Bytes(), &clip)
	if clip.Image != wantSum {
		t.Fatalf("text endpoint fingerprint = %q, want %q", clip.Image, wantSum)
	}

	// ?dl=1 turns the same bytes into a download, the fallback for phones that
	// cannot write images to their clipboard.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/clip/image?dl=1", nil))
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Fatalf("dl=1 content-disposition = %q", cd)
	}
}

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

	for _, path := range []string{"/api/pick/files", "/api/pick/dir", "/api/shared/open", "/api/shared/reveal", "/api/panel/hide"} {
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

// TestSSEStreamPushesStateClipNote pins the event-stream contract: an
// opening snapshot of all three topics with no priming fetch, then changes
// pushed the moment the backend signals them — the shape the phone page's
// whole rendering path is built on.
func TestSSEStreamPushesStateClipNote(t *testing.T) {
	b := newFakeBackend()
	srv := httptest.NewServer(serve.NewHandler(b, true))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	br := bufio.NewReader(resp.Body)
	readEvent := func(want string) string {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		var event, data string
		for time.Now().Before(deadline) {
			line, err := br.ReadString('\n')
			if err != nil {
				t.Fatalf("stream read: %v", err)
			}
			line = strings.TrimRight(line, "\n")
			if strings.HasPrefix(line, "event: ") {
				event = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				data = strings.TrimPrefix(line, "data: ")
			}
			if line == "" && event != "" {
				if event != want {
					t.Fatalf("event = %q, want %q", event, want)
				}
				return data
			}
		}
		t.Fatalf("event %q never arrived", want)
		return ""
	}

	// Opening snapshot: state, clip, note — in that order.
	readEvent("state")
	readEvent("clip")
	readEvent("note")

	// A backend change is pushed, not polled.
	b.emit("state")
	var st model.State
	if err := json.Unmarshal([]byte(readEvent("state")), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Connected {
		t.Fatalf("pushed state = %+v", st)
	}

	b.emit("clip")
	var clip map[string]any
	if err := json.Unmarshal([]byte(readEvent("clip")), &clip); err != nil {
		t.Fatal(err)
	}
	if _, ok := clip["image"]; !ok {
		t.Fatalf("clip event lacks the image fingerprint: %v", clip)
	}

	// A client that walks away is unsubscribed, not leaked.
	resp.Body.Close()
	time.Sleep(50 * time.Millisecond)
	b.emit("state") // must not panic; the handler has returned
}

// TestStaticAssetsGetETagGzipAnd304 pins the asset pipeline: gzip when the
// client accepts it, a strong content-hash etag either way, and a 304 when
// the etag comes back — the difference between re-downloading the page's
// scripts every open and re-validating them for free.
func TestStaticAssetsGetETagGzipAnd304(t *testing.T) {
	b := newFakeBackend()
	h := serve.NewHandler(b, true)

	req := httptest.NewRequest("GET", "/js/api.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("gzip request = %d", rec.Code)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("content-encoding = %q", ce)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("asset carried no etag")
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("body is not gzip: %v", err)
	}
	var plain bytes.Buffer
	io.Copy(&plain, zr)
	if !strings.Contains(plain.String(), "async function api") {
		t.Fatalf("gunzipped body is not api.js: %.80q", plain.String())
	}

	// A matching etag is a 304, gzip accepted or not.
	req = httptest.NewRequest("GET", "/js/api.js", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("revalidation = %d, want 304", rec.Code)
	}

	// Without gzip in Accept-Encoding the plain bytes still arrive.
	req = httptest.NewRequest("GET", "/js/api.js", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || strings.Contains(rec.Header().Get("Content-Encoding"), "gzip") {
		t.Fatalf("plain request = %d encoding=%q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	if !strings.Contains(rec.Body.String(), "async function api") {
		t.Fatal("plain body is not api.js")
	}
}

func TestClipImageAnswersIfNoneMatchWith304(t *testing.T) {
	b := newFakeBackend()
	b.clipImage = []byte("89504E47-fake-png-bytes")
	h := serve.NewHandler(b, true)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/clip/image", nil))
	if rec.Code != 200 {
		t.Fatalf("first fetch = %d", rec.Code)
	}
	etag := rec.Header().Get("ETag")

	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/clip/image", nil)
	req.Header.Set("If-None-Match", etag)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("revalidation = %d, want 304", rec.Code)
	}
}

func TestRevealEndpointStaysInsideTheSharedRoot(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "got.txt"), []byte("x"), 0o644)
	b := newFakeBackend()
	b.serveDir = dir
	h := serve.NewHandler(b, false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/shared/reveal",
		strings.NewReader(`{"path":"got.txt"}`)))
	if rec.Code != 200 {
		t.Fatalf("reveal = %d: %s", rec.Code, rec.Body.String())
	}
	if len(b.opened) != 1 || b.opened[0] != filepath.Join(dir, "got.txt") {
		t.Fatalf("opened = %v", b.opened)
	}

	// Traversal is refused, and so is a file that is not there.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/shared/reveal",
		strings.NewReader(`{"path":"../secret"}`)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("traversal = %d, want 403", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/shared/reveal",
		strings.NewReader(`{"path":"missing.txt"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing file = %d, want 404", rec.Code)
	}
}

// TestUploadRejectsAMidStreamTruncation covers the streaming reader's
// failure path: a body cut off mid-file is an error, and nothing truncated
// is left behind pretending to be the file.
func TestUploadRejectsAMidStreamTruncation(t *testing.T) {
	dir := t.TempDir()
	b := newFakeBackend()
	b.serveDir = dir
	h := serve.NewHandler(b, false)

	var buf strings.Builder
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("files", "cut.txt")
	part.Write([]byte(strings.Repeat("payload-", 400)))
	w.Close()
	body := buf.String()[:buf.Len()-30] // cut into the file's own bytes

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/upload", strings.NewReader(body))
	req.Header.Set("Content-Type", w.FormDataContentType())
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusInternalServerError {
		t.Fatalf("truncated upload = %d, want 400/500", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "cut.txt")); !os.IsNotExist(err) {
		t.Fatalf("a truncated file was left behind: %v", err)
	}
}
