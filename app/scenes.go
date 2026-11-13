package main

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/fanfree/fanfree/engine/safety"
	"github.com/fanfree/fanfree/engine/trash"
)

// What the right panel shows.
const (
	sceneBrowse = iota
	sceneEval   // the clean plan, item by item
	sceneTrash  // what FanFree moved to the trash
)

// ------------------------------------------------------------------------------------------
// trash panel: what FanFree moved, grouped by why

var srcNames = map[trash.Source]string{trash.ByRule: "按清理规则", trash.ByManual: "你手动删除", trash.ByHabit: "依据你的历史操作"}
var srcOrder = []trash.Source{trash.ByHabit, trash.ByManual, trash.ByRule}

func (a *app) sourceColor(s trash.Source) ui.Color {
	switch s {
	case trash.ByRule:
		return a.pal.level[safety.Safe]
	case trash.ByHabit:
		return a.pal.level[safety.Review]
	}
	return a.pal.t2
}

func (a *app) trashPanel(c *ui.Context) {
	pal := a.pal
	items := a.bin.List()
	a.tsun.hlID = ""
	a.binS.rowHL = ""
	by := map[trash.Source][]trash.Item{}
	sums := map[trash.Source]int64{}
	var total int64
	for _, it := range items {
		by[it.Source] = append(by[it.Source], it)
		sums[it.Source] += it.Size
		total += it.Size
	}
	n := len(items)
	ui.Column(c.Key("trashp")).Grow(1).Shrink(1).Children(func() {
		// the header stays put: its size is the dock's, flown here (see flySize); the line under it fades in
		ui.Row(c.Key("tsh")).Padding(26, 24, 0, 28).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
				big := T("空")
				if n > 0 {
					big = fmtSize(total)
				}
				bf := float32(34)
				if a.sz == szCompact {
					bf = 26
				}
				bt := ui.Text(c.Key("tbig"), big).FontSize(bf).FontWeight(600).LetterSpacing(-.8).SingleLine()
				// the dock's size flies here (see flySize): hidden until it lands; measured from
				// last frame's layout, since a hidden element paints nothing to measure
				a.binS.bigR, a.binS.bigF = bt.Bounds(), bf
				if a.motion() && a.binS.heroK < 1 {
					bt.Opacity(0)
				}
				sub := T("删除的内容会先放在这里，清空前都可以还原")
				if n > 0 {
					sub = Tf("%s · 清空前可随时还原", nItems(int64(n)))
					if a.disk.ok && a.disk.total > 0 {
						sub += Tf(" · 占磁盘 %s", diskPct(total, a.disk.total))
					}
				}
				ui.Text(c, sub).FontSize(12).TextColor(pal.t3).Opacity(float32(easeStd(a.progress(a.panelT0, 140*time.Millisecond, 280*time.Millisecond))))
			})
		})
		if n > 0 {
			a.stagger(ui.Box(c.Key("tsb")).Height(4).Radius(2).Clip().Margin(18, 28, 6, 28).Draw(func(p *ui.Painter, r ui.Rect) {
				x := r.X
				for _, s := range []trash.Source{trash.ByRule, trash.ByManual, trash.ByHabit} {
					if sums[s] > 0 {
						w := r.W * float32(sums[s]) / float32(max(total, 1))
						p.Fill(ui.Rect{X: x, Y: r.Y, W: max(0, w-2), H: r.H}, a.sourceColor(s), 0)
						x += w
					}
				}
			}), 1)
		}
		a.stagger(ui.Scroll(c.Key("tsl")).Grow(1).Shrink(1).Children(func() {
			ui.Column(c).Padding(10, 16, 12, 16).Children(func() {
				if n == 0 {
					a.trashEmpty(c)
				}
				for _, src := range srcOrder {
					a.trashGroup(c, src, by[src], sums[src])
				}
			})
		}), 2)
		// the stage at the foot: the exit button, and under it the hero bin (painted by the dock)
		_, H := c.Size()
		ui.Column(c.Key("tstage")).Height(heroBinH(H, a.sz)+stageExtra).Shrink(0).AlignItems(ui.Center).
			Padding(a.closeTop(H), 0, 0, 0).Children(func() {
			// the close button rises out of the hero bin: clipped at the rim, it slides up once
			// the bin has grown
			ui.Box(c.Key("xclip")).Size(closeD, closeD+12).Clip().Children(func() {
				x := ui.Row(c.Key("x")).Size(closeD, closeD).Radius(closeD/2).Justify(ui.Center).AlignItems(ui.Center).
					Border(1, pal.t3.Alpha(.35)).Background(pal.panel2).Cursor(ui.CursorPointer).Tooltip(T("关闭（Esc）")).Children(func() {
					icon(c, icoClose, 14, pal.t2, 2)
				})
				if x.Hovered() {
					x.Background(pal.hov)
				}
				if x.Clicked() {
					a.goScene(sceneBrowse)
				}
				if k := a.progress(a.panelT0, 180*time.Millisecond, 360*time.Millisecond); k < 1 {
					x.Translate(0, (closeD+12)*(1-float32(easeStd(k))))
				}
			})
		})
		// 全部还原 and 清空 rise from under the window's bottom edge
		tsa := ui.Row(c.Key("tsa")).Padding(10, 24, 20, 24).AlignItems(ui.End).Draw(func(p *ui.Painter, r ui.Rect) { a.binS.tsaH = r.H })
		if k := a.progress(a.panelT0, 60*time.Millisecond, 380*time.Millisecond); k < 1 {
			tsa.Translate(0, (max(a.binS.tsaH, 50)+8)*(1-float32(easeStd(k))))
		}
		tsa.Children(func() {
			if a.btn(c.Key("all"), T("全部还原"), btnPlain, false, nil).Disabled(n == 0).Clicked() {
				ids := make([]string, n)
				for i, it := range items {
					ids[i] = it.ID
				}
				a.restore(ids)
			}
			ui.Spacer(c)
			label := T("清空")
			if n > 0 {
				label += " " + fmtSize(total)
			}
			e := a.btn(c.Key("empty"), label, btnPlain, false, nil).Disabled(n == 0 || a.trashMoving > 0 || a.emptying || !a.binS.dumpT0.IsZero()).TextColor(pal.level[safety.Risky])
			if e.Hovered() {
				e.Background(pal.level[safety.Risky].Alpha(.12))
			}
			if e.Clicked() {
				a.ask = &askState{kind: askEmpty, t0: a.now}
			}
		})
	})
}

