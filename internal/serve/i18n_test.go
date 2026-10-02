package serve_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/volcanicll/cabledrop/internal/serve"
)

// Guards for the bilingual UI. The dictionary in assets/js/i18n.js is the
// single source of every user-facing string on both surfaces, and nothing
// compiles it — a key missing from one language or a Chinese string left in
// the HTML ships as a mixed-language page. These tests read the assets the
// way the server hands them to a browser.

var i18nEntry = regexp.MustCompile(`^\s*'([a-zA-Z0-9.]+)': '([^']*)',?$`)

func readAsset(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("assets", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read assets/%s: %v", rel, err)
	}
	return string(raw)
}

// parseI18nDict extracts one language block. Entries are deliberately written
// one per line in 'key': 'value', form — a line that does not parse is itself
// a bug, because the machine-readable shape is what lets this test exist.
func parseI18nDict(t *testing.T, src, lang string) map[string]string {
	t.Helper()
	open := strings.Index(src, lang+": {")
	if open < 0 {
		t.Fatalf("i18n.js has no %s block", lang)
	}
	rest := src[open+len(lang)+3:]
	closing := strings.Index(rest, "\n  }")
	if closing < 0 {
		t.Fatalf("i18n.js %s block never closes", lang)
	}
	dict := map[string]string{}
	for _, line := range strings.Split(rest[:closing], "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := i18nEntry.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("i18n.js %s: unparsable entry %q — write one entry per line as 'key': 'value',", lang, line)
		}
		if _, dup := dict[m[1]]; dup {
			t.Fatalf("i18n.js %s: duplicate key %q", lang, m[1])
		}
		dict[m[1]] = m[2]
	}
	if len(dict) == 0 {
		t.Fatalf("i18n.js %s block is empty", lang)
	}
	return dict
}

// TestI18nKeysAreComplete cross-checks the dictionaries against every key the
// pages use: through t() in the scripts or a data-i18n attribute in the HTML.
// A key missing from a language renders as the raw key — the exact bug this
// feature is most likely to ship with — and a key no page references is dead
// weight that rots until it lies.
func TestI18nKeysAreComplete(t *testing.T) {
	src := readAsset(t, "js/i18n.js")
	zh := parseI18nDict(t, src, "zh")
	en := parseI18nDict(t, src, "en")

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
	// \bt\( so a name merely ending in t (split, parseInt) never matches;
	// only literal single-quoted keys count, which is also the house style.
	keyRe := regexp.MustCompile(`\bt\('([a-zA-Z0-9.]+)'`)
	for _, f := range []string{"js/i18n.js", "js/api.js", "js/phone.js", "js/panel.js"} {
		for _, m := range keyRe.FindAllStringSubmatch(readAsset(t, f), -1) {
			used[m[1]] = true
		}
	}
	attrRe := regexp.MustCompile(`data-i18n(?:-ph|-alt|-title|-aria)?="([a-zA-Z0-9.]+)"`)
	for _, f := range []string{"phone.html", "panel.html"} {
		for _, m := range attrRe.FindAllStringSubmatch(readAsset(t, f), -1) {
			used[m[1]] = true
		}
	}

	for k := range used {
		for lang, dict := range map[string]map[string]string{"zh": zh, "en": en} {
			if _, ok := dict[k]; !ok {
				t.Errorf("key %q is used but missing in %s — the UI would show the raw key", k, lang)
			}
		}
	}
	for lang, dict := range map[string]map[string]string{"zh": zh, "en": en} {
		for k := range dict {
			if !used[k] {
				t.Errorf("key %q sits in the %s dictionary but no page uses it", k, lang)
			}
		}
	}
}

// TestServedHTMLIsLanguageNeutral pins the shape of the pages themselves:
// they carry no user-facing text at all, because text baked into the HTML
// would flash one language before applyLang() runs and never switch after.
// The pages used to be hard-coded Chinese, so absence of any CJK rune in the
// served bytes is the proof that none of it crept back.
func TestServedHTMLIsLanguageNeutral(t *testing.T) {
	cjk := regexp.MustCompile(`[\x{3000}-\x{303F}\x{4E00}-\x{9FFF}\x{FF01}-\x{FF60}]`)

	// The phone-facing listener serves phone.html at / and panel.html at
	// /panel; the desktop listener serves panel.html at /.
	h := serve.NewHandler(newFakeBackend(), true)
	for _, path := range []string{"/", "/panel"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if m := cjk.FindString(rec.Body.String()); m != "" {
			t.Errorf("phone handler %s serves hard-coded %q — move it into assets/js/i18n.js", path, m)
		}
	}

	rec := httptest.NewRecorder()
	serve.NewHandler(newFakeBackend(), false).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if m := cjk.FindString(rec.Body.String()); m != "" {
		t.Errorf("desktop handler / serves hard-coded %q — move it into assets/js/i18n.js", m)
	}
}

// TestI18nLoadsFirst keeps the script order honest: the page scripts call t()
// as they run, so the dictionary and the detected language must exist before
// the first render.
func TestI18nLoadsFirst(t *testing.T) {
	for _, f := range []struct{ file, page string }{
		{"phone.html", "js/phone.js"},
		{"panel.html", "js/panel.js"},
	} {
		html := readAsset(t, f.file)
		i18n := strings.Index(html, "js/i18n.js")
		api := strings.Index(html, "js/api.js")
		page := strings.Index(html, f.page)
		if i18n < 0 || api < 0 || page < 0 || i18n > api || api > page {
			t.Errorf("%s must load js/i18n.js, then js/api.js, then %s", f.file, f.page)
		}
	}
}
