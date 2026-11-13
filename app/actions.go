package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/fanfree/fanfree/engine/safety"
	"github.com/fanfree/fanfree/engine/scan"
	"github.com/fanfree/fanfree/engine/trash"
)

// ------------------------------------------------------------------------------------------
// manual delete

// requestTrash deletes one item by hand: protected items shake and say why, personal data asks
// first, the rest go at once with an undo.
func (a *app) requestTrash(n int32) {
	if a.t == nil || n <= 0 || !a.t.Attached(n) || a.anal != nil {
		return
	}
	v := a.verdict(n)
	if ok, why := a.canTrash(n); !ok {
		a.shake(n)
		msg := why
		if v.Level() == safety.Protected {
			msg = "「" + a.name(n) + "」受系统保护：" + v.Reason()
		}
		a.toast(ui.Toast{Title: msg})
		return
	}
	if v.Level() == safety.Risky {
		a.ask = &askState{kind: askRisky, node: n, t0: a.now}
		return
	}
	a.trashOne(n)
}

func (a *app) trashOne(n int32) {
	nd := &a.t.Nodes[n]
	name, size := a.name(n), nd.Size
	it := cleanItem{n, trash.Meta{Size: size, Dir: nd.IsDir(), Source: trash.ByManual, Level: int(a.verdict(n).Level())}, ""}
	b := a.trashNodes([]cleanItem{it}, false)
	a.toast(ui.Toast{Title: fmt.Sprintf("已将「%s」移至%s（%s）", name, trashName(), fmtSize(size)),
		Action: "撤销", OnAction: func() { a.undo(b) }})
}

func (a *app) shake(n int32) { a.shakeN, a.shakeT0 = n, a.now }

func (a *app) shakeX(n int32) float32 {
	if n != a.shakeN || a.shakeT0.IsZero() {
		return 0
	}
	k := a.progress(a.shakeT0, 0, 320*time.Millisecond)
	if k >= 1 {
		return 0
	}
	return float32(math.Sin(k*math.Pi*6) * 6 * (1 - k))
}

// ------------------------------------------------------------------------------------------
// drag a row onto the bin

type dragState struct {
	node   int32
	press  bool
	acc    float32 // distance moved, to tell a drag from a click
	on     bool
	x, y   float32 // the pointer, in the window
	d      float32 // ball diameter
	c      ui.Color
	hot    bool
	home   ui.Rect // the row, for the ball's way back
	back   time.Time
	bx, by float32 // where the way back starts
}

// dropSrc hands a dropped ball to the throw, so it flies on from where it was let go.
type dropSrc struct {
	node int32
	x, y float32
	c    ui.Color
	set  bool
}

func (a *app) rowDrag(r ui.Element, n int32) {
	d := &a.drag
	dx, dy, held := r.Dragged()
	if held && a.anal == nil {
		if !d.press || d.node != n {
			px, py, _ := r.PointerPosition()
			b := r.Bounds()
			*d = dragState{node: n, press: true, x: b.X + px - dx, y: b.Y + py - dy, home: b}
		}
		d.x += dx
		d.y += dy
		d.acc += float32(math.Abs(float64(dx)) + math.Abs(float64(dy)))
		if !d.on && d.acc > 6 {
			if ok, _ := a.canTrash(n); ok {
				d.on = true
				d.c = a.swatchColor(n)
				d.d = max(14, min(28, ballR(a.t.Nodes[n].Size, a.binS.tier)*3.4))
			}
		}
		if d.on {
			f := &a.binS
			T := &binTiers[f.tier]
			ox, oy := f.origin()
			d.hot = d.x >= ox-14 && d.x <= ox+T.w+14 && d.y >= oy-20 && d.y <= oy+T.h+14
			f.hot = d.hot
			r.Cursor(ui.CursorGrabbing)
			a.frame = true
		}
	}
	if _, _, ended := r.DragEnded(); ended && d.node == n {
		a.binS.hot = false
		if d.on && d.hot && a.verdict(n).Level() != safety.Risky {
			a.drop = dropSrc{n, d.x, d.y, d.c, true}
			*d = dragState{}
			a.selected = n
			a.requestTrash(n)
			return
		}
		if d.on {
			if d.hot {
				a.requestTrash(n) // personal data: ask first; the ball goes back meanwhile
			}
			d.back, d.bx, d.by, d.on, d.press = a.now, d.x, d.y, false, false
			return
		}
		*d = dragState{back: d.back, bx: d.bx, by: d.by, home: d.home, node: d.node, c: d.c, d: d.d}
	}
	if !held && d.press && d.node == n {
		d.press, d.on = false, false
		a.binS.hot = false
	}
}

