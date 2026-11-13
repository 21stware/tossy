package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// settings persist in <state>/settings.json. Every field is comparable so the view can save
// whenever the value differs from what is on disk.
type settings struct {
	Style        string `json:"style"`
	ReduceMotion bool   `json:"reduceMotion"`
	Lang         string `json:"lang"` // "" follows the system; see languages

	Root        string `json:"root"`        // default scan root; "" = home
	Exclude     string `json:"exclude"`     // one absolute path per line
	CrossDevice bool   `json:"crossDevice"` // descend into other volumes

	Review   bool    `json:"review"`   // show the evaluation sheet before cleaning
	Analysis bool    `json:"analysis"` // show the analysis sweep
	InUseMin float64 `json:"inUseMin"` // skip items written within this many minutes
	SkipApps bool    `json:"skipApps"` // skip caches of running apps

	Habits bool `json:"habits"` // learn from manual deletions
}

func defaultSettings() settings {
	return settings{Style: palettes[0].name, Review: true, Analysis: true, InUseMin: 10, SkipApps: true, Habits: true}
}

func (a *app) settingsPath() string { return filepath.Join(stateDir(), "settings.json") }

func (a *app) loadSettings() {
	a.set = defaultSettings()
	if b, err := os.ReadFile(a.settingsPath()); err == nil {
		json.Unmarshal(b, &a.set)
	}
	a.saved = a.set
	lang = resolveLang(a.set.Lang)
	if i, _ := paletteByName(a.set.Style); i >= 0 {
		a.palIdx = i
	}
}

// syncSettings runs every frame: it folds UI state into the settings and saves on change.
func (a *app) syncSettings() {
	a.set.Style = palettes[a.palIdx].name
	if l := resolveLang(a.set.Lang); l != lang {
		lang = l
		a.prev = previewCache{} // its messages are in the old language
	}
	if a.set == a.saved {
		return
	}
	a.saved = a.set
	if os.Getenv("TOSSY_STATE") == "" && underTest() {
		return
	}
	os.MkdirAll(stateDir(), 0o755)
	b, _ := json.MarshalIndent(a.set, "", "  ")
	os.WriteFile(a.settingsPath(), b, 0o644)
}

// underTest is true under `go test` (no window), where settings must not touch the user's.
func underTest() bool {
	return strings.HasSuffix(os.Args[0], ".test") || strings.Contains(os.Args[0], "/_test/")
}

func (a *app) excludes() []string {
	var out []string
	for _, l := range strings.Split(a.set.Exclude, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if strings.HasPrefix(l, "~") {
				if h, err := os.UserHomeDir(); err == nil {
					l = filepath.Join(h, l[1:])
				}
			}
			out = append(out, l)
		}
	}
	return out
}

func (a *app) motion() bool { return a.pal.motion && !a.set.ReduceMotion }

// trashFX says whether throwing into and emptying the bin animate. E-ink keeps them, as the
// one motion it has: stepped like a panel's partial refreshes (see inkStep).
func (a *app) trashFX() bool { return (a.pal.motion || a.ink()) && !a.set.ReduceMotion }

// ink says whether the e-ink style is animating the bin in steps.
func (a *app) ink() bool { return a.pal.art == artInk && !a.set.ReduceMotion }

// inkStep is how long an e-ink frame stands: the bin's animations jump from one to the next, as
// a panel's partial refreshes do, with the last one left as a ghost.
const inkStep = 110 * time.Millisecond

// clock is the time animations read: under e-ink, the start of the current step.
func (a *app) clock(t time.Time) time.Time {
	if a.pal.art == artInk && !t.IsZero() {
		return t.Truncate(inkStep)
	}
	return t
}

// tick asks for the next frame of a running animation: at the display's rate, or under e-ink
// only when the next step is due.
func (a *app) tick(p *ui.Painter) {
	if a.ink() {
		now := p.Now()
		p.After(now.Truncate(inkStep).Add(inkStep).Sub(now))
		return
	}
	p.AnimationFrame()
}

// defaultRoot is where a fresh launch scans.
func (a *app) defaultRoot() string {
	if a.set.Root != "" {
		if st, err := os.Stat(a.set.Root); err == nil && st.IsDir() {
			return a.set.Root
		}
	}
	h, _ := os.UserHomeDir()
	return h
}

