// FanFree is a disk cleanup analyser: a drillable sunburst of what fills the disk, a list with
// safety verdicts, and an automatic clean that moves only what is safe to the trash.
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/fanfree/fanfree/engine/safety"
	"github.com/fanfree/fanfree/engine/scan"
)

func main() {
	a := newApp()
	root := a.defaultRoot()
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title: "FanFree", Width: 1280, Height: 820, MinWidth: 720, MinHeight: 620,
			StateKey: "main", Content: ui.View(a.view),
			TitleBarStyle: mygo.TitleBarHiddenInset,
		})
		a.startScan(root)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

const rightW0 = 400

// applyTheme picks up the chosen style; every window calls it each frame.
func (a *app) applyTheme(c *ui.Context) {
	if a.palIdx >= 0 && a.palIdx < len(palettes) {
		a.pal = palettes[a.palIdx]
	}
	if a.pal != a.lastPal {
		a.uiTheme = a.pal.theme()
		a.lastPal = a.pal
	}
	c.SetTheme(a.uiTheme) // per frame
}

func (a *app) view(c *ui.Context) {
	a.now = a.clock(c.Now())
	a.applyTheme(c)
	a.frame = false
	a.syncSettings()
	a.runTimers(c)
	a.tickAnalysis(c)
	for _, t := range pendingToasts {
		c.AddToast(t)
	}
	pendingToasts = nil
	if a.scanning {
		c.After(150 * time.Millisecond)
	}
	if c.Shortcut(ui.Cmd, ui.KeyComma) && a.ask == nil { // ⌘, (Ctrl+, elsewhere)
		a.openSettings()
	}
	a.keys(c)
	a.hover, a.hoverAgg, a.centerHot = scan.None, nil, false

	pal := a.pal
	c.Root().Background(pal.bg)
	ui.Column(c).Fill().TextColor(pal.text).FontSize(13).Children(func() {
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			a.sizeRight(c)
			a.leftPane(c)
			a.rightPane(c)
		})
	})
	if a.shown && a.chartR.W > 0 { // the bin, in the window's bottom-right corner
		a.dock(c)
	} else {
		a.binS.right, a.binS.hov = 0, ""
	}
	if a.tvK > 0 && a.bin != nil && a.t != nil { // the trash view's legends, from the window's edges
		a.trashLegend(c)
	}
	if a.shown { // the disk line, at the top centre of the window
		dx, _ := a.diskSpot(c)
		dw := ui.Box(c.Key("diskw")).Absolute().Top(topY(c)+1).Left(dx).ZIndex(5).Translate(0, -(topY(c)+40)*a.tvK)
		if a.tvK > .5 { // hidden in the trash view: clicks go through
			dw.PassThrough()
		}
		dw.Children(func() {
			a.diskBar(c).Opacity(1 - .82*a.askK)
		})
	}
	ui.Box(c.Key("fly")).Absolute().Top(0).Left(0).Fill().PassThrough().ZIndex(50).Draw(func(p *ui.Painter, r ui.Rect) {
		a.now = a.clock(p.Now())
		a.paintFly(p)
		a.paintDrag(p)
		a.paintInkFlash(p, r)
	})
	a.dialogs(c)
	// rebuild in step with the display (120Hz on ProMotion), not on a 16ms timer that beats
	// against the refresh and drops frames
	a.wakeForTimers(c)
	if a.frame || a.fly.active() || len(a.binS.inflight) > 0 {
		if a.ink() {
			now := c.Now()
			c.After(now.Truncate(inkStep).Add(inkStep).Sub(now))
		} else {
			c.AnimationFrame()
		}
	}
}