// trashEmpty is what an empty trash says: where things come from, and how to get them there.
// The stage around the hero bin: room above the exit button, the button, a gap, the bin and
// a floor below it.
const (
	stageTop    = 28
	stageBottom = 34
	closeD      = 32 // the round close button
	stageExtra  = stageTop + closeD + 12 + stageBottom
)

// closeTop puts the close button 12pt above the hero bin as drawn, which can be shorter than
// its room when the panel's width sets its size.
func (a *app) closeTop(H float32) float32 {
	f := &a.binS
	room := heroBinH(H, a.sz)
	T := &binTiers[f.tier]
	top := bodyTop(T.body) // the rim sits on the body's top edge; the room above is the lid's
	drawn := (T.h - top + 5) * max(1, f.heroS)
	return max(stageTop, stageTop+room-drawn)
}

// bodyTop reads y from a body path's leading "Mx y".
func bodyTop(d string) float32 {
	i := 0
	for i < len(d) && d[i] != ' ' {
		i++
	}
	var y float32
	for i++; i < len(d) && d[i] >= '0' && d[i] <= '9'; i++ {
		y = y*10 + float32(d[i]-'0')
	}
	return y
}

// heroBinH is the hero bin's height in the trash view, by window height and size class.
func heroBinH(H float32, sz int) float32 {
	if sz == szCompact {
		return max(110, min(H*.22, 160))
	}
	return max(140, min(H*.26, 220))
}

