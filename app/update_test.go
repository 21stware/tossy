package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// fakeUpdates is an updater whose choices the test reads and changes.
type fakeUpdates struct {
	can, auto, install bool
	checks, sets       int
	last               time.Time
	available          string
}

func (f *fakeUpdates) hooks() *updates {
	return &updates{
		can:        func() bool { return f.can },
		check:      func() { f.checks++ },
		auto:       func() bool { return f.auto },
		setAuto:    func(on bool) { f.auto = on; f.sets++ },
		install:    func() bool { return f.install },
		setInstall: func(on bool) { f.install = on; f.sets++ },
		last:       func() time.Time { return f.last },
		available:  func() string { return f.available },
	}
}

// updatePage is the settings window on the page of updates.
func updatePage(t *testing.T, a *app) *ui.Tester {
	t.Helper()
	a.setTab = -1
	for i, tab := range settingsTabs {
		if tab.key == "update" {
			a.setTab = i
		}
	}
	if a.setTab < 0 {
		t.Fatal("the settings have no page of updates")
	}
	tt := ui.NewTester(a.settingsView, 820, 580)
	tt.Frame()
	return tt
}

// flip clicks the switch of the option named label, at the right end of its row.
func flip(t *testing.T, tt *ui.Tester, label, hint string) {
	t.Helper()
	l, ok1 := tt.Find(label)
	h, ok2 := tt.Find(hint)
	if !ok1 || !ok2 {
		t.Fatalf("no option %q on the page: %v", label, tt.Texts())
	}
	tt.ClickAt(820-30-16, (l.Y+h.Y+h.H)/2)
	tt.Frame()
	tt.Frame()
}

// The page of updates checks on demand, and its switches are the updater's choices, both ways.
func TestUpdateSettings(t *testing.T) {
	a := loaded(t)
	f := &fakeUpdates{can: true, auto: true}
	a.upd = f.hooks()
	tt := updatePage(t, a)
	for _, s := range []string{"Tossy dev", "还没有检查过更新", "检查更新…", "自动检查更新", "自动下载并安装"} {
		if !tt.HasText(s) {
			t.Errorf("no %q on the page: %v", s, tt.Texts())
		}
	}
	if !a.updAuto || a.updInst {
		t.Fatalf("switches show auto %v, install %v; the updater has true, false", a.updAuto, a.updInst)
	}

	if err := tt.Click("检查更新…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if f.checks != 1 {
		t.Errorf("%d checks after a click on the button", f.checks)
	}

	flip(t, tt, "自动检查更新", "每天检查一次，有新版本时询问是否安装")
	if f.auto || f.sets != 1 {
		t.Errorf("after switching checks off: updater auto %v, %d changes", f.auto, f.sets)
	}
	flip(t, tt, "自动下载并安装", "检查到新版本时直接装好，下次启动时生效")
	if !f.install || f.auto || f.sets != 2 {
		t.Errorf("after switching installs on: updater install %v, auto %v, %d changes", f.install, f.auto, f.sets)
	}

	// the update window's checkbox, or a check, changes them from outside
	f.install, f.available, f.last = false, "9.9.9", time.Now()
	tt.Frame()
	if a.updInst || f.sets != 2 {
		t.Errorf("the switch did not follow the updater: shows %v, %d changes", a.updInst, f.sets)
	}
	if !tt.HasText(Tf("有新版本 %s", "9.9.9")) {
		t.Errorf("the new version is not on the page: %v", tt.Texts())
	}
	f.available = ""
	tt.Frame()
	if want := Tf("上次检查：%s", T("今天")); !tt.HasText(want) {
		t.Errorf("no %q on the page: %v", want, tt.Texts())
	}
}

// A build that cannot replace itself says so and offers no choices; a click still checks, and
// the update window says why there is nothing to install.
func TestUpdateSettingsDisabled(t *testing.T) {
	a := loaded(t)
	f := &fakeUpdates{auto: true}
	a.upd = f.hooks()
	tt := updatePage(t, a)
	if !tt.HasText("这个版本不会自行更新：它是开发版本，或者由软件包管理器安装和更新。") || tt.HasText("自动检查更新") {
		t.Errorf("a build without updates shows: %v", tt.Texts())
	}
	if err := tt.Click("检查更新…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if f.checks != 1 || f.sets != 0 {
		t.Errorf("%d checks, %d changes", f.checks, f.sets)
	}

	a.upd = nil // no updater at all
	tt = updatePage(t, a)
	if tt.HasText("检查更新…") || !tt.HasText("Tossy dev") {
		t.Errorf("without an updater the page shows: %v", tt.Texts())
	}
}

// The page in every language, with nothing left in the source language.
func TestUpdateSettingsLanguages(t *testing.T) {
	for _, l := range languages {
		if l.lang == langZhHans {
			continue
		}
		t.Run(l.code, func(t *testing.T) {
			a := loaded(t)
			a.set.Lang = l.code
			t.Cleanup(func() { lang = langZhHans })
			f := &fakeUpdates{can: true, auto: true}
			a.upd = f.hooks()
			tt := updatePage(t, a)
			tt.Frame()
			for _, s := range []string{"版本与自动更新", "检查更新…", "还没有检查过更新", "自动检查更新", "自动下载并安装"} {
				if !tt.HasText(T(s)) {
					t.Errorf("no %q on the page: %v", T(s), tt.Texts())
				}
			}
			shot(t, tt, "update-"+l.code)
		})
	}
}