// paintDrag draws the ball under the pointer, or on its way back to the row.
func (a *app) paintDrag(p *ui.Painter) {
	d := &a.drag
	if d.on {
		a.paintBall(p, d.x, d.y, d.d/2, d.c, 1)
		return
	}
	if d.back.IsZero() {
		return
	}
	k := float64(p.Now().Sub(d.back)) / float64(200*time.Millisecond)
	if k >= 1 || !a.motion() {
		d.back = time.Time{}
		return
	}
	e := float32(easeStd(k))
	hx, hy := d.home.X+d.home.W/2, d.home.Y+d.home.H/2
	a.paintBall(p, d.bx+(hx-d.bx)*e, d.by+(hy-d.by)*e, d.d/2*(1-.6*e), d.c, 1-e)
	p.AnimationFrame()
}

// ------------------------------------------------------------------------------------------
// the dock

// landed is what the dock counts: trash entries whose ball is no longer in the air.
func (a *app) landed(ents []pileEnt) (n int, size int64) {
	for _, e := range ents {
		if a.binS.inflight[e.key] > 0 || (e.id == "" && a.motion() && a.binS.landed[e.key].IsZero()) {
			continue
		}
		n++
		size += e.size
	}
	return
}

func (a *app) dock(c *ui.Context) {
	pal := a.pal
	f := &a.binS
	ents := a.trashEnts()
	a.syncBin(ents)
	total := len(ents)
	n, size := a.landed(ents)
	T := &binTiers[f.tier]
	// the trash scene zooms the bin into a hero at the column's bottom centre
	inT := a.scene == sceneTrash && a.bin != nil
	W, H := c.Size()
	hk := float32(0)
	if inT {
		hk = 1
	}
	k := float32(a.tweenTo(&f.heroTw, float64(hk), 550*time.Millisecond))
	f.heroK = k
	heroH := max(110, min(H*.27, 210))
	cw := float32(rightW0)
	if a.rightTw.set {
		cw = float32(a.rightTw.to)
	}
	f.heroS = max(1, min(min(cw*.44, 260)/T.w, heroH/T.h, 3.2))
	f.heroTX, f.heroTY = W-a.rightW/2, H-30
	row := ui.Row(c.Key("dock")).Padding(12, 14, 10, 18).Gap(12).AlignItems(ui.End)
	if f.textH > 0 {
		nat := max(f.textH, T.h) + 22
		row.Height(nat + (T.h*f.heroS+52-nat)*k)
	}
	// hover a ball of the hero bin: its row lights and scrolls into view, its arc lights
	prev := f.hov
	f.hov = ""
	if inT && k > .9 {
		if x, y, over := row.PointerPosition(); over {
			b := row.Bounds()
			if e, ok := a.hoverBall(b.X+x, b.Y+y); ok {
				f.hov = e.key
				if e.id != "" {
					a.tsun.hlID = e.id
				}
				row.Cursor(ui.CursorPointer)
			}
		}
	}
	f.hovNew = f.hov != "" && f.hov != prev
	if f.hov != prev {
		a.frame = true
	}
	live := k < .3
	row.Children(func() {
		ui.Column(c.Key("dtext")).Grow(1).Shrink(1).Gap(1).Opacity(max(0, 1-k*1.8)).Draw(func(p *ui.Painter, r ui.Rect) {
			if k == 0 {
				f.textH = r.H
			}
		}).Children(func() {
			title := trashName()
			if total > 0 {
				title += " · " + T.name
			}
			ui.Text(c, title).FontSize(12).TextColor(pal.t2).SingleLine()
			sz := "空"
			if n > 0 {
				sz = fmtSize(size)
			}
			ui.Text(c, sz).FontSize(20).FontWeight(600).SingleLine()
			sub := "拖动文件到这里删除"
			if total > 0 {
				sub = fmtNum(int64(n)) + " 项 · 清空前可随时还原"
			}
			ui.Text(c, sub).FontSize(12).TextColor(pal.t3).SingleLine().Ellipsis("…")
			ui.Row(c).Gap(6).Padding(8, 0, 0, 0).Children(func() {
				label := "查看"
				if a.scene == sceneTrash {
					label = "返回"
				}
				if a.btn(c.Key("view"), label, btnPlain, true, nil).Disabled(total == 0 && a.scene != sceneTrash).Clicked() && live {
					if a.scene == sceneTrash {
						a.goScene(sceneBrowse)
					} else {
						a.goScene(sceneTrash)
					}
				}
				if a.btn(c.Key("empty"), "清空", btnPlain, true, nil).Disabled(total == 0 || a.trashMoving > 0).Clicked() && live {
					a.ask = &askState{kind: askEmpty, t0: a.now}
				}
			})
		})
		bin := ui.Box(c.Key("bin")).Size(T.w, T.h).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
			a.now = p.Now()
			f.right, f.bottom = r.X+r.W, r.Y+r.H
			a.paintBinBack(p, f.ents)
		})
		if !inT {
			bin.Cursor(ui.CursorPointer)
			if total > 0 {
				bin.Tooltip("查看" + trashName())
			}
			if bin.Clicked() && total > 0 {
				a.goScene(sceneTrash)
			}
		}
	})
}