func (a *app) trashEmpty(c *ui.Context) {
	pal := a.pal
	ui.Column(c.Key("tempty")).Padding(18, 12).Gap(12).Children(func() {
		for i, l := range [][2]string{
			{"自动清理", "左侧的按钮会把可安全清理的内容移到这里"},
			{"手动删除", "列表行尾的垃圾桶，或把扇区拖到这里"},
			{"随时还原", "清空之前，这里的每一项都能放回原处"},
		} {
			a.rise(ui.Row(c.Key(i)).Gap(10).AlignItems(ui.Start).Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(pal.t4).Margin(6, 0, 0, 0).Shrink(0)
				ui.Column(c).Gap(2).Shrink(1).Children(func() {
					ui.Text(c, T(l[0])).FontSize(12.5).FontWeight(600).TextColor(pal.t2)
					ui.Text(c, T(l[1])).FontSize(12).TextColor(pal.t3)
				})
			}), a.panelT0, 3+i)
		}
	})
}

func (a *app) trashGroup(c *ui.Context, src trash.Source, items []trash.Item, size int64) {
	pal := a.pal
	if len(items) == 0 {
		return
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Size > items[j].Size })
	ui.Column(c.Key(string(src))).Padding(12, 0, 0, 0).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(4, 12).FontSize(12).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(a.sourceColor(src))
			ui.Text(c, T(srcNames[src])).TextColor(pal.t2).FontWeight(500)
			ui.Text(c, nItems(int64(len(items)))).TextColor(pal.t4)
			ui.Spacer(c)
			ui.Text(c, fmtSize(size)).TextColor(pal.t3).Font(pal.mono)
		})
		if src == trash.ByHabit {
			seen := map[string]bool{}
			for _, it := range items {
				if seen[it.Habit] {
					continue
				}
				seen[it.Habit] = true
				d, seed := 0, false
				if a.habit != nil {
					for _, e := range a.habit.Entries() {
						if e.Label == it.Habit && e.D > d {
							d, seed = e.D, e.Seed
						}
					}
				}
				s := habitLabel(it.Habit)
				if d > 0 {
					s += Tf(" · 你曾手动删除 %d 个同类项", d)
					if seed {
						s += T("（示例历史）")
					}
				}
				ui.Text(c.Key("why"+it.Habit), s).FontSize(11.5).TextColor(pal.level[safety.Review].Alpha(.85)).Padding(0, 12, 4, 26)
			}
		}
		for i, it := range items {
			if i == 300 {
				ui.Text(c, Tf("另有 %d 项", len(items)-300)).FontSize(11.5).TextColor(pal.t3).Padding(0, 12, 4, 26)
				break
			}
			a.trashRow(c, it)
		}
	})
}

func (a *app) trashRow(c *ui.Context, it trash.Item) {
	pal := a.pal
	r := ui.Row(c.Key(it.ID)).Gap(10).Padding(7, 10, 7, 12).Radius(7).AlignItems(ui.Center)
	hot := r.Hovered()
	if hot {
		a.tsun.hlID = it.ID
		a.binS.rowHL = it.Orig
	}
	ball := a.binS.hov == it.Orig
	if ball && a.binS.hovNew {
		r.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollNearest, Animation: &ui.Smooth})
	}
	if hot || ball || (a.tsun.hl != nil && a.tsun.hl.hasItem(it.ID)) {
		r.Background(pal.hov)
	}
	r.Children(func() {
		lv := max(0, min(3, it.Level))
		ui.Box(c).Size(7, 7).Radius(3.5).Background(pal.level[lv]).Shrink(0)
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			ui.Text(c, filepath.Base(it.Orig)).SingleLine().Ellipsis("…")
			ui.Text(c, displayHome(it.Orig)).FontSize(10.5).Font(pal.mono).TextColor(pal.t4).SingleLine().TruncateMiddle()
		})
		ui.Text(c, fmtSize(it.Size)).FontSize(12).TextColor(pal.t2).Font(pal.mono).SingleLine()
		b := a.btn(c.Key("r"), T("还原"), btnPlain, true, nil).Height(22).Padding(0, 7)
		if !hot {
			b.Opacity(0)
		}
		if b.Clicked() {
			a.restore([]string{it.ID})
		}
	})
}