// keys: Esc backs out (analysis, scene, folder), Backspace goes up, Delete trashes the
// selection, Enter opens it, the arrows move through the list.
func (a *app) keys(c *ui.Context) {
	if a.t == nil || a.scanning || a.ask != nil {
		return
	}
	up := func() {
		if a.focus != 0 {
			a.zoomTo(a.t.Nodes[a.focus].Parent)
		}
	}
	if c.Shortcut(0, ui.KeyEscape) {
		switch {
		case a.anal != nil:
			a.cancelAnalysis()
		case a.scene != sceneBrowse:
			a.goScene(sceneBrowse)
		case a.fileSel(): // the detail slides back to the list
			a.selected = a.focus
		case a.pf != pfNone:
			a.pf = pfNone
		default:
			up()
		}
	}
	if a.scene != sceneBrowse || a.anal != nil {
		return
	}
	if c.Shortcut(0, ui.KeyBackspace) {
		up()
	}
	sel := a.selected
	if (c.Shortcut(0, ui.KeyDelete) || c.Shortcut(ui.Cmd, ui.KeyBackspace)) && sel > 0 && sel != a.focus && a.t.Attached(sel) {
		a.requestTrash(sel)
	}
	if c.Shortcut(0, ui.KeyEnter) && sel != scan.None && sel != a.focus && a.t.Nodes[sel].IsDir() && a.t.Nodes[sel].First != scan.None {
		a.zoomTo(sel)
	}
	if d := b2i(c.Shortcut(0, ui.KeyDown)) - b2i(c.Shortcut(0, ui.KeyUp)); d != 0 {
		rows := a.children()
		if len(rows) > 0 {
			i := -1
			for k, n := range rows {
				if n == sel {
					i = k
				}
			}
			i = max(0, min(len(rows)-1, i+d))
			if i < 0 {
				i = 0
			}
			a.selected = rows[i]
			a.list.ScrollIntoView(i)
		}
	}
}

// ------------------------------------------------------------------------------------------
// left: crumbs, chart, clean bar

func (a *app) leftPane(c *ui.Context) {
	// the trash scene quiets the left side: crumbs, tabs and the clean bar fade, a title comes in
	tv := 0.0
	if a.scene == sceneTrash && a.bin != nil && a.t != nil {
		tv = 1
	}
	a.tvOn = tv == 1
	a.tvK = max(0, min(1, float32(a.springTo(&a.tvSp, tv))))
	if a.tvK == 0 {
		a.rightW0 = a.rightW
	}
	// the chart shows safety only while the auto clean analyses (no tab for it any more)
	if a.anal != nil {
		a.mode = modeSafety
	} else if !a.keepMode {
		a.mode = modeStructure
	}
	ui.Column(c).Grow(1).Shrink(1).Children(func() {
		a.ltop(c)
		chart := ui.Box(c.Key("chart")).Grow(1).Clip().Draw(func(p *ui.Painter, r ui.Rect) {
			a.chartR = r
			a.paintSun(p, r)
		}).Children(func() {
			ui.Box(c.Key("hud")).Absolute().Top(0).Left(0).Fill().PassThrough().Draw(a.paintHUD)
			if a.mode == modeSafety && a.t != nil && a.tvK == 0 {
				a.legend(c)
			}
		})
		if a.scene == sceneTrash && a.bin != nil && a.t != nil {
			a.trashChartInput(chart)
		} else {
			a.chartInput(chart)
			if chart.PressedInside() { // the chart takes the room back
				a.pf = pfNone
			}
		}
		bot := a.lbot(c)
		if a.tvK > 0 { // the clean bar drops straight down and gives its height to the chart
			bot.Translate((a.rightW-a.refRight())/2, 90*a.tvK).Height(lbotH * (1 - a.tvK))
		}
	})
}

func (a *app) ltop(c *ui.Context) {
	pal := a.pal
	// no title bar: the top strip clears the window controls and drags the window
	bar := c.TitleBar()
	top := topY(c)
	tv := a.tvK
	// the crumbs and the trash title stop short of the disk line at the top centre
	navMax := float32(1e4)
	if a.shown {
		dx, _ := a.diskSpot(c)
		navMax = max(80, dx-18-bar.Left-16)
	}
	live := !a.tvOn && a.anal == nil
	ui.Row(c.Key("ltop")).Padding(top, 16, 0, 18+bar.Left).Gap(8).AlignItems(ui.Center).Height(30 + top).DragWindow().Children(func() {
		ui.Row(c.Key("lnav")).Gap(8).AlignItems(ui.Center).Shrink(1).MaxWidth(navMax).Clip().Translate(0, -(30+top)*tv).Children(func() {
			showUp := a.t != nil && a.focus != 0 && !a.scanning
			if showUp && !a.upOn {
				a.upT0 = a.now // the up button slides in from the left
			}
			a.upOn = showUp
			if showUp {
				up := ui.Row(c.Key("up")).Height(28).Padding(0, 9, 0, 4).Gap(2).Radius(7).AlignItems(ui.Center).
					TextColor(pal.t2).Cursor(ui.CursorPointer).Tooltip(T("返回上级（Esc）")).Shrink(0)
				up.Children(func() {
					icon(c, icoBack, 16, pal.t2, 2)
					ui.Text(c, a.name(a.t.Nodes[a.focus].Parent)).FontSize(12.5).SingleLine().MaxWidth(120).Ellipsis("…")
				})
				if k := a.progress(a.upT0, 0, 220*time.Millisecond); k < 1 {
					e := float32(easeStd(k))
					up.Translate(-(up.Bounds().W+18)*(1-e), 0)
				}
				if up.Hovered() && live {
					up.Background(pal.hov).TextColor(pal.text)
				}
				if up.Clicked() && live {
					a.zoomTo(a.t.Nodes[a.focus].Parent)
				}
			}
			a.crumbs(c)
		})
		if tv > 0 { // the trash scene's title, where the crumbs were
			ui.Row(c.Key("tvt")).Absolute().Left(18+bar.Left).MaxWidth(navMax).Top(top).Height(30).Gap(10).AlignItems(ui.Center).
				Translate(0, -(30+top)*(1-tv)).PassThrough().Children(func() {
				ui.Text(c, Tf("%s里的内容来自哪里", trashName())).FontSize(13).FontWeight(600).SingleLine().Shrink(0)
				ui.Text(c, a.trashSummary()).FontSize(12).TextColor(pal.t3).SingleLine().TruncateMiddle().Shrink(1)
			})
		}
		ui.Spacer(c)
		if a.btn(c.Key("open"), "", btnGhost, false, func() { icon(c, icoFolder, 16, pal.t2, 1.7) }).
			Tooltip(T("选择文件夹…")).Disabled(a.anal != nil).Translate(0, -(30+top)*tv).Clicked() && !a.tvOn {
			a.pickFolder(T("选择要分析的文件夹"), a.startScan)
		}
		if a.btn(c.Key("settings"), T("设置"), btnPlain, false, nil).Tooltip(Tf("设置（%s）", settingsKey())).Translate(0, -(30+top)*tv).Clicked() && !a.tvOn {
			a.openSettings()
		}
	})
}

