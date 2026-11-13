// FanFree is a disk cleanup analyser: a drillable sunburst of what fills the disk, a list with
// safety verdicts, and an automatic clean that moves only what is safe to the trash.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/fanfree/fanfree/engine/safety"
	"github.com/fanfree/fanfree/engine/scan"
	"github.com/fanfree/fanfree/engine/trash"
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
			Title: "FanFree", Width: 1280, Height: 820, MinWidth: 960, MinHeight: 620,
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

func (a *app) view(c *ui.Context) {
	a.now = c.Now()
	if a.palIdx >= 0 && a.palIdx < len(palettes) {
		a.pal = palettes[a.palIdx]
	}
	if a.pal != a.lastPal {
		a.uiTheme = a.pal.theme()
		a.lastPal = a.pal
	}
	c.SetTheme(a.uiTheme) // per frame
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
	a.keys(c)
	a.hover, a.hoverAgg, a.centerHot = scan.None, nil, false

	pal := a.pal
	c.Root().Background(pal.bg)
	ui.Column(c).Fill().TextColor(pal.text).FontSize(13).Children(func() {
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			a.leftPane(c)
			ui.Box(c).Width(1).Background(pal.line)
			a.rightPane(c)
		})
	})
	ui.Box(c.Key("fly")).Absolute().Top(0).Left(0).Fill().PassThrough().ZIndex(50).Draw(func(p *ui.Painter, r ui.Rect) {
		a.now = p.Now()
		a.paintFly(p)
		a.paintDrag(p)
	})
	a.dialogs(c)
	if a.frame || a.fly.active() || len(a.binS.inflight) > 0 {
		c.After(16 * time.Millisecond)
	}
}