// pickFolder asks for a directory off the main thread, then calls done with it on the main thread.
func (a *app) pickFolder(title string, done func(string)) {
	if a.win == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: title, Directory: true, DefaultPath: a.root})
		if err != nil || len(paths) == 0 {
			return
		}
		a.onMain(func() { done(paths[0]) })
	}()
}

func revealLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return T("在访达中显示")
	case "windows":
		return T("在资源管理器中显示")
	}
	return T("在文件管理器中显示")
}

// ------------------------------------------------------------------------------------------
// panel

// Icons of the settings categories, in the 24-unit viewBox.
const (
	icoSliders = "M4 7h9M17 7h3M15 4.5v5M4 17h3M11 17h9M9 14.5v5"
	icoScan    = "M10.5 4.5c3.31 0 6 2.69 6 6c0 3.31-2.69 6-6 6c-3.31 0-6-2.69-6-6c0-3.31 2.69-6 6-6zM15 15l5 5"
	icoHabit   = "M4 17.5l5-5 3.5 3L20 8M15 8h5v5"
)

// settingsTabs are the categories in the left column, in order; hint is the page's subtitle.
var settingsTabs = []struct{ key, label, hint, icon string }{
	{"look", "外观", "风格与动态效果", icoSliders},
	{"scan", "扫描", "位置与排除", icoScan},
	{"clean", "清理", "流程与占用保护", icoSparkle},
	{"habits", "清理习惯", "从手动删除中学习", icoHabit},
	{"trash", "废纸篓", "恢复与记录", icoTrash},
}

// tabLabel is a category's name on screen; the trash goes by the platform's name for it.
func tabLabel(key, label string) string {
	if key == "trash" {
		return trashName()
	}
	return T(label)
}

// openSettings shows the settings window, or brings it forward when it is open.
func (a *app) openSettings() {
	a.showSettings = true
	if a.win == nil { // under test the settings view is driven directly
		return
	}
	if a.setWin != nil {
		a.setWin.Show()
		a.setWin.Focus()
		return
	}
	a.setWin = mygo.NewWindow(mygo.WindowOptions{
		Title: T("设置"), Width: 820, Height: 580, MinWidth: 720, MinHeight: 440,
		StateKey: "settings", Content: ui.View(a.settingsView),
		TitleBarStyle: mygo.TitleBarHiddenInset, DisableMaximize: true, DisableFullScreen: true,
	})
	a.setWin.OnClosed(func() {
		a.setWin, a.showSettings = nil, false
		a.win.Invalidate()
	})
}

func (a *app) closeSettings() {
	if a.setWin != nil {
		a.setWin.Close()
		return
	}
	a.showSettings = false
}

// setColors are the settings window's neutrals: greys drawn from the style's own text and
// backdrop, so every style gets a calm window and only the icons and controls carry its accent.
type setColors struct{ side, page, line, sel, hov, track, raised ui.Color }

func (a *app) setColors() setColors {
	p := a.pal
	k := setColors{
		side: p.bg.Mix(p.text, .045), page: p.bg.Mix(p.text, .015),
		line: p.text.Alpha(.09), sel: p.text.Alpha(.11), hov: p.text.Alpha(.05),
		track: p.text.Alpha(.06), raised: p.text.Alpha(.14),
	}
	if !p.dark {
		k.side, k.page = p.bg.Mix(p.text, .05), p.bg.Mix(ui.RGB(255, 255, 255), .55)
		k.sel, k.raised = p.text.Alpha(.08), ui.RGB(255, 255, 255)
	}
	if p.rule > 0 { // the same papers and rule as the main window, not greys mixed into the hue
		k.side, k.page, k.line = p.panel, p.bg, p.ink.Alpha(p.rule)
	}
	return k
}