func (a *app) crumbs(c *ui.Context) {
	pal := a.pal
	ui.Row(c.Key("crumbs")).Gap(1).AlignItems(ui.Center).Shrink(1).FontSize(12.5).Children(func() {
		if a.t == nil {
			ui.Text(c, displayRoot(a.root)).TextColor(pal.text).FontWeight(600).Padding(3, 6)
			return
		}
		ch := a.chain(a.focus)
		from := 0
		crumb := func(k int, n int32, last bool) {
			col := pal.t3
			if last {
				col = pal.text
			}
			t := ui.Text(c.Key(fmt.Sprint("c", k)), a.name(n)).TextColor(col).SingleLine().MaxWidth(170).Ellipsis("…").Padding(3, 6).Radius(5)
			if last {
				t.FontWeight(600)
				return
			}
			t.Cursor(ui.CursorPointer)
			if t.Hovered() {
				t.TextColor(pal.text).Background(pal.hov)
			}
			if t.Clicked() && a.anal == nil && !a.tvOn {
				a.zoomTo(n)
			}
		}
		sep := func(k int) { ui.Text(c.Key(fmt.Sprint("s", k)), "›").TextColor(pal.t4).Padding(0, 1) }
		if len(ch) > 5 {
			from = len(ch) - 3
			crumb(0, ch[0], false)
			sep(0)
			ui.Text(c.Key("dots"), "…").TextColor(pal.t4).Padding(3, 4)
		}
		for k := from; k < len(ch); k++ {
			if k > 0 {
				sep(k)
			}
			crumb(k, ch[k], k == len(ch)-1)
		}
	})
}

// chartInput turns pointer input over the chart into hover, drill-down and selection.
func (a *app) chartInput(chart ui.Element) {
	if a.t == nil || a.scanning || a.anal != nil {
		return
	}
	b := chart.Bounds()
	x, y, over := chart.PointerPosition()
	hit := -1
	if over {
		hit = a.sun.hit(b.X+x, b.Y+y)
		switch {
		case hit == -2:
			a.centerHot = a.focus != 0
		case hit >= 0 && hit < len(a.sun.arcs):
			if h := a.sun.arcs[hit]; h.node == scan.None {
				cp := h
				cp.path = nil
				a.hoverAgg = &cp
			} else {
				a.hover = h.node
			}
		}
	}
	if a.centerHot || a.hover != scan.None {
		chart.Cursor(ui.CursorPointer)
	}
	if chart.Clicked() {
		switch {
		case a.centerHot:
			a.zoomTo(a.t.Nodes[a.focus].Parent)
		case a.hover != scan.None:
			n := a.hover
			if a.t.Nodes[n].IsDir() && a.t.Nodes[n].First != scan.None {
				a.zoomTo(n)
			}
			a.selected = n
		case a.hoverAgg != nil:
			a.selected = a.hoverAgg.parent
		}
	}
	if chart.RightClicked() && a.hover != scan.None {
		a.selected = a.hover
	}
	chart.ContextMenu(func(m *ui.Menu) {
		n := a.hover
		if n == scan.None {
			n = a.selected
		}
		a.throwNode = scan.None // from the chart: the sector flies, not the list row
		a.nodeMenu(m, n)
	})
}