// keys: Esc backs out (analysis, scene, folder), Backspace goes up, Delete trashes the
// selection, Enter opens it, the arrows move through the list.
func (a *app) keys(c *ui.Context) {
	if a.t == nil || a.scanning || a.showSettings || a.ask != nil {
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
		case a.scene == sceneEval:
			a.closeEval()
		case a.scene != sceneBrowse:
			a.goScene(sceneBrowse)
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
	ui.Column(c).Grow(1).Shrink(1).Children(func() {
		a.ltop(c)
		chart := ui.Box(c.Key("chart")).Grow(1).Clip().Draw(a.paintSun).Children(func() {
			ui.Box(c.Key("hud")).Absolute().Top(0).Left(0).Fill().PassThrough().Draw(a.paintHUD)
			switch {
			case a.scene == sceneTrash && a.bin != nil && a.t != nil:
				a.trashLegend(c)
			case a.mode == modeSafety && a.t != nil:
				a.legend(c)
			}
		})
		if a.scene == sceneTrash && a.bin != nil && a.t != nil {
			a.trashChartInput(chart)
		} else {
			a.chartInput(chart)
		}
		a.lbot(c)
	})
}

func (a *app) ltop(c *ui.Context) {
	pal := a.pal
	// no title bar: the top strip clears the window controls and drags the window
	bar := c.TitleBar()
	top := max(float32(14), bar.Bottom-22)
	ui.Row(c.Key("ltop")).Padding(top, 16, 0, 18+bar.Left).Gap(8).AlignItems(ui.Center).Height(30+top).DragWindow().Children(func() {
		if a.t != nil && a.focus != 0 && !a.scanning {
			up := ui.Row(c.Key("up")).Height(28).Padding(0, 9, 0, 4).Gap(2).Radius(7).AlignItems(ui.Center).
				TextColor(pal.t2).Cursor(ui.CursorPointer).Tooltip("返回上级（Esc）").Shrink(0)
			up.Children(func() {
				icon(c, icoBack, 16, pal.t2, 2)
				ui.Text(c, a.name(a.t.Nodes[a.focus].Parent)).FontSize(12.5).SingleLine().MaxWidth(120).Ellipsis("…")
			})
			if up.Hovered() {
				up.Background(pal.hov).TextColor(pal.text)
			}
			if up.Clicked() && a.anal == nil {
				a.zoomTo(a.t.Nodes[a.focus].Parent)
			}
		}
		a.crumbs(c)
		ui.Spacer(c)
		ui.Segmented(c.Key("mode"), &a.mode, "目录", "安全性").Disabled(a.anal != nil)
		if a.btn(c.Key("open"), "", btnGhost, false, func() { icon(c, icoFolder, 16, pal.t2, 1.7) }).
			Tooltip("选择文件夹…").Disabled(a.anal != nil).Clicked() {
			a.pickFolder("选择要分析的文件夹", a.startScan)
		}
		if a.btn(c.Key("settings"), "设置", btnPlain, false, nil).Tooltip("设置（⌘,）").Clicked() {
			a.showSettings = true
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
			if t.Clicked() && a.anal == nil {
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
			if a.t.Nodes[n].IsDir() && a.t.Nodes[n].First != scan.None && a.scene != sceneEval {
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
	if nd.IsDir() && nd.First != scan.None && n != a.focus && m.Item("打开").Chosen() {
		a.zoomTo(n)
	}
	path := a.t.Path(n)
	if m.Item(revealLabel()).Chosen() {
		mygo.Shell.ShowItemInFolder(path)
	}
	if m.Item("复制路径").Chosen() {
		mygo.Clipboard.WriteText(path)
	}
	m.Separator()
	ok, _ := a.canTrash(n)
	if m.Item("移至" + trashName()).Disabled(!ok).Danger().Chosen() {
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

// lbot is the clean bar: what can go, 查看评估 and the big 自动清理 button.
func (a *app) lbot(c *ui.Context) {
	pal := a.pal
	an := a.anal
	k := float32(0)
	if an != nil {
		k = float32(an.k)
	}
	ui.Column(c.Key("lbot")).Children(func() {
		// the analysis progress line along the top edge
		ui.Box(c.Key("anbar")).Height(2).Draw(func(p *ui.Painter, r ui.Rect) {
			if k > 0 {
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * k, H: r.H}, pal.level[safety.Safe].Alpha(.55), 0)
			}
		})
		ui.Row(c).Padding(10, 16, 16, 20).Gap(10).AlignItems(ui.Center).Children(func() {
			a.cleanSum(c)
			if a.btn(c.Key("eval"), "查看评估", btnPlain, false, nil).Disabled(a.t == nil || a.scanning || an != nil).Clicked() {
				a.evaluate()
			}
			a.bigButton(c)
		})
	})
}

func (a *app) cleanSum(c *ui.Context) {
	pal := a.pal
	dot := func(col ui.Color) { ui.Box(c).Size(7, 7).Radius(3.5).Background(col).Shrink(0) }
	b := func(s string) ui.Element {
		return ui.Text(c, s).FontSize(15).FontWeight(600).TextColor(pal.text).SingleLine()
	}
	t := func(s string) ui.Element { return ui.Text(c, s).TextColor(pal.t3).SingleLine().Shrink(1).Ellipsis("…") }
	ui.Row(c.Key("cs")).Grow(1).Shrink(1).Gap(7).AlignItems(ui.Center).FontSize(12.5).Children(func() {
		an := a.anal
		switch {
		case a.scanning:
			a.spinner(c, 11, pal.t3)
			t("正在扫描…")
		case a.t == nil:
			dot(pal.t4)
			t("没有扫描结果")
		case an != nil && an.doneAt.IsZero():
			a.spinner(c, 11, pal.level[safety.Safe])
			ui.Text(c, an.phase()+"…").TextColor(pal.t2).SingleLine()
			t(fmt.Sprintf("已发现 %s 项 · %s", fmtNum(int64(an.found)), fmtSize(an.foundSz)))
		case an != nil:
			dot(pal.level[safety.Safe])
			b(fmtSize(planSize(an.plan)))
			nh := 0
			for _, it := range an.plan {
				if it.meta.Source == trash.ByHabit {
					nh++
				}
			}
			s := fmtNum(int64(len(an.plan))) + " 项"
			ui.Text(c, s).TextColor(pal.t3).SingleLine()
			if nh > 0 {
				ui.Text(c, fmt.Sprintf("· %d 项依据你的习惯", nh)).TextColor(pal.level[safety.Review]).SingleLine()
			}
			if an.skipped > 0 {
				t(fmt.Sprintf("· 跳过 %d 项（%s 正在使用）", an.skipped, joinCN(an.apps)))
			}
		default:
			items, busy := a.idlePlan()
			if s := planSize(items); s > 0 {
				dot(pal.level[safety.Safe])
				b(fmtSize(s))
				t("可安全清理 · " + fmtNum(int64(len(items))) + " 项")
			} else {
				dot(pal.t4)
				msg := "没有可安全清理的内容"
				if busy > 0 {
					msg += fmt.Sprintf("（%d 项正在使用，已跳过）", busy)
				}
				t(msg)
			}
		}
	})
}

func joinCN(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += "、"
		}
		out += x
	}
	return out
}

// bigButton is #bFree: 自动清理 with the size it frees; 停止分析 with progress while the sweep
// runs; 开始清理 for the beat between the sweep and the throw.
func (a *app) bigButton(c *ui.Context) {
	pal := a.pal
	an := a.anal
	var label, chip string
	busy, disabled := false, false
	k := float32(0)
	switch {
	case an != nil && an.doneAt.IsZero():
		label, chip, busy, k = "停止分析", fmt.Sprintf("%d%%", int(an.k*100)), true, float32(an.k)
	case an != nil:
		label, chip = "开始清理", fmtSize(planSize(an.plan))
	default:
		items, _ := a.idlePlan()
		if s := planSize(items); s > 0 {
			label, chip = "自动清理", fmtSize(s)
		} else {
			label, disabled = "已清理干净", true
		}
		disabled = disabled || a.t == nil || a.scanning
		if a.scanning || a.t == nil {
			label, chip = "自动清理", ""
		}
	}
	bg, fg := pal.text, pal.bg
	if busy {
		bg, fg = pal.panel2, pal.text
	}
	if disabled {
		bg, fg = pal.panel2, pal.t3
	}
	e := ui.Row(c.Key("bFree")).Height(46).Padding(0, 14, 0, 16).Gap(8).Radius(13).AlignItems(ui.Center).
		Background(bg).TextColor(fg).Cursor(ui.CursorPointer).Shrink(0).Clip().
		Draw(func(p *ui.Painter, r ui.Rect) {
			if busy && k > 0 {
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * k, H: r.H}, pal.level[safety.Safe].Alpha(.22), 0)
			}
		})
	e.Children(func() {
		icon(c, icoSparkle, 17, fg, 1.9)
		ui.Text(c, label).FontSize(14.5).FontWeight(600).SingleLine()
		if chip != "" {
			chipBg := ui.RGBA(0, 0, 0, 1).Alpha(.11)
			if busy || disabled {
				chipBg = pal.text.Alpha(.08)
			}
			ui.Text(c, chip).FontSize(12).Font(pal.mono).Padding(3, 7).Radius(8).Background(chipBg).SingleLine()
		}
	})
	if disabled {
		e.Disabled(true).DimDisabled(false).Cursor(ui.CursorDefault)
		return
	}
	if e.Pressed() {
		e.Scale(.985, .985)
	} else if e.Hovered() {
		e.Translate(0, -1).Shadow(0, 6, 16, -6, ui.RGBA(0, 0, 0, 1).Alpha(.35))
	}
	if e.Clicked() {
		a.autoClean()
	}
}

// ------------------------------------------------------------------------------------------
// right: disk, list, detail, dock

func (a *app) rightPane(c *ui.Context) {
	pal := a.pal
	W, _ := c.Size()
	tw := float64(rightW0)
	if a.scene == sceneTrash && a.bin != nil {
		tw = float64(max(340, min(W*.46, 640)))
	}
	a.rightW = float32(a.tweenTo(&a.rightTw, tw, 550*time.Millisecond))
	ui.Column(c.Key("right")).Width(a.rightW).Shrink(0).Background(pal.panel).Children(func() {
		a.diskBar(c)
		ui.Box(c).Height(1).Background(pal.line)
		ui.Column(c.Key("scene")).Grow(1).Shrink(1).Clip().Children(func() {
			switch {
			case a.scene == sceneEval && a.eval != nil && a.t != nil:
				a.evalPanel(c)
			case a.scene == sceneTrash && a.bin != nil:
				a.trashPanel(c)
			default:
				a.browsePanel(c)
			}
		})
		ui.Box(c).Height(1).Background(pal.line)
		a.dock(c)
	})
}

func (a *app) diskBar(c *ui.Context) {
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
	bar := c.TitleBar()
	ui.Column(c.Key("disk")).Padding(16, 18+bar.Right, 14, 18).Gap(9).DragWindow().Children(func() {
		ui.Row(c).AlignItems(ui.Center).Height(26).Children(func() {
			ui.Text(c, vol).FontWeight(600).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
			if a.btn(c.Key("rescan"), "", btnGhost, true, func() { a.rescanIcon(c) }).Tooltip("重新扫描").
				Disabled(a.scanning || a.anal != nil).Clicked() {
				a.startScan(a.root)
			}
		})
		ui.Box(c.Key("bar")).Height(4).Radius(2).Background(pal.panel2).Clip().Draw(func(p *ui.Painter, r ui.Rect) {
			uw := r.W * float32(uk)
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: uw, H: r.H}, pal.t2, 0)
			if tk > 0 {
				p.Fill(ui.Rect{X: r.X + uw, Y: r.Y, W: max(2, r.W*float32(tk)), H: r.H}, pal.level[safety.Review], 0)
			}
		})
		ui.Row(c).Gap(4).FontSize(12).Children(func() {
			if !d.ok {
				ui.Text(c, "无法读取磁盘容量").TextColor(pal.t3)
				return
			}
			ui.Text(c, fmtSize(d.free)).FontWeight(600).TextColor(pal.t2)
			ui.Text(c, "可用，共 "+fmtSize(d.total)).TextColor(pal.t3)
		})
	})
}

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