// settingsView is the settings window: the categories on the left, the chosen one's page on the
// right. A page is a title over hairline-separated rows: what an option does on the left, its
// control on the right.
func (a *app) settingsView(c *ui.Context) {
	a.applyTheme(c)
	a.syncSettings()
	if a.win != nil {
		a.win.Invalidate() // the main window follows the style and the language
	}
	if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
		a.closeSettings()
	}
	pal, k := a.pal, a.setColors()
	if a.setTab < 0 || a.setTab >= len(settingsTabs) {
		a.setTab = 0
	}
	c.Root().Background(k.page)
	ui.Row(c).Fill().AlignItems(ui.Stretch).TextColor(pal.text).FontSize(13).Children(func() {
		a.settingsNav(c, k)
		ui.Box(c).Width(1).Shrink(0).Background(k.line)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).MinHeight(0).Children(func() {
			tab := settingsTabs[a.setTab]
			ui.Scroll(c.Key("body-" + tab.key)).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c.Key(tab.key)).Padding(max(28, c.TitleBar().Bottom), 30, 30, 30).Children(func() {
					ui.Column(c).Gap(5).DragWindow().Children(func() {
						ui.Text(c, tabLabel(tab.key, tab.label)).FontSize(22).Bold()
						ui.Text(c, T(tab.hint)).FontSize(13).TextColor(pal.t3)
					})
					ui.Box(c).Height(1).Margin(16, 0, 0, 0).Background(k.line)
					a.groupN = 0
					switch tab.key {
					case "look":
						a.setAppearance(c)
					case "scan":
						a.setScan(c)
					case "clean":
						a.setClean(c)
					case "habits":
						a.setHabits(c)
					case "trash":
						a.setTrash(c)
					}
				})
			})
		})
	})
}

// settingsNav is the left column, under the window controls: the categories with their icons,
// the version at the foot.
func (a *app) settingsNav(c *ui.Context, k setColors) {
	pal := a.pal
	top := max(20, c.TitleBar().Bottom+14)
	ui.Column(c.Key("nav")).Width(212).Shrink(0).Padding(top, 12, 16, 12).Gap(4).Background(k.side).DragWindow().Children(func() {
		for i, t := range settingsTabs {
			sel := i == a.setTab
			fg := pal.t2
			if sel {
				fg = pal.text
			}
			r := ui.Row(c.Key(t.key)).Height(38).Padding(0, 12).Gap(12).Radius(max(pal.radius, 4) + 3).AlignItems(ui.Center).
				Cursor(ui.CursorPointer).Children(func() {
				icon(c, t.icon, 18, pal.accent, 1.7)
				ui.Text(c, tabLabel(t.key, t.label)).FontSize(14.5).FontWeight(500).TextColor(fg).SingleLine().Ellipsis("…")
			})
			if sel {
				r.Background(k.sel)
			} else if r.Hovered() {
				r.Background(k.hov)
			}
			if r.Clicked() {
				a.setTab = i
			}
		}
		ui.Box(c).Grow(1)
		ui.Row(c).Padding(0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(7, 7).Radius(3.5).Background(pal.level[0])
			v := mygo.App.Version()
			if v == "" {
				v = "dev"
			}
			ui.Text(c, "Tossy "+v).FontSize(12).TextColor(pal.t3).SingleLine()
		})
	})
}

// groupTitle is a small caption over a run of rows.
func (a *app) groupTitle(c *ui.Context, title string) {
	ui.Row(c).Padding(22, 0, 4, 0).Children(func() {
		ui.Text(c, T(title)).FontSize(12).FontWeight(600).TextColor(a.pal.t3)
	})
	a.groupN = 0
}

// group is a run of related rows under an optional caption.
func (a *app) group(c *ui.Context, key, title string, fn func()) {
	ui.Column(c.Key(key)).Children(func() {
		if title != "" {
			a.groupTitle(c, title)
		}
		fn()
	})
}

// cell is one row of a page: it draws the hairline above every row but the first of a run.
func (a *app) cell(c *ui.Context, key string, fn func()) {
	if a.groupN > 0 {
		ui.Box(c).Height(1).Background(a.setColors().line)
	}
	a.groupN++
	ui.Column(c.Key(key)).Padding(16, 0).Gap(10).Children(fn)
}

// rowLabel is an option's name over what it does.
func (a *app) rowLabel(c *ui.Context, label, hint string) {
	ui.Column(c).Gap(4).Grow(1).Shrink(1).MinWidth(0).Children(func() {
		ui.Text(c, label).FontSize(14.5).FontWeight(600)
		if hint != "" {
			ui.Text(c, hint).FontSize(12.5).TextColor(a.pal.t3)
		}
	})
}

// option is one labelled control: label and hint on the left, the control on the right.
func (a *app) option(c *ui.Context, key, label, hint string, control func()) {
	a.cell(c, key, func() {
		ui.Row(c).Gap(24).AlignItems(ui.Center).Children(func() {
			a.rowLabel(c, label, hint)
			ui.Row(c).Shrink(0).Gap(6).AlignItems(ui.Center).Children(control)
		})
	})
}