// nodeMenu is the context menu for a file or folder, on the chart and in the list.
func (a *app) nodeMenu(m *ui.Menu, n int32) {
	if a.t == nil || n == scan.None || !a.t.Attached(n) {
		return
	}
	nd := &a.t.Nodes[n]
	if nd.IsDir() && nd.First != scan.None && n != a.focus && m.Item(T("打开")).Chosen() {
		a.zoomTo(n)
	}
	path := a.t.Path(n)
	if m.Item(revealLabel()).Chosen() {
		mygo.Shell.ShowItemInFolder(path)
	}
	if m.Item(T("复制路径")).Chosen() {
		mygo.Clipboard.WriteText(path)
	}
	m.Separator()
	ok, _ := a.canTrash(n)
	if m.Item(Tf("移至%s", trashName())).Disabled(!ok).Danger().Chosen() {
		a.selected = n
		a.requestTrash(n)
	}
}

// legend sits in the chart's bottom-left corner in safety mode.
func (a *app) legend(c *ui.Context) {
	pal := a.pal
	sums := a.ev.LevelSums(a.focus)
	ui.Column(c.Key("legend")).Absolute().Left(18).Bottom(10).Gap(5).PassThrough().Children(func() {
		for lv, name := range levelTitles {
			ui.Row(c.Key(lv)).Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(8, 8).Radius(2).Background(pal.level[lv])
				ui.Text(c, name).FontSize(11.5).TextColor(pal.t2)
				ui.Text(c, fmtSize(sums[lv])).FontSize(11).Font(pal.mono).TextColor(pal.t4)
			})
		}
	})
}

// levelTitles are the four verdicts as the detail panel names them.
var levelTitles = [4]string{"可安全清理", "建议复核后清理", "个人数据 · 谨慎", "系统保护 · 禁止删除"}

// levelNames are the short forms.
var levelNames = [4]string{"可清理", "需复核", "个人数据", "系统保护"}

// lbot is the clean bar: the big 自动清理 button, centred, with what it would take on it.
// lbotH is the clean bar's height: the progress line, the padded big button.
const lbotH = 2 + 12 + 48 + 22

func (a *app) lbot(c *ui.Context) ui.Element {
	pal := a.pal
	an := a.anal
	k := float32(0)
	if an != nil {
		k = float32(an.k)
	}
	return ui.Column(c.Key("lbot")).Children(func() {
		// the analysis progress line along the top edge
		ui.Box(c.Key("anbar")).Height(2).Draw(func(p *ui.Painter, r ui.Rect) {
			if k > 0 {
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * k, H: r.H}, pal.level[safety.Safe].Alpha(.55), 0)
			}
		})
		ui.Row(c).Padding(12, 16, 22, 16).Justify(ui.Center).Shrink(0).Children(func() {
			a.bigButton(c)
		})
		if a.tvOn { // faded in the trash scene: a click here leads back, not to the hidden buttons
			if ui.Box(c.Key("lbotx")).Absolute().Top(0).Left(0).Fill().Clicked() {
				a.goScene(sceneBrowse)
			}
		}
	})
}

