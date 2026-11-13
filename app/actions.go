package main

import (
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/21stware/tossy/engine/gitrepo"
	"github.com/21stware/tossy/engine/safety"
	"github.com/21stware/tossy/engine/scan"
	"github.com/21stware/tossy/engine/trash"
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
			msg = Tf("「%s」受系统保护：%s", a.name(n), T(v.Reason()))
		}
		a.toast(ui.Toast{Title: msg})
		return
	}
	if v.Level() == safety.Risky {
		a.ask = &askState{kind: askRisky, node: n, t0: a.now}
		return
	}
	if list := a.checkoutsIn(n); len(list) > 0 {
		a.ask = &askState{kind: askGit, node: n, list: list, t0: a.now}
		return
	}
	a.trashOne(n)
}

// asksFirst says whether deleting n stops for a confirm (the dragged ball goes back meanwhile).
func (a *app) asksFirst(n int32) bool {
	return a.verdict(n).Level() == safety.Risky || len(a.checkoutsIn(n)) > 0
}

func (a *app) trashOne(n int32) {
	nd := &a.t.Nodes[n]
	name, size := a.name(n), nd.Size
	it := cleanItem{n, trash.Meta{Size: size, Dir: nd.IsDir(), Source: trash.ByManual, Level: int(a.verdict(n).Level())}, ""}
	b := a.trashNodes([]cleanItem{it}, false)
	a.toast(ui.Toast{Title: Tf("已将「%s」移至%s（%s）", name, trashName(), fmtSize(size)),
		Action: T("撤销"), OnAction: func() { a.undo(b) }})
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
			x0, y0 := f.toHero(ox, oy)
			x1, y1 := f.toHero(ox+T.w, oy+T.h)
			d.hot = d.x >= x0-14 && d.x <= x1+14 && d.y >= y0-20 && d.y <= y1+14
			f.hot = d.hot
			r.Cursor(ui.CursorGrabbing)
			a.frame = true
		}
	}
	if _, _, ended := r.DragEnded(); ended && d.node == n {
		a.binS.hot = false
		if d.on && d.hot && !a.asksFirst(n) {
			a.drop = dropSrc{n, d.x, d.y, d.c, true}
			*d = dragState{}
			a.selected = n
			a.requestTrash(n)
			return
		}
		if d.on {
			if d.hot {
				a.requestTrash(n) // personal data or a checkout: ask first; the ball goes back meanwhile
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
	if k >= 1 || !a.trashFX() {
		d.back = time.Time{}
		return
	}
	e := float32(easeStd(k))
	hx, hy := d.home.X+d.home.W/2, d.home.Y+d.home.H/2
	a.paintBall(p, d.bx+(hx-d.bx)*e, d.by+(hy-d.by)*e, d.d/2*(1-.6*e), d.c, 1-e)
	a.tick(p)
}

// ------------------------------------------------------------------------------------------
// the dock

// landed is what the dock counts: trash entries whose ball is no longer in the air.
func (a *app) landed(ents []pileEnt) (n int, size int64) {
	for _, e := range ents {
		if a.binS.inflight[e.key] > 0 || (e.id == "" && a.trashFX() && a.binS.landed[e.key].IsZero()) {
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
	tier := &binTiers[f.tier] // not T: the dock's text calls the translator T below
	// the bin rests in the window's bottom-right corner, at the foot of the right column; the
	// trash scene grows it into a hero on the trash panel's stage, in the same column
	inT := a.sceneOn() == sceneTrash
	W, H := c.Size()
	hk := 0.0
	if inT {
		hk = 1
	}
	k := max(0, float32(a.springTo(&f.heroSp, hk)))
	f.heroK = min(k, 1)
	// the hero's place and size come from the trash view's final layout, not from the stage as
	// it moves: the panels widen and the bin's reserved foot shrinks while the bin grows, and
	// following those mid-way bent its path (it dipped before settling)
	if inT && a.wTto > 0 {
		fw := a.wTto - 1 // the trash panel's width, less its left hairline
		th := f.tsaH
		if th == 0 {
			th = 50
		}
		f.heroS = max(dockS, min(heroBinH(H, a.sz)/tier.h, fw*.55/tier.w, 3.4))
		f.heroTX, f.heroTY = W-fw/2, H-2-th-stageBottom
	} else if f.heroS == 0 {
		f.heroS, f.heroTX, f.heroTY = dockS, f.right-tier.w/2, f.bottom
	}
	hw := tier.w * f.heroS
	f.heroW = hw + 56
	nat := a.dockNat()
	// the row covers the bin alone as the hero (the exit button sits just above it)
	rx := dockR - 14 + (W-f.heroTX-(hw+28)/2-(dockR-14))*k
	by := dockB - 10 + (H-f.heroTY-10-(dockB-10))*k
	row := ui.Row(c.Key("dock")).Absolute().Right(rx).Bottom(by).ZIndex(3).Padding(12, 14, 10, 14).Gap(18).AlignItems(ui.End).
		Height(nat + (tier.h*f.heroS+22-nat)*k).MinWidth(tier.w*dockS + 28 + (hw-tier.w*dockS)*k)
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
	row.Children(func() {
		ui.Spacer(c)
		th := f.textH
		if th == 0 {
			th = 74
		}
		ui.Column(c.Key("dtextc")).Shrink(1).Clip().Children(func() {
			ui.Column(c.Key("dtext")).Gap(3).AlignItems(ui.End).Translate(0, (th+4)*min(1, k*1.6)).Opacity(1 - .82*a.askK).Draw(func(p *ui.Painter, r ui.Rect) {
				if k == 0 {
					f.textH = r.H
				}
			}).Children(func() {
				sz := T("空")
				if n > 0 {
					sz = fmtSize(size)
				}
				st := ui.Text(c.Key("dsz"), sz).FontSize(dockFS).FontWeight(600).SingleLine().Draw(func(p *ui.Painter, r ui.Rect) {
					if k == 0 { // at rest: remember where, for the flight to the header
						f.szDX, f.szDY, f.szSet = W-r.X, H-r.Y, true
					}
				})
				if k > 0 && a.motion() {
					st.Opacity(0) // it is flying, or it is the header's now
				}
				sub := T("拖动文件到这里删除")
				if total > 0 {
					sub = Tf("%s · 点按查看", nItems(int64(n)))
				}
				if !inT {
					sub = T("点按查看")
					if total > 0 {
						sub = nItems(int64(n))
					}
				}
				ui.Text(c, sub).FontSize(14).TextColor(pal.t3).SingleLine().Ellipsis("…")
			})
		})
		bin := ui.Box(c.Key("bin")).Size(tier.w*dockS, tier.h*dockS).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
			a.now = a.clock(p.Now())
			// the slot as the tier draws it, at scale 1: hero() zooms it about its bottom centre
			f.right, f.bottom = r.X+r.W/2+tier.w/2, r.Y+r.H
			a.paintBinBack(p, f.ents)
		})
		if !inT {
			bin.Tooltip(Tf("查看%s", trashName()))
		}
	})
	a.flySize(c, k, n, size)
	row.Cursor(ui.CursorPointer)
	if row.Clicked() {
		switch {
		case inT && f.hov != "": // a ball: bring its row into view, stay in the trash
			f.hovNew = true
			a.frame = true
		case inT: // the hero bin closes the trash view, like the button above it
			a.goScene(sceneBrowse)
		case a.bin != nil: // an empty bin opens too
			a.goScene(sceneTrash)
		}
	}
}

// flySize is the shared trash size: on the way into the trash view the dock's size text
// grows into the header's, and shrinks back on the way out, riding the hero's spring.
func (a *app) flySize(c *ui.Context, k float32, n int, size int64) {
	f := &a.binS
	if k <= 0 || k >= 1 || !f.szSet || f.bigR.W == 0 || !a.motion() {
		return
	}
	W, H := c.Size()
	x0, y0 := W-f.szDX, H-f.szDY
	x1, y1 := f.bigR.X, f.bigR.Y
	fs := f.bigF
	if fs == 0 {
		fs = 34
	}
	s := T("空")
	if n > 0 {
		s = fmtSize(size)
	}
	ui.Text(c.Key("flysz"), s).Absolute().Left(x0 + (x1-x0)*k).Top(y0 + (y1-y0)*k).ZIndex(6).PassThrough().
		FontSize(dockFS + (fs-dockFS)*k).FontWeight(600).LetterSpacing(-.8 * k).SingleLine().TextColor(a.pal.text)
}

// dockR and dockB keep the bin off the window's (or, as the hero, the chart's) edges.
const dockR, dockB = 22, 14

// dockNat is the trash row's resting height: its text or the bin, whichever is taller.
func (a *app) dockNat() float32 {
	th := a.binS.textH
	if th == 0 {
		th = 74
	}
	return max(th, binTiers[a.binS.tier].h*dockS) + 22
}

// goScene switches the right panel; the new one staggers in.
func (a *app) goScene(s int) {
	if s == a.scene {
		return
	}
	a.switchScene(func() {
		if s == sceneTrash {
			a.pf = pfNone // the trash takes the focus
			a.enterTrashSun()
		} else if a.scene == sceneTrash { // the disk chart grows back
			a.sun.intro, a.sun.key = a.now, geoKey{}
		}
		a.scene = s
	})
}

// switchScene applies the change at once (no stagger-out: the layout springs start moving on
// the very frame of the press) and lets the new panel's parts stagger in.
func (a *app) switchScene(apply func()) {
	a.sceneNext++
	a.inkRefresh(true)
	apply()
	a.panelT0, a.sceneT0 = a.now, time.Time{}
	a.frame = true
}

// ------------------------------------------------------------------------------------------
// confirms

const (
	askRisky = iota
	askEmpty
	askGit // a folder holding Git working trees
)

type askState struct {
	kind int
	node int32
	list []int32     // askGit: the checkouts inside
	chk  *emptyCheck // askEmpty: what git says about the checkouts in the trash
	t0   time.Time
}

// The confirm is a message bubble that grows out of the right panel's top-left corner; the
// panel behind it quiets down, and a click anywhere else or Esc dismisses it.
func (a *app) dialogs(c *ui.Context) {
	if a.ask != nil {
		if ui.Box(c.Key("askx")).Absolute().Top(0).Left(0).Fill().ZIndex(60).Clicked() {
			a.askClose()
		}
		if c.Shortcut(0, ui.KeyEscape) {
			a.askClose()
		}
	}
	q, live := a.ask, true
	if q == nil && a.askOut != nil {
		q, live = a.askOut, false
		if a.progress(a.askOutT0, 0, 170*time.Millisecond) >= 1 {
			q, a.askOut = nil, nil
		}
	}
	if q != nil {
		a.askBox(c, q, live)
	}
}

// askClose lets the bubble shrink back into its corner.
func (a *app) askClose() {
	if a.ask != nil {
		a.askOut, a.askOutT0, a.ask = a.ask, a.now, nil
	}
}

// dropAsk forgets a confirm whose subject is gone.
func (a *app) dropAsk(q *askState) {
	if a.ask == q {
		a.ask = nil
	}
	if a.askOut == q {
		a.askOut = nil
	}
}

func (a *app) askBox(c *ui.Context, q *askState, live bool) {
	pal := a.pal
	var title, ok string
	danger := true
	var body func()
	switch q.kind {
	case askRisky:
		n := q.node
		if a.t == nil || !a.t.Attached(n) {
			a.dropAsk(q)
			return
		}
		title, ok = T("确认删除个人数据？"), Tf("仍然移至%s", trashName())
		body = func() {
			ui.Text(c, Tf("「%s」被识别为个人数据（%s），大小 %s。", a.name(n), T(strings.TrimPrefix(a.verdict(n).Reason(), "个人数据，")), fmtSize(a.t.Nodes[n].Size))).TextColor(pal.t2)
			ui.Text(c, Tf(markNote, trashName())).TextColor(pal.t2)
		}
	case askGit:
		n := q.node
		if a.t == nil || !a.t.Attached(n) {
			a.dropAsk(q)
			return
		}
		title, ok = T("删除 Git 工作区？"), Tf("仍然移至%s", trashName())
		if len(q.list) == 1 && q.list[0] == n {
			if a.ev.Git(n) == gitrepo.Worktree {
				title = T("删除这个 Git worktree？")
			} else {
				title = T("删除这个 Git 仓库？")
			}
		}
		body = func() {
			danger = a.gitAskBody(c, q)
			if !danger {
				ok = Tf("移至%s", trashName())
			}
		}
	case askEmpty:
		a.emptyBubble(c, q, live)
		return
	}
	W, _ := c.Size()
	surf := pal.panel2
	if !pal.dark {
		surf = pal.panel
	}
	bw := min(372, max(300, a.rightW-28))
	box := ui.Column(c.Key("askbox")).Absolute().Left(W-14-bw).Top(14).Width(bw).ZIndex(61).
		Padding(16, 16, 12, 16).Gap(10).FontSize(12.5).Background(surf).Radius(4, 16, 16, 16).
		Shadow(0, 24, 60, -18, ui.RGBA(0, 0, 0, .7)).TransformOrigin(0, 0)
	if live {
		// slides in from the window's right edge like a drawer
		k := a.progress(q.t0, 0, 300*time.Millisecond)
		if k < 1 {
			box.Translate((bw+14)*(1-float32(easeStd(k))), 0)
		}
	} else {
		// and back out the same way
		e := float32(easeIn(a.progress(a.askOutT0, 0, 220*time.Millisecond)))
		box.Translate((bw+14)*e, 0).PassThrough()
	}
	part := func(el ui.Element, i int) ui.Element {
		pk := a.progress(q.t0, time.Duration(90+i*45)*time.Millisecond, 240*time.Millisecond)
		if pk < 1 {
			el.Translate(bw*(1-float32(easeStd(pk))), 0)
		}
		return el
	}
	box.Children(func() {
		part(ui.Row(c.Key("ah")).Gap(10).AlignItems(ui.Center).Children(func() {
			col := pal.level[safety.Risky]
			ic := icoWarn
			ui.Box(c).Size(30, 30).Radius(15).Background(col.Alpha(.14)).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				icon(c, ic, 17, col, 1.8)
			})
			ui.Text(c, title).FontSize(15).FontWeight(600).Grow(1).Shrink(1)
		}), 0)
		part(ui.Column(c.Key("ab")).Gap(6).Children(body), 1)
		part(ui.Row(c.Key("af")).Gap(8).Justify(ui.End).Padding(4, 0, 0, 0).Children(func() {
			if a.btn(c.Key("no"), T("取消"), btnPlain, false, nil).Clicked() && live {
				a.askClose()
			}
			kind := btnDanger
			if !danger {
				kind = btnPrimary
			}
			if a.btn(c.Key("ok"), ok, kind, false, nil).Clicked() && live {
				a.askClose()
				if q.kind == askRisky || q.kind == askGit {
					a.trashOne(q.node)
				}
			}
		}), 2)
	})
}