// segment is one choice of a segmented control drawn by segments.
type segment struct{ key, label string }

// segments is a pill track of choices, the chosen one raised; it reports the key clicked.
func (a *app) segments(c *ui.Context, key string, items []segment, cur string) (picked string, ok bool) {
	pal, k := a.pal, a.setColors()
	square := pal.radius == 0
	rad := max(pal.radius, 4) + 4
	track := ui.Row(c.Key(key)).Padding(3).Gap(2).Radius(rad).Background(k.track).Shrink(0)
	if square { // technical styles: a ruled strip of cells, the chosen one inverted
		rad = 2
		track.Padding(0).Gap(0).Radius(0).Background(ui.Transparent).Border(1, pal.ink.Alpha(.45))
	}
	track.Children(func() {
		for i, it := range items {
			sel := it.key == cur
			s := ui.Row(c.Key("s"+it.key)).Height(30).Padding(0, 14).Radius(rad - 2).AlignItems(ui.Center).Cursor(ui.CursorPointer).Children(func() {
				t := ui.Text(c, it.label).FontSize(13.5).SingleLine()
				switch {
				case sel && square:
					t.FontWeight(600).TextColor(pal.accentText)
				case sel:
					t.FontWeight(600)
				default:
					t.TextColor(pal.t2)
				}
			})
			if square && i > 0 {
				s.BorderStartWidth(1).BorderColor(pal.ink.Alpha(.3))
			}
			if sel && square {
				s.Background(pal.accent)
			} else if sel {
				s.Background(k.raised)
			} else if s.Hovered() {
				s.Background(k.hov)
			}
			if s.Clicked() {
				picked, ok = it.key, true
			}
		}
	})
	return
}

func (a *app) setAppearance(c *ui.Context) {
	langs := []segment{{"", T("跟随系统")}}
	for _, l := range languages {
		langs = append(langs, segment{l.code, l.name}) // each in its own language
	}
	a.option(c, "lang", T("语言"), T("界面使用的语言"), func() {
		if k, ok := a.segments(c, "pick", langs, a.set.Lang); ok {
			a.set.Lang = k
		}
	})
	a.cell(c, "style", func() {
		a.rowLabel(c, T("风格"), T("整个界面的配色、扇区画法、字体与圆角"))
		a.styleCards(c)
		cur := palettes[a.palIdx]
		ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
			ui.Text(c, T(cur.label)).FontSize(13).FontWeight(600).SingleLine()
			ui.Text(c, T(cur.desc)).FontSize(12.5).TextColor(a.pal.t3).Shrink(1).MinWidth(0)
		})
	})
	a.option(c, "rm", T("减少动态效果"), T("关闭缩放过渡、开场动画与飞入废纸篓动画（墨水屏只保留分步刷新的废纸篓动画）"), func() {
		ui.Switch(c.Key("sw"), &a.set.ReduceMotion)
	})
}

// styleCards is the grid of styles, four to a row: a miniature of each in its own manner (its
// paper, its sectors, a hint of its instruments), its name under it, the chosen one ringed in
// the accent.
func (a *app) styleCards(c *ui.Context) {
	pal, k := a.pal, a.setColors()
	ui.Grid(c.Key("styles")).Columns(4).Gap(12).Padding(6, 0, 2, 0).Children(func() {
		for i, sp := range palettes {
			sel := i == a.palIdx
			rad := min(sp.radius, 8)
			card := ui.Column(c.Key(sp.name)).Gap(7).Padding(4, 4, 8, 4).Radius(rad+4).AlignItems(ui.Center).
				Cursor(ui.CursorPointer).Border(2, ui.Transparent).Children(func() {
				ui.Box(c).Fill().Height(78).Radius(rad).Background(sp.bg).Border(1, k.line).Clip().
					Draw(func(p *ui.Painter, r ui.Rect) { paintSwatch(p, r, sp) })
				ui.Row(c).Gap(6).AlignItems(ui.Baseline).Children(func() {
					t := ui.Text(c, T(sp.label)).FontSize(13.5).SingleLine()
					if sel {
						t.FontWeight(700)
					} else {
						t.FontWeight(500).TextColor(pal.t2)
					}
				})
			})
			card.Tooltip(T(sp.desc))
			if sel {
				card.Border(2, pal.accent)
			} else if card.Hovered() {
				card.Background(k.hov)
			}
			if card.Clicked() {
				a.palIdx = i
			}
		}
	})
}