// bigButton is #bFree: 自动清理 with the size it frees; 停止分析 with progress while the sweep
// runs; 开始清理 for the beat between the sweep and the throw.
func (a *app) bigButton(c *ui.Context) {
	pal := a.pal
	an := a.anal
	var label, chip, tip string
	busy, disabled := false, false
	k := float32(0)
	switch {
	case an != nil && an.doneAt.IsZero():
		label, busy, k = T(an.phase())+"…", true, float32(an.k)
		chip = nItems(int64(an.found)) + " · " + fmtSize(an.foundSz)
		tip = T("停止分析（Esc）")
	case an != nil:
		label, chip = T("正在清理"), fmtSize(planSize(an.plan))+" · "+nItems(int64(len(an.plan)))
		if an.skipped > 0 {
			tip = Tf("跳过 %d 项（%s 正在使用）", an.skipped, joinList(an.apps))
		}
	default:
		items, skipped := a.idlePlan()
		if s := planSize(items); s > 0 {
			label, chip = T("自动清理"), fmtSize(s)+" · "+nItems(int64(len(items)))
			if a.sz == szCompact {
				chip = fmtSize(s)
			}
			tip = Tf("把可安全清理的内容移到%s，清空前都可以还原", trashName())
		} else {
			label, disabled = T("已清理干净"), true
		}
		if skipped > 0 {
			tip = Tf("%d 项正在使用，已跳过", skipped)
		}
		disabled = disabled || a.t == nil || a.scanning
		if a.scanning {
			label, chip = T("正在扫描…"), ""
		} else if a.t == nil {
			label, chip = T("自动清理"), ""
		}
	}
	bg, fg := pal.text, pal.bg
	if busy {
		bg, fg = pal.panel2, pal.text
	}
	if disabled {
		bg, fg = pal.panel2, pal.t3
	}
	if a.scanning { // a status line, not a button: no fill, no chip
		bg, fg = ui.Color{}, pal.t3
	}
	e := ui.Row(c.Key("bFree")).Height(48).Padding(0, 10, 0, 20).Gap(9).Radius(24).AlignItems(ui.Center).
		Background(bg).TextColor(fg).Cursor(ui.CursorPointer).Shrink(0).Clip().
		Draw(func(p *ui.Painter, r ui.Rect) {
			p.Clip(r, r.H/2, func() { // the element's Clip covers its children, not its own paint
				safe := pal.level[safety.Safe]
				if busy {
					// stripes drifting along while the sweep runs
					off := float32(p.Now().Sub(a.anal.t0)%(800*time.Millisecond)) / float32(800*time.Millisecond) * 22
					var st ui.Path
					for x := r.X - r.H - 22 + off; x < r.X+r.W+r.H; x += 22 {
						st.MoveTo(x, r.Y+r.H).LineTo(x+r.H*.47, r.Y)
					}
					p.StrokePath(&st, 10, safe.Alpha(.07))
					p.AnimationFrame()
				}
				if busy && k > 0 {
					p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * k, H: r.H}, safe.Alpha(.22), 0)
				}
				// the sheen sweeps across once each time the pointer comes over it
				if sk := a.progress(a.sheenT0, 0, 700*time.Millisecond); !a.sheenT0.IsZero() && sk < 1 {
					x := r.X + r.W*float32(-1.2+2.4*ease(sk))
					p.FillGradient(ui.Rect{X: x, Y: r.Y, W: r.W, H: r.H}, ui.LinearGradient{Angle: 105, Stops: []ui.GradientStop{
						{Color: ui.RGBA(255, 255, 255, 0), At: .35}, {Color: ui.RGBA(255, 255, 255, .55), At: .5}, {Color: ui.RGBA(255, 255, 255, 0), At: .65}}}, 0)
					p.AnimationFrame()
				}
				a.paintDust(p, r, fg)
			})
		})
	e.Children(func() {
		if busy || a.scanning {
			a.spinner(c, 13, fg)
		}
		ui.Text(c, label).FontSize(14.5).FontWeight(600).SingleLine()
		if chip != "" {
			chipBg := ui.RGBA(0, 0, 0, 1).Alpha(.11)
			if busy || disabled {
				chipBg = pal.text.Alpha(.08)
			}
			ui.Text(c, chip).FontSize(12).Font(pal.mono).Padding(4, 10).Radius(12).Background(chipBg).SingleLine()
		} else {
			ui.Box(c).Width(4)
		}
	})
	if tip != "" {
		e.Tooltip(tip)
	}
	// the press bursts: the button pops and a ring of sparks flies out from under it
	if bk := a.progress(a.burstT0, 0, 700*time.Millisecond); !a.burstT0.IsZero() && bk < 1 {
		s := 1 + .06*float32(math.Sin(math.Pi*min(1, bk*2.2)))
		e.Scale(s, s)
		ui.Box(c.Key("burst")).Absolute().Left(-90).Right(-90).Top(-70).Bottom(-70).PassThrough().ZIndex(5).
			Draw(func(p *ui.Painter, r ui.Rect) { a.paintBurst(p, r, bk) })
	}
	if disabled {
		e.Disabled(true).DimDisabled(false).Cursor(ui.CursorDefault)
		a.sheenOn = false
		a.dustHot = false
		return
	}
	hot := e.Hovered()
	if hot && !a.sheenOn && !busy {
		a.sheenT0 = a.now
	}
	a.sheenOn = hot
	a.dustHot = hot && !busy
	// a plain lift, not a coloured halo: a tight contact shadow plus a soft one further down
	shade := ui.RGBA(0, 0, 0, 1)
	switch {
	case busy:
		e.Shadow(0, 2, 6, 0, shade.Alpha(.12))
	case e.Pressed():
		e.Translate(0, 1).Scale(.985, .985).Shadow(0, 1, 3, 0, shade.Alpha(.18))
	case hot:
		e.Translate(0, -1).Shadow(0, 6, 16, -4, shade.Alpha(.22))
	default:
		e.Shadow(0, 3, 10, -3, shade.Alpha(.16))
	}
	if e.PressedInside() && !busy && an == nil && a.motion() {
		a.burstT0 = a.now
	}
	if e.Clicked() {
		a.autoClean()
	}
}

