package serve

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestPhoneJSWorksOnOldEngines is the JS twin of TestPhoneCSSWorksOnOldEngines,
// and it guards the same engine.
//
// The phone page runs in the ROM's own AOSP WebView, pinned at Chrome 75. CSS
// fails softly there — a dropped declaration paints something plausible — but
// JS does not: one unsupported token and the engine refuses to parse the whole
// file, so the page renders and then sits there dead, with nothing in the UI to
// say why. `??` in panel.js is exactly this (harmless, because the panel's real
// runtime is the desktop WebView, but it is why the check is scoped to the
// scripts the *phone* page loads).
//
// The scripts are read out of phone.html rather than listed here, so a new
// script added to that page is covered without anyone remembering to come back.
func TestPhoneJSWorksOnOldEngines(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("assets", "phone.html"))
	if err != nil {
		t.Fatalf("read assets/phone.html: %v", err)
	}

	src := regexp.MustCompile(`<script[^>]+src="([^"]+)"`).FindAllStringSubmatch(string(html), -1)
	if len(src) == 0 {
		t.Fatal("phone.html loads no scripts — the extraction pattern is wrong")
	}

	// Each entry is a token that landed after Chrome 75, and what to use instead.
	banned := []struct {
		token, why string
	}{
		{"??=", "logical nullish assignment is Chrome 85+ — write it out"},
		{"||=", "logical OR assignment is Chrome 85+ — write it out"},
		{"&&=", "logical AND assignment is Chrome 85+ — write it out"},
		{"??", "nullish coalescing is Chrome 80+ — use || or an explicit check"},
		{"?.", "optional chaining is Chrome 80+ — use && or an explicit check"},
		{"replaceAll", "String.prototype.replaceAll is Chrome 85+ — use a regex with /g"},
		{".at(", "Array.prototype.at is Chrome 92+ — index it directly"},
		{"allSettled", "Promise.allSettled is Chrome 76+ — wrap in try/catch instead"},
	}

	for _, m := range src {
		rel := strings.TrimPrefix(m[1], "/")
		path := filepath.Join("assets", filepath.FromSlash(rel))
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s (loaded by phone.html): %v", path, err)
		}
		js := stripJSComments(string(raw))
		for _, b := range banned {
			if strings.Contains(js, b.token) {
				// A token may be mentioned in prose the comment stripper kept
				// (a string literal). Say which file and token, and let the
				// author judge.
				t.Errorf("%s uses %q — %s", rel, b.token, b.why)
			}
		}
	}
}

// stripJSComments removes // and /* … */ so the prose explaining these very
// constraints does not trip the checks. It is deliberately naive about string
// literals: a token inside a string still fails the test, which is the safe
// direction to be wrong in.
func stripJSComments(js string) string {
	js = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(js, "")
	js = regexp.MustCompile(`(?m)//.*$`).ReplaceAllString(js, "")
	return js
}
