package serve_test

import (
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Guards for the landing page in site/, which is a separate surface from the
// app's own pages: it has its own dictionary and its own copy, and nothing
// compiles either one. The failure this file exists for is the screenshot set:
// the page is bilingual, so a screenshot left in one language shows up as
// English copy over a Chinese UI — and because the images are named per
// language, a missing translation is a 404 rather than a visible mistake.
//
// The test lives here rather than beside site/ so that `go test ./...` — which
// is what CI runs — picks it up. site/ has no Go package of its own and its
// workflow does not run tests.

// siteRoot walks up from the package directory to the repository root. Hard
// coding ../.. would break the moment the package moves.
func siteRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "site", "index.html")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("never found site/index.html above the package directory")
		}
		dir = parent
	}
}

func readSiteFile(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

// siteEntry accepts both quote styles: values containing an apostrophe (the
// English alt text does) are written with double quotes, and a parser that
// only understood single quotes would silently skip them — which is exactly
// the entry that would then go unchecked.
var siteEntry = regexp.MustCompile(`'([a-zA-Z0-9.]+)':\s*(?:'([^']*)'|"([^"]*)")`)

// siteDict pulls one language block out of site/i18n.js.
func siteDict(t *testing.T, src, lang string) map[string]string {
	t.Helper()
	open := strings.Index(src, "\n    "+lang+": {")
	if open < 0 {
		t.Fatalf("site/i18n.js has no %s block", lang)
	}
	rest := src[open:]
	closing := strings.Index(rest, "\n    }")
	if closing < 0 {
		t.Fatalf("site/i18n.js %s block never closes", lang)
	}
	dict := map[string]string{}
	for _, m := range siteEntry.FindAllStringSubmatch(rest[:closing], -1) {
		val := m[2]
		if val == "" && m[3] != "" {
			val = m[3]
		}
		if _, dup := dict[m[1]]; dup {
			t.Errorf("site/i18n.js %s: duplicate key %q", lang, m[1])
		}
		dict[m[1]] = val
	}
	if len(dict) == 0 {
		t.Fatalf("site/i18n.js %s block parsed to nothing", lang)
	}
	return dict
}

// TestSiteLanguagesAreComplete cross-checks the two dictionaries and the keys
// the page actually asks for. A key present in one language only renders as
// the raw key in the other; a key no element references is dead weight.
func TestSiteLanguagesAreComplete(t *testing.T) {
	root := siteRoot(t)
	src := readSiteFile(t, root, "site/i18n.js")
	zh := siteDict(t, src, "zh")
	en := siteDict(t, src, "en")
	html := readSiteFile(t, root, "site/index.html")

	for k := range zh {
		if _, ok := en[k]; !ok {
			t.Errorf("key %q is in zh but missing in en", k)
		}
	}
	for k := range en {
		if _, ok := zh[k]; !ok {
			t.Errorf("key %q is in en but missing in zh", k)
		}
	}

	used := map[string]bool{}
	attrRe := regexp.MustCompile(`data-i18n(?:-html|-attr)?="([^"]*)"`)
	for _, m := range attrRe.FindAllStringSubmatch(html, -1) {
		for _, part := range strings.Split(m[1], ",") {
			// data-i18n-attr carries "attr:key" pairs; the others are a bare key.
			key := part
			if i := strings.Index(part, ":"); i >= 0 {
				key = part[i+1:]
			}
			if key = strings.TrimSpace(key); key != "" {
				used[key] = true
			}
		}
	}
	for k := range used {
		for lang, dict := range map[string]map[string]string{"zh": zh, "en": en} {
			if _, ok := dict[k]; !ok {
				t.Errorf("key %q is used in site/index.html but missing in %s — the page would show the raw key", k, lang)
			}
		}
	}
}

// TestSiteScreenshotsSwitchLanguage pins the point of the per-language images:
// every shot the page swaps must have a counterpart in the other language, and
// the file it names must exist. A typo here is a 404 that only shows up after
// switching language, on a page nobody reloads in the other language by hand.
func TestSiteScreenshotsSwitchLanguage(t *testing.T) {
	root := siteRoot(t)
	src := readSiteFile(t, root, "site/i18n.js")
	zh := siteDict(t, src, "zh")
	en := siteDict(t, src, "en")

	keys := map[string]bool{}
	for k := range en {
		if strings.HasPrefix(k, "shot.") && strings.HasSuffix(k, ".src") {
			keys[k] = true
		}
	}
	if len(keys) == 0 {
		t.Fatal("no shot.*.src keys found — the screenshot swap is unguarded")
	}

	for k := range keys {
		zs, zok := zh[k]
		if !zok {
			t.Errorf("%q is in en but not in zh — switching to Chinese would leave the English screenshot up", k)
			continue
		}
		if zs == en[k] {
			t.Errorf("%q is the same path in both languages (%q) — the screenshot does not switch", k, zs)
		}
		for lang, rel := range map[string]string{"en": en[k], "zh": zs} {
			// Values are written relative to site/, where the page lives.
			p := filepath.Join(root, "site", filepath.FromSlash(rel))
			if _, err := os.Stat(p); err != nil {
				t.Errorf("%s %s = %q does not resolve to a file (%v)", lang, k, rel, err)
			}
		}
	}
}

// TestSiteImageBoxesMatchTheImages catches stale width/height attributes on the
// <img> tags. The attributes reserve the box before the image loads; when they
// disagree with the real file the page reflows on load, and because the two
// languages use different files it reflows again on every switch.
func TestSiteImageBoxesMatchTheImages(t *testing.T) {
	root := siteRoot(t)
	html := readSiteFile(t, root, "site/index.html")

	imgRe := regexp.MustCompile(`<img[^>]*src="([^"]+)"[^>]*width="(\d+)"[^>]*height="(\d+)"[^>]*data-i18n-attr="src:([^",]+)`)
	found := 0
	for _, m := range imgRe.FindAllStringSubmatch(html, -1) {
		rel, w, h, key := m[1], m[2], m[3], m[4]
		if strings.HasPrefix(rel, "http") {
			continue
		}
		found++
		f, err := os.Open(filepath.Join(root, "site", filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("%s: cannot open %q: %v", key, rel, err)
			continue
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil {
			t.Errorf("%s: %q is not a readable image: %v", key, rel, err)
			continue
		}
		wantW, _ := strconv.Atoi(w)
		wantH, _ := strconv.Atoi(h)
		// Only the ratio matters: the CSS sets the display width, and the
		// attributes exist to reserve the right shape before the bytes arrive.
		gotRatio := float64(cfg.Height) / float64(cfg.Width)
		wantRatio := float64(wantH) / float64(wantW)
		if diff := gotRatio - wantRatio; diff > 0.01 || diff < -0.01 {
			t.Errorf("%s: declared %dx%d (ratio %.3f) but %q is %dx%d (ratio %.3f) — the box is the wrong shape",
				key, wantW, wantH, wantRatio, rel, cfg.Width, cfg.Height, gotRatio)
		}
	}
	if found == 0 {
		t.Fatal("no language-swapped <img> tags matched — the pattern or the markup changed")
	}
}