// paintBurst draws the sparks and the ring of the clean button's press, k from 0 to 1.
func (a *app) paintBurst(p *ui.Painter, r ui.Rect, k float64) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	safe := a.pal.level[safety.Safe]
	e := float32(easeOut(k))
	fade := float32(1 - easeIn(k))
	// rays from under the button, flattened to its shape: a short dash leads, and a trail of
	// dots it leaves behind. No ring.
	const n = 18
	for i := 0; i < n; i++ {
		ang := float64(i)/n*2*math.Pi + .09
		sp := float32(1)
		if i%2 == 1 {
			sp = .78
		}
		ca, sa := float32(math.Cos(ang)), float32(math.Sin(ang))*.55
		d := (70 + 120*e) * sp
		col := safe
		if i%3 == 0 {
			col = a.pal.text
		}
		var st ui.Path
		st.MoveTo(cx+ca*(d-12*fade), cy+sa*(d-12*fade)).LineTo(cx+ca*d, cy+sa*d)
		p.StrokePath(&st, 1.6, col.Alpha(.85*fade))
		var dots ui.Path
		for j := 1; j <= 3; j++ {
			dd := d - 12*fade - float32(j)*9*sp
			if dd < 60 {
				break
			}
			dots.Circle(cx+ca*dd, cy+sa*dd, 1.3-.25*float32(j))
		}
		p.FillPath(&dots, col.Alpha(.6*fade))
	}
	p.AnimationFrame()
}

// ------------------------------------------------------------------------------------------
// right: disk, list, detail, dock

// The right column is a two-dimensional accordion of three panels: the list, the selection's
// detail and the trash. The one in focus (by a press inside it) widens the column and takes the
// height; the others squeeze down to their headers. With none in focus the column is narrow
// and the panels small. Every size follows a spring, so a change of mind mid-way just bends.
const (
	pfNone = iota
	pfList
)

const (
	listMin = 64 // a squeezed list: its header
)

// Window size classes: each has its own column widths and its own density.
const (
	szCompact = iota // under 1120pt: the chart keeps the room, the column is a slim strip
	szRegular
	szWide // 1520pt and up: wider panels with an extra column of facts
)

func sizeClass(W float32) int {
	switch {
	case W < 1120:
		return szCompact
	case W >= 1520:
		return szWide
	}
	return szRegular
}

// panelWidth is one panel's width in a size class: wide when it has focus, slim when not.
// Each panel follows its own spring; the column is as wide as its widest panel, so the chart
// beside a slim panel keeps the room.
func panelWidth(sz int, focus, trash bool) float32 {
	switch {
	case focus && trash:
		return [3]float32{320, 384, 460}[sz]
	case focus:
		return [3]float32{304, 372, 460}[sz]
	}
	return [3]float32{184, 220, 256}[sz]
}

// sizeRight sets the panels' widths for this frame: none before the first scan finishes, then
// pushed in from the right.
func (a *app) sizeRight(c *ui.Context) {
	W, _ := c.Size()
	a.winW = W
	a.sz = sizeClass(W)
	if a.t != nil && !a.scanning && !a.shown {
		a.shown, a.rightEnter = true, true
		a.slimT0 = a.now
	}
	inT := a.sceneOn() == sceneTrash
	// a newly selected file opens its detail in the panel, which takes the focus to widen
	if sel := a.fileSel(); sel != a.selOn || (sel && a.selected != a.selLast) {
		if sel && !inT {
			a.pf = pfList
		}
		a.selOn, a.selLast = sel, a.selected
	}
	slim := inT || a.pf != pfList // the list in its short form unless it has the focus
	if slim != a.slim {
		a.slim = slim // a change of width only reflows the rows: no stagger
	}
	var tL, tT float32
	if a.shown {
		tL = panelWidth(a.sz, !inT && a.pf == pfList, false)
		tT = panelWidth(a.sz, inT, true)
	}
	tD := float32(0) // the detail lives in the list's panel now
	a.wL = float32(a.springTo(&a.wLsp, float64(tL)))
	a.wD = float32(a.springTo(&a.wDsp, float64(tD)))
	a.wT = float32(a.springTo(&a.wTsp, float64(tT)))
	a.wTto = tT
	a.rightTo = max(tL, tD, tT)
	if a.rightEnter { // the first push: the column slides, the panels keep their final widths
		a.rightW = max(0, float32(a.springTo(&a.rightSp, float64(a.rightTo))))
		a.wL, a.wD, a.wT = tL, tD, tT
		if math.Abs(float64(a.rightW-a.rightTo)) < .5 {
			a.rightEnter = false
		}
	} else {
		a.rightW = max(0, a.wL, a.wD, a.wT)
		a.rightSp.snap(float64(a.rightW))
	}
}

