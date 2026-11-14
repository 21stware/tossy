package main

import (
	"path/filepath"
	"runtime"
	"testing"
)

// Home is cut off only at a folder boundary, in the platform's own separators and case rules.
func TestUnderHome(t *testing.T) {
	home := filepath.Join(string(filepath.Separator)+"home", "an")
	if runtime.GOOS == "windows" {
		home = `C:\Users\an`
	}
	sep := string(filepath.Separator)
	cases := []struct {
		p    string
		rest string
		ok   bool
	}{
		{home, "", true},
		{home + sep + "Downloads", sep + "Downloads", true},
		{home + "na" + sep + "x", "", false}, // a sibling whose name starts the same
		{sep + "elsewhere", "", false},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases, struct {
			p    string
			rest string
			ok   bool
		}{`c:\users\AN\Desktop`, `\Desktop`, true})
	}
	for _, c := range cases {
		if rest, ok := underHome(c.p, home); rest != c.rest || ok != c.ok {
			t.Errorf("underHome(%q) = %q, %v; want %q, %v", c.p, rest, ok, c.rest, c.ok)
		}
	}
	if _, ok := underHome(home, ""); ok {
		t.Error("an unknown home matched")
	}
}

// The bin carries the platform's name for it.
func TestTrashName(t *testing.T) {
	want := map[string]string{"darwin": "废纸篓", "windows": "回收站"}[runtime.GOOS]
	if want == "" {
		want = "回收站" // Linux desktops in Simplified Chinese
	}
	if got := trashName(); got != want {
		t.Errorf("trashName = %q, want %q", got, want)
	}
	if runtime.GOOS == "linux" {
		useLang(t, langEn)
		if got := trashName(); got != "Trash" {
			t.Errorf("English on Linux: %q", got)
		}
	}
}