const icoWarn = "M12 8.5v4.5M12 16.4v.1M10.3 4.2L2.9 17.3Q2.2 20 4.6 20H19.4Q21.8 20 21.1 17.3L13.7 4.2Q12 1.8 10.3 4.2Z"

// emptyTrash annihilates the given items (all of them if ids is nil), deletes them for good off
// the main thread, and lets the bin shine once they are gone.
func (a *app) emptyTrash(ids []string) {
	if a.bin == nil || a.emptying {
		return
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var size int64
	keys := map[string]bool{}
	for _, it := range a.bin.List() {
		if ids == nil || want[it.ID] {
			size += it.Size
			keys[it.Orig] = true
		}
	}
	a.emptying = true
	wait := a.startDump(keys)
	start, bin := time.Now(), a.bin
	go func() {
		freed, err := bin.Empty(ids)
		left := bin.List()
		a.onMain(func() {
			// the animation runs while the files go; whichever ends last finishes
			a.after(max(0, wait-time.Since(start)), func() { a.emptied(freed, size, left, err) })
		})
	}()
}

// emptied updates the screen once Empty has returned.
func (a *app) emptied(freed, planned int64, left []trash.Item, err error) {
	a.emptying = false
	still := map[string]bool{}
	for _, it := range left {
		still[it.ID] = true
	}
	for id := range a.trashed {
		if !still[id] {
			delete(a.trashed, id)
		}
	}
	a.freedTotal += freed
	a.disk.ok = false // re-read free space
	f := &a.binS
	f.landed = map[string]time.Time{}
	if err != nil {
		a.toast(ui.Toast{Title: T("部分项目未能删除"), Description: firstLine(err.Error()), Type: "error"})
	}
	back := time.Duration(0)
	if a.trashFX() {
		f.doneT0, f.bumpT0 = a.now, a.now
		f.freed, f.freedT = Tf("已释放 %s", fmtSize(min(freed, planned))), a.now
		back = 900 * time.Millisecond
	}
	if len(left) > 0 {
		return // what was kept stays on view
	}
	a.after(back, func() {
		if a.scene == sceneTrash {
			a.goScene(sceneBrowse)
		}
	})
}

var _ = scan.None