// goScene switches the right panel; the new one staggers in.
func (a *app) goScene(s int) {
	if s == a.scene {
		return
	}
	a.switchScene(func() {
		if a.scene == sceneEval {
			a.eval = nil
		}
		if s == sceneTrash {
			a.enterTrashSun()
		} else if a.scene == sceneTrash { // the disk chart grows back
			a.sun.intro, a.sun.key = a.now, geoKey{}
		}
		a.scene = s
	})
}

// switchScene lets the current panel's parts stagger out (130ms), then applies the change
// and lets the new panel's parts stagger in.
func (a *app) switchScene(apply func()) {
	a.sceneNext++
	if !a.motion() {
		apply()
		a.panelT0, a.sceneT0 = a.now, time.Time{}
		return
	}
	seq := a.sceneNext
	a.sceneT0 = a.now
	a.after(130*time.Millisecond, func() {
		if seq != a.sceneNext {
			return
		}
		apply()
		a.panelT0, a.sceneT0 = a.now, time.Time{}
	})
}

// ------------------------------------------------------------------------------------------
// confirms

const (
	askRisky = iota
	askEmpty
)

type askState struct {
	kind int
	node int32
	t0   time.Time
}

func (a *app) dialogs(c *ui.Context) {
	if a.ask != nil {
		open := true
		ui.Modal(c.Key("ask"), &open, func() { a.askBox(c) })
		if !open {
			a.ask = nil
		}
	}
	if a.showSettings {
		a.settingsModal(c)
	}
}

func (a *app) askBox(c *ui.Context) {
	pal := a.pal
	q := a.ask
	if q == nil {
		return
	}
	var title, ok string
	danger, binIcon := true, false
	var body func()
	switch q.kind {
	case askRisky:
		n := q.node
		if a.t == nil || !a.t.Attached(n) {
			a.ask = nil
			return
		}
		title, ok = "确认删除个人数据？", "仍然移至"+trashName()
		body = func() {
			ui.Text(c, fmt.Sprintf("「%s」被识别为个人数据（%s），大小 %s。", a.name(n), strings.TrimPrefix(a.verdict(n).Reason(), "个人数据，"), fmtSize(a.t.Nodes[n].Size))).TextColor(pal.t2)
			ui.Text(c, "它会先进入"+trashName()+"，清空前仍可还原。").TextColor(pal.t2)
		}
	case askEmpty:
		items := a.bin.List()
		if len(items) == 0 {
			a.ask = nil
			return
		}
		var size int64
		for _, it := range items {
			size += it.Size
		}
		title, ok = fmt.Sprintf("永久删除这 %s 个项目？", fmtNum(int64(len(items)))), "永久删除"
		binIcon = true
		body = func() { a.emptyBody(c, items, size) }
	}
	k := a.progress(q.t0, 0, 340*time.Millisecond)
	e := float32(bez(.2, .9, .3, 1.1, k))
	box := ui.Column(c.Key("askbox")).Width(380).Padding(18, 20, 16, 20).Gap(12).FontSize(12.5)
	if k < 1 {
		box.Scale(.35+.65*e, .35+.65*e).Opacity(min(1, float32(k)/.7))
	}
	part := func(el ui.Element, i int) ui.Element {
		pk := a.progress(q.t0, time.Duration(90+i*45)*time.Millisecond, 240*time.Millisecond)
		if pk < 1 {
			pe := float32(easeStd(pk))
			el.Opacity(pe).Translate(0, 4*(1-pe))
		}
		return el
	}
	box.Children(func() {
		part(ui.Row(c.Key("ah")).Gap(10).AlignItems(ui.Center).Children(func() {
			col := pal.level[safety.Risky]
			ic := icoWarn
			if binIcon {
				ic = icoTrash
			}
			ui.Box(c).Size(30, 30).Radius(15).Background(col.Alpha(.14)).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				icon(c, ic, 17, col, 1.8)
			})
			ui.Text(c, title).FontSize(15).FontWeight(600).Grow(1).Shrink(1)
		}), 0)
		part(ui.Column(c.Key("ab")).Gap(6).Children(body), 1)
		part(ui.Row(c.Key("af")).Gap(8).Justify(ui.End).Padding(4, 0, 0, 0).Children(func() {
			if a.btn(c.Key("no"), "取消", btnPlain, false, nil).Clicked() {
				a.ask = nil
			}
			kind := btnDanger
			if !danger {
				kind = btnPrimary
			}
			if a.btn(c.Key("ok"), ok, kind, false, nil).Clicked() {
				a.ask = nil
				switch q.kind {
				case askRisky:
					a.trashOne(q.node)
				case askEmpty:
					a.emptyTrash()
				}
			}
		}), 2)
	})
}

