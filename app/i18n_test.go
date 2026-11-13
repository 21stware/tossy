package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"

	"github.com/21stware/tossy/engine/safety"
)

func useLang(t *testing.T, l language) {
	t.Helper()
	old := lang
	lang = l
	t.Cleanup(func() { lang = old })
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

var verbRe = regexp.MustCompile(`%(\[\d+\])?[-+# 0]*[\d.]*[a-zA-Z]`)

// verbs are a format's verbs by argument: "%d of %s" → [d s].
func verbs(format string) []string {
	var out []string
	next := 0
	for _, m := range verbRe.FindAllStringSubmatch(format, -1) {
		if m[1] != "" {
			next, _ = strconv.Atoi(m[1][1 : len(m[1])-1])
			next--
		}
		for len(out) <= next {
			out = append(out, "")
		}
		out[next] = m[0][len(m[0])-1:]
		next++
	}
	return out
}

// Every entry is translated into all three languages, and keeps the source's format verbs.
func TestCatalogComplete(t *testing.T) {
	for k, tr := range catalog {
		if strings.HasSuffix(k, "|1") {
			base := strings.TrimSuffix(k, "|1")
			if _, ok := catalog[base]; !ok {
				t.Errorf("%q: singular of a missing entry", k)
			}
			if tr[0] == "" {
				t.Errorf("%q: no English singular", k)
			}
			if fmt.Sprint(verbs(tr[0])) != fmt.Sprint(verbs(base)) {
				t.Errorf("%q: verbs %v, source has %v", k, verbs(tr[0]), verbs(base))
			}
			continue
		}
		for i, s := range tr {
			if s == "" {
				t.Errorf("%q: no %s translation", k, [...]string{"English", "Traditional Chinese", "Japanese"}[i])
			} else if fmt.Sprint(verbs(s)) != fmt.Sprint(verbs(k)) {
				t.Errorf("%q → %q: verbs %v, source has %v", k, s, verbs(s), verbs(k))
			}
		}
		if hasHan(tr[0]) {
			t.Errorf("%q: the English translation %q has Chinese in it", k, tr[0])
		}
	}
}

// untranslated are the source strings that never reach the screen.
var untranslated = map[string]bool{
	"桌面纸篓": true, "垃圾桶": true, "大垃圾桶": true, "垃圾箱": true, // bin tiers, named for the code's reader
	"个人数据，": true, // a prefix cut off a rule's reason
}

// Every Chinese string in the app's code has its translations.
func TestSourceStringsTranslated(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || strings.HasPrefix(f, "i18n") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil || !hasHan(s) || untranslated[s] {
				return true
			}
			if _, ok := catalog[s]; !ok {
				t.Errorf("%s: %q is not in the catalog", fset.Position(lit.Pos()), s)
			}
			return true
		})
	}
}

// The engine's rule reasons and tags are translated too.
func TestRulesTranslated(t *testing.T) {
	for _, r := range safety.Rules() {
		for _, s := range []string{r.Reason, r.Tag} {
			if _, ok := catalog[s]; !ok && s != "" {
				t.Errorf("rule %s: %q is not in the catalog", r.ID, s)
			}
		}
	}
}

func TestParseLang(t *testing.T) {
	for tag, want := range map[string]language{
		"zh_CN.UTF-8": langZhHans, "zh-Hans-CN": langZhHans, "zh": langZhHans, "zh-Hans-HK": langZhHans,
		"zh_TW.UTF-8": langZhHant, "zh-Hant-HK": langZhHant, "zh-HK": langZhHant, "zh-Hant": langZhHant,
		"ja_JP.UTF-8": langJa, "ja": langJa, "en-US": langEn, "en_GB.UTF-8": langEn, "fr-FR": langEn,
	} {
		if got, ok := parseLang(tag); !ok || got != want {
			t.Errorf("parseLang(%q) = %v, %v; want %v", tag, got, ok, want)
		}
	}
	for _, tag := range []string{"", "C", "POSIX", "C.UTF-8"} {
		if _, ok := parseLang(tag); ok {
			t.Errorf("parseLang(%q) should not name a language", tag)
		}
	}
}

func TestPluralsAndLabels(t *testing.T) {
	useLang(t, langEn)
	if got := nItems(1); got != "1 item" {
		t.Errorf("nItems(1) = %q", got)
	}
	if got := nItems(1200); got != fmtNum(1200)+" items" {
		t.Errorf("nItems(1200) = %q", got)
	}
	if got := ageStr(1); got != "1 day ago" {
		t.Errorf("ageStr(1) = %q", got)
	}
	if got := habitLabel("~/Downloads 中的 .dmg 文件"); got != ".dmg files in ~/Downloads" {
		t.Errorf("habitLabel = %q", got)
	}
	if got := habitLabel("something else"); got != "something else" {
		t.Errorf("habitLabel passes unknown labels through, got %q", got)
	}
	lang = langJa
	if got := habitLabel("~/Downloads 中的文件夹"); got != "~/Downloads 内のフォルダ" {
		t.Errorf("habitLabel = %q", got)
	}
}

// The window in each language: the main view and the settings, with nothing left in the source
// language where a translation exists.
func TestLanguages(t *testing.T) {
	for _, l := range languages {
		t.Run(l.code, func(t *testing.T) {
			a := loaded(t)
			a.set.Lang = l.code
			tt := ui.NewTester(a.view, 1280, 820)
			tt.Advance(2 * time.Second)
			tt.Frame()
			tt.Frame()
			if lang != l.lang {
				t.Fatalf("lang = %v, want %v", lang, l.lang)
			}
			t.Cleanup(func() { lang = langZhHans })
			if !tt.HasText(T("自动清理")) {
				t.Errorf("no %q on screen", T("自动清理"))
			}
			if l.lang != langZhHans && tt.HasText("自动清理") && T("自动清理") != "自动清理" {
				t.Errorf("the source string is still on screen")
			}
			shot(t, tt, "lang-"+l.code)
			st := ui.NewTester(a.settingsView, 820, 580)
			st.Frame()
			for _, s := range []string{"语言", "外观", "减少动态效果"} {
				if !st.HasText(T(s)) {
					t.Errorf("settings: no %q", T(s))
				}
			}
			for _, o := range languages { // each language is offered under its own name
				if !st.HasText(o.name) {
					t.Errorf("settings: no %q", o.name)
				}
			}
			shot(t, st, "lang-"+l.code+"-settings")
		})
	}
}

// Picking a language in the settings switches the window at once and is saved.
func TestLanguageSwitch(t *testing.T) {
	a := loaded(t)
	t.Cleanup(func() { lang = langZhHans })
	a.openSettings()
	tt := ui.NewTester(a.settingsView, 820, 580)
	tt.Frame()
	if err := tt.Click("English"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if a.set.Lang != "en" || lang != langEn {
		t.Fatalf("after picking English: set.Lang = %q, lang = %v", a.set.Lang, lang)
	}
	if !tt.HasText("Language") || !tt.HasText("Appearance") {
		t.Errorf("the settings did not switch to English")
	}
	b, err := os.ReadFile(a.settingsPath())
	if err != nil || !strings.Contains(string(b), `"lang": "en"`) {
		t.Errorf("the language was not saved: %s %v", b, err)
	}
	if err := tt.Click(T("跟随系统")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if a.set.Lang != "" || lang != langZhHans { // under test the system speaks the source language
		t.Errorf("after picking System: set.Lang = %q, lang = %v", a.set.Lang, lang)
	}
}