func (a *app) rightPane(c *ui.Context) {
	pal := a.pal
	_, H := c.Size()
	if a.rightW < .5 {
		return
	}
	inT := a.sceneOn() == sceneTrash
	askOn := 0.0
	if a.ask != nil {
		askOn = 1
	}
	a.askK = max(0, min(1, float32(a.springTo(&a.askSp, askOn))))
	quiet := func(el ui.Element) ui.Element {
		if a.askK > .002 {
			el.Opacity(1 - .82*a.askK).Blur(1.5 * a.askK)
		}
		return el
	}
	inner := max(a.rightW, a.rightTo*b2f(a.rightEnter))
	avail := max(0, H-3)
	// the trash panel has no height outside the trash view, and the detail none without a
	// selection; the bin rests at the bottom (see dock) and grows into the chart in the trash
	var tT float32
	if inT {
		tT = max(0, avail-listMin)
	}
	hT := max(0, float32(a.springTo(&a.hTsp, float64(tT))))
	hB := a.dockNat() * (1 - a.binS.heroK)
	hL := max(0, avail-hT-hB)
	a.rightHL = hL
	// the squeezed list and detail fade out at their lower edge instead of cutting a line in half
	fade := func(h, natural float32) { // call inside the panel
		if h >= natural {
			return
		}
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(min(22, h*.4)).PassThrough().
			Draw(func(p *ui.Painter, r ui.Rect) {
				p.FillGradient(r, ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{
					{Color: pal.bg.Alpha(0), At: 0}, {Color: pal.bg, At: 1}}}, 0)
			})
	}
	right := ui.Column(c.Key("right")).Width(a.rightW).Shrink(0).Clip()
	if pal.rule > 0 { // a ruled edge and the darker panel paper, for styles that need the boundary
		right.Background(pal.panel).BorderStartWidth(1).BorderColor(pal.ink.Alpha(pal.rule))
	}
	right.Children(func() {
		rin := ui.Column(c.Key("rin")).Width(inner).Grow(1).Shrink(0).AlignItems(ui.End)
		rin.Children(func() {
			lp := quiet(ui.Column(c.Key("plist")).Width(a.wL).Height(hL+1).Padding(1, 0, 0, 1).Shrink(0).Clip()).Children(func() {
				a.browsePanel(c)
				fade(hL, 200)
			})
			if lp.PressedInside() {
				a.focusPanel(pfList)
			}
			ui.Column(c.Key("ptrash")).Width(a.wT).Height(hT).Padding(1, 0, 0, 1).Shrink(0).Clip().Children(func() {
				if hT > 1 && a.bin != nil {
					quiet(ui.Column(c.Key("tbody")).Grow(1).Shrink(1).MinHeight(0).Clip()).Children(func() { a.trashPanel(c) })
				}
			})
		})
	})
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// rightX locates the column (for tests).
func (a *app) rightX() float32 { return a.winW - a.wL }

// focusPanel brings the list or the detail forward (leaving the trash view if it was open).
func (a *app) focusPanel(p int) {
	if a.scene == sceneTrash {
		a.goScene(sceneBrowse)
	}
	a.pf = p
}

// hasSel says whether something below the focus is selected: with nothing selected the
// selection is the focus itself, which the list's header already names.
func (a *app) hasSel() bool {
	n := a.selected
	return a.t != nil && n != scan.None && n != a.focus && a.t.Attached(n)
}

// fileSel says whether the selection is a file (or an empty folder): the panel shows its
// detail instead of the list. A folder with contents opens as the list.
func (a *app) fileSel() bool {
	if !a.hasSel() {
		return false
	}
	nd := &a.t.Nodes[a.selected]
	return !nd.IsDir() || nd.First == scan.None
}

