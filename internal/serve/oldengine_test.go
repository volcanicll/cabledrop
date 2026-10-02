package serve

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestPhoneCSSWorksOnOldEngines is the guard for a bug class that is invisible
// at the point of failure.
//
// The phone page renders in whatever WebView the device ships, which on the
// Android hardware this was tested against is the ROM's own AOSP WebView —
// pinned at Chrome 75 and not updatable. An engine that does not understand a
// declaration does not complain: it drops the declaration and paints something
// plausible. A dropped `inset: 0` turned the fixed body into a shrink-to-fit
// box a third of the screen wide; dropped flex `gap` glued every row together;
// a dropped `max()` inside a `padding` shorthand took the whole page gutter
// with it. Nothing anywhere said a word.
//
// So: every value that uses a modern feature must have a plain fallback for the
// same property written earlier in the same rule, so the cascade lands on
// something the old engine can use.
func TestPhoneCSSWorksOnOldEngines(t *testing.T) {
	// A declaration whose value mentions var() or env() is not validated when
	// the stylesheet is parsed: the engine keeps it and resolves it when it
	// computes the value. If the substitution then produces something the
	// engine cannot use, the property becomes `unset` — and `unset` discards
	// any fallback written above it. So for these, a preceding fallback line is
	// not enough; the whole declaration has to sit behind @supports, where an
	// engine that cannot evaluate it never sees it.
	//
	// A declaration with no var()/env() in it behaves the other way round: an
	// old engine drops it at parse time and the fallback above it stands.
	modern := []struct {
		marker string
		why    string
	}{
		{"dvh", "dvh units are Chrome 108+"},
		{"min(", "min() is Chrome 79+"},
		{"max(", "max() is Chrome 79+"},
		{"anywhere", "overflow-wrap: anywhere is Chrome 80+"},
	}
	// colour functions are banned outright here rather than merely gated: the
	// tokens file is already hex/rgba for the same reason, so a token always
	// exists that expresses the intent.
	banned := []struct {
		marker string
		why    string
	}{
		{"color-mix(", "color-mix() is Chrome 111+ and, being wrapped in var(), " +
			"fails at computed-value time rather than parse time — use a token"},
	}
	// `inset` is shorthand for four properties, so its fallback is those four
	// spelled out rather than a second `inset`.
	insetNeeds := []string{"top", "right", "bottom", "left"}

	for _, name := range []string{"base.css", "phone.css"} {
		path := filepath.Join("assets", "css", name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		css := stripCSSComments(string(raw))

		for _, rule := range rules(css) {
			selector, body := rule.selector, rule.body

			// Flex `gap` is accepted by these engines and then ignored by the
			// layout algorithm, so it cannot be feature-detected from CSS:
			// `@supports (gap: 1px)` is true on Chrome 75, because grid gap
			// exists. Requiring that every `gap` in these two files belongs to
			// a grid container declared in the same rule sidesteps the problem
			// — grid gap has been supported since Chrome 66.
			if usesProperty(body, "gap") && !hasGridDisplay(body) {
				t.Errorf("%s: rule %q sets `gap` without `display: grid`. Flex gap "+
					"landed in Chrome 84 and older engines parse the declaration and "+
					"then ignore it, so the spacing silently disappears — use margins "+
					"or `> * + *`",
					name, selector)
			}

			if usesProperty(body, "inset") {
				for _, need := range insetNeeds {
					if !usesProperty(body, need) {
						t.Errorf("%s: rule %q uses `inset` without `%s` — inset is Chrome 87+ "+
							"and a dropped inset leaves the box with no offsets at all",
							name, selector, need)
						break
					}
				}
			}

			for _, m := range banned {
				for _, decl := range declarations(body) {
					if strings.Contains(decl.value, m.marker) {
						t.Errorf("%s: rule %q sets `%s: %s` — %s",
							name, selector, decl.name, decl.value, m.why)
					}
				}
			}

			inSupports := strings.Contains(selector, "@supports")
			for _, m := range modern {
				for _, decl := range declarations(body) {
					if !strings.Contains(decl.value, m.marker) {
						continue
					}
					substituted := strings.Contains(decl.value, "var(") ||
						strings.Contains(decl.value, "env(")
					if substituted {
						if !inSupports {
							t.Errorf("%s: rule %q sets `%s: %s` using a feature from %s, "+
								"in a value that resolves var()/env() — such a declaration "+
								"is not dropped by an old engine until it computes the "+
								"value, at which point the property becomes `unset` and any "+
								"fallback above it is discarded. Put it behind @supports",
								name, selector, decl.name, decl.value, m.why)
						}
						continue
					}
					if !earlierPlainDeclaration(body, decl.name, m.marker) {
						t.Errorf("%s: rule %q sets `%s: %s` using a feature from %s, with no "+
							"plain fallback for `%s` earlier in the rule — older engines drop "+
							"it silently",
							name, selector, decl.name, decl.value, m.why, decl.name)
					}
				}
			}
		}
	}
}

type cssRule struct{ selector, body string }

// rules walks the stylesheet and yields each declaration block together with
// the selector (and any enclosing at-rule) that introduced it.
func rules(css string) []cssRule {
	var out []cssRule
	var stack []string
	start := 0
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{':
			if sel := strings.TrimSpace(css[start:i]); sel != "" {
				stack = append(stack, sel)
			} else {
				stack = append(stack, "")
			}
			start = i + 1
		case '}':
			if len(stack) > 0 {
				var named []string
				for _, s := range stack {
					if s != "" {
						named = append(named, s)
					}
				}
				out = append(out, cssRule{
					selector: strings.Join(named, " › "),
					body:     css[start:i],
				})
				stack = stack[:len(stack)-1]
			}
			start = i + 1
		}
	}
	return out
}

type cssDecl struct{ name, value string }

func declarations(body string) []cssDecl {
	var out []cssDecl
	for _, line := range strings.Split(body, ";") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k := strings.Index(line, ":")
		if k < 0 {
			continue
		}
		out = append(out, cssDecl{
			name:  strings.TrimSpace(line[:k]),
			value: strings.TrimSpace(line[k+1:]),
		})
	}
	return out
}

// earlierPlainDeclaration reports whether the property was already set in this
// rule with a value free of the given marker.
func earlierPlainDeclaration(body, name, marker string) bool {
	for _, decl := range declarations(body) {
		if decl.name != name {
			continue
		}
		if !strings.Contains(decl.value, marker) {
			return true
		}
		// First occurrence of the property already uses the marker: it is the
		// one that will be dropped, and there is nothing behind it.
		return false
	}
	return false
}

func hasGridDisplay(body string) bool {
	return regexp.MustCompile(`display\s*:\s*(inline-)?grid`).MatchString(body)
}

// usesProperty reports whether a rule sets the named property, wherever in the
// body it appears — several rules here are written one line per rule.
func usesProperty(body, name string) bool {
	for _, decl := range declarations(body) {
		if decl.name == name {
			return true
		}
	}
	return false
}

// stripCSSComments removes /* … */ so the prose explaining these very
// constraints does not trip the checks.
func stripCSSComments(css string) string {
	return regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
}