// paintSwatch draws a miniature of a style: its paper, two rings of sectors painted its way, and
// the mark its instruments leave (centre lines and a title block, brackets and a prompt, ticks…).
func paintSwatch(p *ui.Painter, r ui.Rect, sp *palette) {
	cx, cy := r.X+r.W*.5, r.Y+r.H*.52
	ink := sp.ink
	switch sp.deco {
	case decoGrid:
		var g ui.Path
		for x := r.X + 4; x < r.X+r.W; x += 8 {
			g.MoveTo(x, r.Y).LineTo(x, r.Y+r.H)
		}
		for y := r.Y + 4; y < r.Y+r.H; y += 8 {
			g.MoveTo(r.X, y).LineTo(r.X+r.W, y)
		}
		p.StrokePath(&g, .6, ink.Alpha(.08))
	case decoDots:
		var d ui.Path
		for y := r.Y + 5; y < r.Y+r.H; y += 9 {
			for x := r.X + 5; x < r.X+r.W; x += 9 {
				d.Circle(x, y, .6)
			}
		}
		p.FillPath(&d, ink.Alpha(.2))
	case decoPolka:
		var d ui.Path
		for y, row := r.Y+5, 0; y < r.Y+r.H+4; y, row = y+11, row+1 {
			for x := r.X + 5 + float32(row&1)*9; x < r.X+r.W+4; x += 18 {
				d.Circle(x, y, 2)
			}
		}
		p.FillPath(&d, sp.accent.Alpha(.1))
	case decoHearts:
		var d ui.Path
		for y, row := r.Y+6, 0; y < r.Y+r.H+4; y, row = y+13, row+1 {
			for x := r.X + 6 + float32(row&1)*11; x < r.X+r.W+4; x += 22 {
				heart(&d, x, y, 2.6)
			}
		}
		p.FillPath(&d, sp.accent.Alpha(.14))
	case decoPaint:
		paintSplashes(p, r, .35, .18)
	}
	// sectors: an inner ring of five and an outer ring split under them
	r0, rw := r.H*.11, r.H*.15
	if sp.art == artBlueprint {
		var m ui.Path
		p.FillPath(circle(&m, cx, cy, r0+2*rw+2, 0, tau), sp.bg)
	}
	spans := []float32{.3, .22, .18, .14, .1}
	var b float32
	for k, sv := range spans {
		for ring := 0; ring < 2; ring++ {
			parts := 1
			if ring == 1 {
				parts = 3 - k%2
			}
			for q := 0; q < parts; q++ {
				u0 := b + sv*float32(q)/float32(parts)
				u1 := b + sv*float32(q+1)/float32(parts)
				if ring == 1 && q == parts-1 {
					u1 -= sv * .25 // the outer ring leaves a gap, as real trees do
				}
				gap := float32(.035)
				var path ui.Path
				sector(&path, cx, cy, r0+float32(ring)*rw+.6, r0+float32(ring+1)*rw-.6, u0*tau+gap/2, u1*tau-gap/2, min(sp.corner, 1.5))
				h, s, l := sp.rampAt(u0)
				l -= float32(ring) * 4
				if sp.art == artTE && k == 0 && ring == 0 {
					h, s, l = 16, 100, 58
				}
				swatchArc(p, &path, sp, h, s, l, ring == 1, k == 1 && ring == 1)
			}
		}
		b += sv
	}
	R := r0 + 2*rw
	switch sp.instr {
	case instrHUD, instrMech:
		var tk ui.Path
		for k := 0; k < 36; k++ {
			ang := float64(k) * tau / 36
			l := float32(2)
			if k%3 == 0 {
				l = 4
			}
			x0, y0 := polar(cx, cy, R+4, ang)
			x1, y1 := polar(cx, cy, R+4+l, ang)
			tk.MoveTo(x0, y0).LineTo(x1, y1)
		}
		p.StrokePath(&tk, .8, ink.Alpha(.5))
		if sp.instr == instrMech {
			for k := 0; k < 4; k++ {
				x := r.X + 8 + float32(k)*7
				var st ui.Path
				st.MoveTo(x, r.Y+14).LineTo(x+4, r.Y+8).LineTo(x+7, r.Y+8).LineTo(x+3, r.Y+14).Close()
				p.FillPath(&st, sp.amber)
			}
		}
	case instrBlueprint:
		var cl ui.Path
		cl.MoveTo(r.X+6, cy).LineTo(r.X+r.W-6, cy).MoveTo(cx, r.Y+5).LineTo(cx, r.Y+r.H-5)
		cl.Dash(0, 7, 2, 1.5, 2)
		p.StrokePath(&cl, .6, ink.Alpha(.45))
		tb := ui.Rect{X: r.X + r.W - 34, Y: r.Y + r.H - 16, W: 30, H: 12}
		p.Fill(tb, sp.bg, 0)
		var box ui.Path
		box.MoveTo(tb.X, tb.Y).LineTo(tb.X+tb.W, tb.Y).LineTo(tb.X+tb.W, tb.Y+tb.H).LineTo(tb.X, tb.Y+tb.H).Close()
		box.MoveTo(tb.X, tb.Y+5).LineTo(tb.X+tb.W, tb.Y+5)
		p.StrokePath(&box, .6, ink.Alpha(.8))
	case instrTerminal:
		var br ui.Path
		in, l := float32(5), float32(7)
		for _, c := range [4][4]float32{{r.X + in, r.Y + in, 1, 1}, {r.X + r.W - in, r.Y + in, -1, 1}, {r.X + in, r.Y + r.H - in, 1, -1}, {r.X + r.W - in, r.Y + r.H - in, -1, -1}} {
			br.MoveTo(c[0], c[1]+c[3]*l).LineTo(c[0], c[1]).LineTo(c[0]+c[2]*l, c[1])
		}
		p.StrokePath(&br, .8, ink.Alpha(.6))
		p.RichText(r.X+9, r.Y+r.H-17, 0, ui.Span{Text: ">", Size: 8, Font: "monospace", Color: ink.Alpha(.8)})
		p.Fill(ui.Rect{X: r.X + 17, Y: r.Y + r.H - 16, W: 4, H: 7}, ink.Alpha(.8), 0)
	case instrTE:
		p.FillPath((&ui.Path{}).Circle(r.X+9, r.Y+9, 2.5), sp.accent)
	}
	if sp.deco == decoScan {
		var l ui.Path
		for y := r.Y + 1; y < r.Y+r.H; y += 2 {
			l.MoveTo(r.X, y).LineTo(r.X+r.W, y)
		}
		p.StrokePath(&l, .5, ui.RGBA(0, 0, 0, .3))
	}
}

