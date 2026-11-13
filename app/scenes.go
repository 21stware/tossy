package main

import (
	"fmt"
	"path/filepath"
	"sort"

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
		a.stagger(ui.Row(c.Key("tsh")).Padding(16, 14, 0, 24).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
				ui.Text(c, trashName()+" · "+binTiers[a.binS.tier].name).FontSize(12).TextColor(pal.t3)
				big := "空"
				if n > 0 {
					big = fmtSize(total)
				}
				bf := float32(34)
				if a.sz == szCompact {
					bf = 26
				}
				ui.Text(c, big).FontSize(bf).FontWeight(600).LetterSpacing(-.8)
				sub := "删除的内容会先放在这里，清空前都可以还原"
				if n > 0 {
					sub = fmt.Sprintf("%s 项 · 清空前可随时还原", fmtNum(int64(n)))
					if a.disk.ok && a.disk.total > 0 {
						sub += " · 占磁盘 "+diskPct(total, a.disk.total)
					}
				}
				ui.Text(c, sub).FontSize(12).TextColor(pal.t3)
			})
			if a.btn(c.Key("x"), "", btnGhost, false, func() { icon(c, icoClose, 16, pal.t3, 2) }).Tooltip("关闭（Esc）").Clicked() {
				a.goScene(sceneBrowse)
			}
		}), 0)
		if n > 0 {
			a.stagger(ui.Box(c.Key("tsb")).Height(4).Radius(2).Clip().Margin(16, 24, 4, 24).Draw(func(p *ui.Painter, r ui.Rect) {
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
			ui.Column(c).Padding(6, 12, 10, 12).Children(func() {
				if n == 0 {
					a.trashEmpty(c)
				}
				for _, src := range srcOrder {
					a.trashGroup(c, src, by[src], sums[src])
				}
			})
		}), 2)
		a.stagger(ui.Row(c.Key("tsa")).Padding(8, 18, 14, 18).AlignItems(ui.End).Children(func() {
			if a.btn(c.Key("all"), "全部还原", btnPlain, false, nil).Disabled(n == 0).Clicked() {
				ids := make([]string, n)
				for i, it := range items {
					ids[i] = it.ID
				}
				a.restore(ids)
			}
			ui.Spacer(c)
			label := "清空"
			if n > 0 {
				label += " " + fmtSize(total)
			}
			e := a.btn(c.Key("empty"), label, btnPlain, false, nil).Disabled(n == 0 || a.trashMoving > 0).TextColor(pal.level[safety.Risky])
			if e.Hovered() {
				e.Background(pal.level[safety.Risky].Alpha(.12))
			}
			if e.Clicked() {
				a.ask = &askState{kind: askEmpty, t0: a.now}
			}
		}), 3)
	})
}

// trashEmpty is what an empty trash says: where things come from, and how to get them there.
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
					ui.Text(c, l[0]).FontSize(12.5).FontWeight(600).TextColor(pal.t2)
					ui.Text(c, l[1]).FontSize(12).TextColor(pal.t3)
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
			ui.Text(c, srcNames[src]).TextColor(pal.t2).FontWeight(500)
			ui.Text(c, fmt.Sprintf("%d 项", len(items))).TextColor(pal.t4)
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
				s := it.Habit
				if d > 0 {
					s += fmt.Sprintf(" · 你曾手动删除 %d 个同类项", d)
					if seed {
						s += "（示例历史）"
					}
				}
				ui.Text(c.Key("why"+it.Habit), s).FontSize(11.5).TextColor(pal.level[safety.Review].Alpha(.85)).Padding(0, 12, 4, 26)
			}
		}
		for i, it := range items {
			if i == 300 {
				ui.Text(c, fmt.Sprintf("另有 %d 项", len(items)-300)).FontSize(11.5).TextColor(pal.t3).Padding(0, 12, 4, 26)
				break
			}
			a.trashRow(c, it)
		}
	})
}

func (a *app) trashRow(c *ui.Context, it trash.Item) {
	pal := a.pal
	r := ui.Row(c.Key(it.ID)).Gap(10).Padding(5, 8, 5, 12).Radius(7).AlignItems(ui.Center)
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
		b := a.btn(c.Key("r"), "还原", btnPlain, true, nil).Height(22).Padding(0, 7)
		if !hot {
			b.Opacity(0)
		}
		if b.Clicked() {
			a.restore([]string{it.ID})
		}
	})
}
