package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// The interface speaks Simplified Chinese, Traditional Chinese, English and Japanese. Simplified
// Chinese is the source: every string in the code is written in it and doubles as the key of
// its translations in catalog (i18n_catalog.go), gettext-style. A string without a translation
// shows as written.

type language int

const (
	langZhHans language = iota
	langEn
	langZhHant
	langJa
)

// languages are the choices in the settings, by their settings.json code and their own name.
var languages = []struct {
	code, name string
	lang       language
}{
	{"zh-Hans", "简体中文", langZhHans},
	{"zh-Hant", "繁體中文", langZhHant},
	{"en", "English", langEn},
	{"ja", "日本語", langJa},
}

// lang is the language on screen. Only the main thread reads or sets it.
var lang = langZhHans

// T translates a source string.
func T(s string) string {
	if lang == langZhHans {
		return s
	}
	if tr, ok := catalog[s]; ok && tr[lang-1] != "" {
		return tr[lang-1]
	}
	return s
}

// Tf translates a format and fills it in. Translations reorder arguments with %[n]s.
func Tf(format string, args ...any) string { return fmt.Sprintf(T(format), args...) }

// Tn is Tf for a format that counts n things: English has a singular under the key format+"|1".
func Tn(n int64, format string, args ...any) string {
	if lang == langEn && n == 1 {
		if tr, ok := catalog[format+"|1"]; ok {
			return fmt.Sprintf(tr[0], args...)
		}
	}
	return Tf(format, args...)
}

// nItems is "N 项": a count of things.
func nItems(n int64) string { return Tn(n, "%s 项", fmtNum(n)) }

// joinList joins the names in a list.
func joinList(s []string) string { return strings.Join(s, T("、")) }

// parseLang maps a locale tag ("zh_TW.UTF-8", "zh-Hant-HK", "ja-JP", "en") to a language.
func parseLang(tag string) (language, bool) {
	t := strings.ToLower(strings.ReplaceAll(tag, "_", "-"))
	if i := strings.IndexAny(t, ".@"); i >= 0 {
		t = t[:i]
	}
	switch {
	case t == "":
		return 0, false
	case strings.HasPrefix(t, "zh"):
		// the script, when written, decides ("zh-Hans-HK" is Simplified); the region only hints
		parts := strings.Split(t, "-")[1:]
		for _, p := range parts {
			switch p {
			case "hans":
				return langZhHans, true
			case "hant":
				return langZhHant, true
			}
		}
		for _, p := range parts {
			if p == "tw" || p == "hk" || p == "mo" {
				return langZhHant, true
			}
		}
		return langZhHans, true
	case strings.HasPrefix(t, "ja"):
		return langJa, true
	case t == "c" || t == "posix":
		return 0, false
	}
	return langEn, true
}

// systemLang is the first of the user's preferred languages the app speaks; English otherwise.
func systemLang() language {
	if underTest() {
		return langZhHans // the tests read the source strings
	}
	for _, tag := range systemLocales() {
		// parseLang answers English for a language the app does not speak: skip those here, so
		// that "fr, ja" picks Japanese
		if l, ok := parseLang(tag); ok && (l != langEn || strings.HasPrefix(strings.ToLower(tag), "en")) {
			return l
		}
	}
	return langEn
}

// envLocales are the POSIX locale variables, most specific first.
func envLocales() []string {
	var out []string
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			out = append(out, v)
		}
	}
	return out
}

var sysLang = -1 // systemLang, asked once

// resolveLang turns the setting ("" follows the system) into a language. TOSSY_LANG overrides.
func resolveLang(code string) language {
	if v := os.Getenv("TOSSY_LANG"); v != "" {
		code = v
	}
	for _, l := range languages {
		if l.code == code {
			return l.lang
		}
	}
	if l, ok := parseLang(code); ok {
		return l
	}
	if sysLang < 0 {
		sysLang = int(systemLang())
	}
	return language(sysLang)
}

// Habit labels are stored (habits.json, the trash journal) as the engine writes them, in the
// source language, and they identify the habit. They are translated on the way to the screen.
var habitLabelRe = []struct {
	re     *regexp.Regexp
	format string
}{
	{regexp.MustCompile(`^(.*) 中的文件夹$`), "%s 中的文件夹"},
	{regexp.MustCompile(`^(.*) 中的 \.(\S+) 文件$`), "%s 中的 .%s 文件"},
	{regexp.MustCompile(`^(.*) 中 2 个月前的内容$`), "%s 中 2 个月前的内容"},
}

func habitLabel(label string) string {
	if lang == langZhHans {
		return label
	}
	for _, h := range habitLabelRe {
		if m := h.re.FindStringSubmatch(label); m != nil {
			args := make([]any, len(m)-1)
			for i, s := range m[1:] {
				args[i] = s
			}
			return Tf(h.format, args...)
		}
	}
	return label
}