// swatchArc paints one miniature sector as paintArc would, without the chart's state.
func swatchArc(p *ui.Painter, path *ui.Path, sp *palette, h, s, l float32, leaf, mark bool) {
	switch sp.art {
	case artHolo:
		p.FillPath(path, hsl(h, s, l*.8, .26))
		p.StrokePath(path, .7, hsl(h, min(100, s+12), min(86, l+14), .9))
	case artPhosphor:
		p.FillPath(path, hsl(h, s, l*.6, .12))
		p.StrokePath(path, 2, hsl(h, s, l, .12))
		p.StrokePath(path, .7, hsl(h, s, min(80, l+12), .9))
	case artBlueprint:
		p.FillPath(path, ui.RGBA(255, 255, 255, .08))
		a := float32(.95)
		if leaf {
			a = .6
		}
		p.StrokePath(path, .7, sp.ink.Alpha(a))
	case artInk:
		p.FillPath(path, hsl(0, 0, l, 1))
		if mark {
			hatch(p, path, false, sp.ink.Alpha(.35))
		}
		p.StrokePath(path, .7, sp.ink.Alpha(.85))
	case artPlate, artTE:
		p.FillPath(path, hsl(h, s, l, 1))
		p.StrokePath(path, .6, sp.ink.Alpha(.4))
	case artToon:
		p.FillPath(path, hsl(h, s, l, 1))
		p.StrokePath(path, 1.1, sp.ink.Alpha(.9))
	default:
		p.FillPath(path, hsl(h, s, l, 1))
	}
}

