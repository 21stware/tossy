package main

import (
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

// Tossy updates itself from the signed releases on GitHub (updates in mygo.json). MyGo's updater
// plugin checks in the background, shows a new version's notes and installs it; its window is
// drawn in native UI, as the app has no webview. The app adds "Check for Updates…" to the menu
// and a page of the settings.

// updates is what the settings page asks of the updater. Tests put their own in.
type updates struct {
	can        func() bool // this build can replace itself: not a development build, not a .deb or .rpm
	check      func()      // as the user asked: the update window shows at once
	auto       func() bool // check in the background
	setAuto    func(bool)
	install    func() bool // install what a background check finds, without asking
	setInstall func(bool)
	last       func() time.Time // the last successful check
	available  func() string    // the newer version it found, "" for none
}

// useUpdater adds the updater to the app. Call it before App.Run.
func (a *app) useUpdater() {
	// The window speaks the language the app launched in: MyGo's own locale knows nothing of
	// the app's catalog.
	code := ""
	for _, l := range languages {
		if l.lang == lang {
			code = l.code
		}
	}
	mygo.Use(native.New(updater.Options{Language: code}))

	var once sync.Once
	var can bool
	a.upd = &updates{
		// Enabled probes the install directory: ask once
		can:        func() bool { once.Do(func() { can = mygo.Updater.Enabled() }); return can },
		check:      updater.CheckForUpdates,
		auto:       updater.AutomaticChecks,
		setAuto:    updater.SetAutomaticChecks,
		install:    updater.AutomaticDownloads,
		setInstall: updater.SetAutomaticDownloads,
		last:       updater.LastCheck,
		available:  updater.AvailableVersion,
	}
	updater.OnChange(func() { // a check ended, or the update window changed a choice
		if a.setWin != nil {
			a.setWin.Invalidate()
		}
	})

	if runtime.GOOS == "darwin" { // the default menu bar, with the item after About
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Label: "Tossy", Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleAbout},
				updater.MenuItem(),
				mygo.Separator(),
				{Role: mygo.RoleServices},
				mygo.Separator(),
				{Role: mygo.RoleHide},
				{Role: mygo.RoleHideOthers},
				{Role: mygo.RoleUnhide},
				mygo.Separator(),
				{Role: mygo.RoleQuit},
			}},
			{Role: mygo.RoleFileMenu},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleViewMenu},
			{Role: mygo.RoleWindowMenu},
		}))
	}
}

const icoUpdate = "M12 4v10.5M7.5 10l4.5 4.5 4.5-4.5M5 19.5h14"

// appVersion is the version on screen; a build outside `mygo build` has none.
func appVersion() string {
	if v := mygo.App.Version(); v != "" {
		return v
	}
	return "dev"
}

// setUpdate is the settings page of updates: the version with a check on demand, and what
// happens in the background.
func (a *app) setUpdate(c *ui.Context) {
	u := a.upd
	state := T("还没有检查过更新")
	if u != nil {
		if v := u.available(); v != "" {
			state = Tf("有新版本 %s", v)
		} else if t := u.last(); !t.IsZero() {
			state = Tf("上次检查：%s", ageStr(int(time.Since(t).Hours()/24)))
		}
	}
	a.option(c, "ver", "Tossy "+appVersion(), state, func() {
		if u != nil && a.btn(c.Key("check"), T("检查更新…"), btnSolid, false, nil).Clicked() {
			u.check()
		}
	})
	if u == nil || !u.can() {
		a.cell(c, "off", func() {
			ui.Text(c, T("这个版本不会自行更新：它是开发版本，或者由软件包管理器安装和更新。")).FontSize(12.5).TextColor(a.pal.t3)
		})
		return
	}
	// The switches hold a.updAuto and a.updInst. One that differs from what it showed in the
	// last frame was flipped by the user; otherwise both follow the updater, whose own window
	// changes them too.
	if a.updShown {
		if a.updAuto != a.updWas[0] {
			u.setAuto(a.updAuto)
		}
		if a.updInst != a.updWas[1] {
			u.setInstall(a.updInst)
		}
	}
	a.updAuto, a.updInst = u.auto(), u.install()
	a.updWas, a.updShown = [2]bool{a.updAuto, a.updInst}, true
	a.group(c, "bg", "后台", func() {
		a.option(c, "auto", T("自动检查更新"), T("每天检查一次，有新版本时询问是否安装"), func() {
			ui.Switch(c.Key("sw"), &a.updAuto)
		})
		a.option(c, "inst", T("自动下载并安装"), T("检查到新版本时直接装好，下次启动时生效"), func() {
			ui.Switch(c.Key("sw"), &a.updInst)
		})
	})
}