const icoWarn = "M12 8.5v4.5M12 16.4v.1M10.3 4.2L2.9 17.3Q2.2 20 4.6 20H19.4Q21.8 20 21.1 17.3L13.7 4.2Q12 1.8 10.3 4.2Z"

// emptyBody explains where everything in the bin came from, the learned habits above all.
func (a *app) emptyBody(c *ui.Context, items []trash.Item, size int64) {
	pal := a.pal
	type grp struct {
		n    int
		size int64
		ids  []string
	}
	var rule, manual, hab grp
	habits := map[string]*grp{}
	var hkeys []string
	for _, it := range items {
		g := &rule
		switch it.Source {
		case trash.ByManual:
			g = &manual
		case trash.ByHabit:
			g = &hab
			h := habits[it.Habit]
			if h == nil {
				h = &grp{}
				habits[it.Habit] = h
				hkeys = append(hkeys, it.Habit)
			}
			h.n++
			h.size += it.Size
		}
		g.n++
		g.size += it.Size
		g.ids = append(g.ids, it.ID)
	}
	sort.Strings(hkeys)
	ui.Text(c, fmtSize(size)+" 会从磁盘上抹掉，之后无法再还原。").TextColor(pal.t2)
	tot := float32(max(size, 1))
	ui.Box(c.Key("sbar")).Height(6).Radius(3).Clip().Margin(4, 0).Draw(func(p *ui.Painter, r ui.Rect) {
		x := r.X
		for _, s := range []struct {
			g grp
			c ui.Color
		}{{rule, pal.level[safety.Safe]}, {manual, pal.t2}, {hab, pal.level[safety.Review]}} {
			if s.g.n > 0 {
				w := r.W * float32(s.g.size) / tot
				p.Fill(ui.Rect{X: x, Y: r.Y, W: w, H: r.H}, s.c, 0)
				x += w
			}
		}
	})
	src := func(key string, g grp, col ui.Color, label string) {
		if g.n == 0 {
			return
		}
		ui.Row(c.Key(key)).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(7, 7).Radius(2).Background(col)
			ui.Text(c, label).TextColor(pal.t2)
			ui.Text(c, fmt.Sprintf("%d 项", g.n)).FontWeight(600)
			ui.Spacer(c)
			ui.Text(c, fmtSize(g.size)).Font(pal.mono).FontSize(12).TextColor(pal.t3)
		})
	}
	src("rule", rule, pal.level[safety.Safe], "按清理规则")
	src("manual", manual, pal.t2, "你手动删除")
	src("habit", hab, pal.level[safety.Review], "依据你的历史操作")
	if hab.n == 0 {
		return
	}
	ui.Column(c.Key("hx")).Gap(6).Padding(10, 12).Radius(8).Background(pal.level[safety.Review].Alpha(.08)).Margin(4, 0, 0, 0).Children(func() {
		for _, k := range hkeys {
			g := habits[k]
			d := 0
			seed := false
			if a.habit != nil {
				for _, e := range a.habit.Entries() {
					if e.Label == k && e.D > d {
						d, seed = e.D, e.Seed
					}
				}
			}
			ui.Column(c.Key(k)).Gap(1).Children(func() {
				ui.Text(c, k).FontWeight(600).FontSize(12)
				s := ""
				if d > 0 {
					s = fmt.Sprintf("你曾手动删除 %d 个同类项", d)
					if seed {
						s += "（示例历史）"
					}
					s += " → "
				}
				ui.Text(c, s+fmt.Sprintf("本次 %d 项 · %s", g.n, fmtSize(g.size))).FontSize(11.5).TextColor(pal.t3)
			})
		}
		if a.btn(c.Key("keep"), fmt.Sprintf("还原这 %d 项", hab.n), btnPlain, true, nil).AlignSelf(ui.Start).Clicked() {
			a.ask = nil
			a.restore(hab.ids)
		}
	})
}

// emptyTrash tips the bin out, then deletes for good.
func (a *app) emptyTrash() {
	if a.bin == nil {
		return
	}
	var size int64
	for _, it := range a.bin.List() {
		size += it.Size
	}
	a.startDump(size)
	wait := time.Duration(0)
	if a.motion() {
		wait = 440 * time.Millisecond
	}
	a.after(wait, func() {
		freed, err := a.bin.Empty(nil)
		a.trashed = nil
		a.freedTotal += freed
		a.disk.ok = false // re-read free space
		a.binS.landed = map[string]time.Time{}
		if err != nil {
			a.toast(ui.Toast{Title: "部分项目未能删除", Description: firstLine(err.Error()), Type: "error"})
		}
		if a.scene == sceneTrash {
			a.goScene(sceneBrowse)
		}
	})
}

var _ = scan.None