func (a *app) setScan(c *ui.Context) {
	root := a.set.Root
	if root == "" {
		root = T("主目录（默认）")
	}
	a.option(c, "root", T("启动时扫描"), root, func() {
		if a.set.Root != "" && a.btn(c.Key("reset"), T("恢复默认"), btnPlain, false, nil).Clicked() {
			a.set.Root = ""
		}
		if a.btn(c.Key("pick"), T("选择…"), btnSolid, false, nil).Clicked() {
			a.pickFolder(T("选择默认扫描位置"), func(p string) { a.set.Root = p })
		}
	})
	a.option(c, "xdev", T("跨越其他磁盘"), T("扫描挂载在目录内的其他卷（默认只扫描同一磁盘，和 du -x 一致）"), func() {
		ui.Switch(c.Key("sw"), &a.set.CrossDevice)
	})
	a.cell(c, "ex", func() {
		a.rowLabel(c, T("排除的位置"), T("每行一个路径，可用 ~ 表示主目录；排除的目录只记录、不进入。下次扫描生效。"))
		ui.TextArea(c.Key("ta"), &a.set.Exclude).Height(140).Font("monospace").FontSize(12)
	})
}

func (a *app) setClean(c *ui.Context) {
	a.option(c, "review", T("清理前先复核方案"), T("分析结束后在右侧列出每一项，可逐项取消；关闭则分析后直接清理"), func() {
		ui.Switch(c.Key("sw"), &a.set.Review)
	})
	a.option(c, "anal", T("显示分析扫描"), T("清理前的扫描动画，同时检查文件占用"), func() {
		ui.Switch(c.Key("sw"), &a.set.Analysis)
	})
	a.group(c, "inuse", "占用保护", func() {
		a.option(c, "apps", T("跳过正在运行的应用的缓存"), T("如浏览器、Slack、VS Code、Xcode 正在运行，跳过它们的缓存"), func() {
			ui.Switch(c.Key("sw"), &a.set.SkipApps)
		})
		a.option(c, "recent", T("跳过最近写入的项目"), Tf("%d 分钟内有写入的缓存视为正在使用", int(a.set.InUseMin)), func() {
			ui.Slider(c.Key("sl"), &a.set.InUseMin, 0, 120).Width(180)
		})
	})
}

func (a *app) setHabits(c *ui.Context) {
	pal := a.pal
	a.option(c, "on", T("从手动删除中学习"), T("同类项目手动删除多次后，自动清理会一并包含；从废纸篓恢复会让习惯减弱"), func() {
		ui.Switch(c.Key("sw"), &a.set.Habits)
	})
	if a.habit == nil {
		return
	}
	es := a.habit.Entries()
	keys := make([]string, 0, len(es))
	for k := range es {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return es[keys[i]].D > es[keys[j]].D })
	a.group(c, "learned", "已学到的习惯", func() {
		if len(keys) == 0 {
			a.cell(c, "none", func() {
				ui.Text(c, T("还没有学到习惯")).FontSize(13).TextColor(pal.t3)
			})
		}
		for _, k := range keys {
			e := es[k]
			label := e.Label
			if label == "" {
				label = k
			}
			sub := Tf("手动删除 %d 次 · 恢复 %d 次 · 最大 %s", e.D, e.R, fmtSize(e.Max))
			if e.Seed {
				sub += T(" · 示例")
			}
			a.option(c, k, habitLabel(label), sub, func() {
				if a.btn(c.Key("forget"), T("忘记"), btnPlain, true, nil).Clicked() {
					a.habit.Forget(k)
					a.habit.Save()
					a.plan = nil
				}
			})
		}
	})
}

func (a *app) setTrash(c *ui.Context) {
	a.option(c, "how", T("工作方式"), T("放入废纸篓只是 Tossy 里的标记：文件原地不动，也不会进系统废纸篓。恢复即取消标记；清空时才在原位置永久删除被标记的项目。"), func() {})
	a.cell(c, "dir", func() {
		ui.Row(c).Gap(24).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(4).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, T("记录位置")).FontSize(14.5).FontWeight(600)
				ui.Text(c, stateDir()).FontSize(12).Font("monospace").TextColor(a.pal.t3).SingleLine().TruncateMiddle()
			})
			if a.btn(c.Key("reveal"), revealLabel(), btnSolid, false, nil).Clicked() {
				os.MkdirAll(stateDir(), 0o755)
				mygo.Shell.OpenPath(stateDir())
			}
		})
	})
}