// sceneOn is the scene the right column shows: the trash only while there is a bin.
func (a *app) sceneOn() int {
	if a.scene == sceneTrash && a.bin != nil {
		return sceneTrash
	}
	return sceneBrowse
}

func (a *app) diskBar(c *ui.Context) ui.Element {
	pal := a.pal
	if a.disk.ver != a.ver+1 || !a.disk.ok {
		a.disk.total, a.disk.free, a.disk.ok = diskUsage(a.root)
		a.disk.ver = a.ver + 1
	}
	d := a.disk
	var tr int64
	for _, e := range a.binS.ents {
		tr += e.size
	}
	used := d.total - d.free - tr
	uk, tk := 0.0, 0.0
	if d.ok && d.total > 0 {
		uk = a.tweenTo(&a.diskU, float64(used)/float64(d.total), 800*time.Millisecond)
		tk = a.tweenTo(&a.diskT, float64(tr)/float64(d.total), 800*time.Millisecond)
	}
	vol := displayRoot(a.root)
	if a.t != nil {
		vol = a.name(0)
	}
	_, dw := a.diskSpot(c)
	free := ""
	if d.ok {
		free = Tf("%s 可用", fmtSize(d.free))
		if a.sz != szCompact {
			free += " / " + fmtSize(d.total)
		}
	}
	return ui.Column(c.Key("disk")).Width(dw).Gap(5).DragWindow().Children(func() {
		ui.Row(c).AlignItems(ui.Center).Height(22).Gap(8).Children(func() {
			ui.Text(c, vol).FontSize(12.5).FontWeight(600).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(dw * .45)
			ui.Text(c, free).FontSize(12).TextColor(pal.t3).SingleLine().Shrink(1).Ellipsis("…")
			ui.Spacer(c)
			if a.btn(c.Key("rescan"), "", btnGhost, true, func() { a.rescanIcon(c) }).Tooltip(T("重新扫描")).
				Disabled(a.scanning || a.anal != nil).Clicked() {
				a.startScan(a.root)
			}
		})
		ui.Box(c.Key("bar")).Height(3).Radius(1.5).Background(pal.panel2).Clip().Draw(func(p *ui.Painter, r ui.Rect) {
			uw := r.W * float32(uk)
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: uw, H: r.H}, pal.t2, 0)
			if tk > 0 {
				p.Fill(ui.Rect{X: r.X + uw, Y: r.Y, W: max(2, r.W*float32(tk)), H: r.H}, pal.level[safety.Review], 0)
			}
			a.paintDiskDone(p, r)
		})
	})
}

// diskW is the disk line's width at the top centre, per size class.
var diskW = [3]float32{240, 300, 360}

// diskSpot places the disk line: centred over the chart's side, between the crumbs and the
// buttons on the right of the top strip.
func (a *app) diskSpot(c *ui.Context) (x, w float32) {
	W, _ := c.Size()
	lw := W - a.refRight()
	w = max(160, min(diskW[a.sz], lw-2*124))
	return (lw - w) / 2, w
}

// refRight is the right column's width that the chart side's centred parts (the disk line,
// the clean bar) line up with. While the trash view comes and goes the column widens and
// narrows under them; they hold the browse layout's place instead, so they leave straight up
// or down from where they were and come back straight to where they will rest.
func (a *app) refRight() float32 {
	switch {
	case a.tvK == 0:
		return a.rightW
	case a.tvOn:
		return a.rightW0
	}
	return a.rightTo
}

// topY is where the top strip's controls sit, clear of the window buttons.
func topY(c *ui.Context) float32 { return max(float32(14), c.TitleBar().Bottom-22) }

func (a *app) rescanIcon(c *ui.Context) {
	col := a.pal.t2
	ui.Box(c).Size(15, 15).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		s := r.W / 24
		cx, cy := r.X+12*s, r.Y+12*s
		var q ui.Path
		q.MoveTo(polar(cx, cy, 8*s, 0))
		arcTo(&q, cx, cy, 8*s, 0, tau*.84)
		p.StrokePath(q.Cap(ui.RoundCap), 1.8*s, col)
		var h ui.Path
		h.MoveTo(r.X+20*s, r.Y+4.5*s).LineTo(r.X+20*s, r.Y+9*s).LineTo(r.X+15.5*s, r.Y+9*s)
		p.StrokePath(h.Cap(ui.RoundCap), 1.8*s, col)
	})
}

// ------------------------------------------------------------------------------------------

// settingsKey is the settings shortcut as the platform writes it.
func settingsKey() string {
	if runtime.GOOS == "darwin" {
		return "⌘,"
	}
	return "Ctrl+,"
}
