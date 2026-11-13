package main

import (
	"math"
	"sort"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/fanfree/fanfree/engine/safety"
)

// Emptying the trash: the bin itself asks, in a comic speech bubble rising from its lid; on a
// yes the balls annihilate one by one, the rest go together, and the bin (and the disk line)
// shine once the space is free.

const (
	bubbleTail = 46 // from the bubble's body to the tip of its tail
	bubbleR    = 22 // the body's corners
	zapLife    = 420 * time.Millisecond
	doneLife   = 1100 * time.Millisecond
)

// emptyBubble is the confirm for 清空: a speech bubble whose tail points at the bin's lid.
func (a *app) emptyBubble(c *ui.Context, q *askState, live bool) {
	pal := a.pal
	if a.bin == nil {
		a.dropAsk(q)
		return
	}
	items := a.bin.List()
	if len(items) == 0 {
		a.dropAsk(q)
		return
	}
	var size int64
	for _, it := range items {
		size += it.Size
	}
	W, H := c.Size()
	f := &a.binS
	tier := &binTiers[f.tier]
	ox, oy := f.origin()
	_, _, s, _, _ := f.hero()
	rx, ry := f.toHero(ox+tier.w/2, oy+bodyTop(tier.body))
	// the tip lands on the lid a little right of centre: the close button stays clear
	tipX, tipY := rx+min(26, tier.w*s*.18), ry-5*s-4
	bw := min(286, max(232, a.rightW-56))
	left := max(W-a.rightW+14, min(tipX-bw*.6, W-16-bw))
	const padT, padR = 12, 10 // room for the emphasis marks and the hard shadow
	box := ui.Column(c.Key("bubble")).Absolute().Left(left).Bottom(H-tipY).Width(bw).ZIndex(61).
		Padding(padT+16, padR+20, bubbleTail+14, 20).Gap(4).AlignItems(ui.Center).
		TransformOrigin(max(0, min(1, (tipX-left)/bw)), 1)
	// pops out of the tip with a springy overshoot, and shrinks back into it
	var sc, op float32 = 1, 1
	if live {
		k := a.progress(q.t0, 0, 380*time.Millisecond)
		sc = float32(easeBack(k))
		op = float32(min(1, k*3))
	} else {
		k := a.progress(a.askOutT0, 0, 170*time.Millisecond)
		sc, op = 1-float32(easeIn(k))*.85, 1-float32(k)
		box.PassThrough()
	}
	if sc != 1 {
		box.Scale(sc, sc)
	}
	if op < 1 {
		box.Opacity(op)
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		body := ui.Rect{X: r.X, Y: r.Y + padT, W: r.W - padR, H: r.H - padT - bubbleTail}
		path := bubblePath(body, tipX, r.Y+r.H)
		ink := pal.text
		fill := pal.panel
		shadow := ink
		if pal.dark {
			fill, shadow = pal.panel2, ui.RGBA(0, 0, 0, .6)
		}
		p.Transform(ui.Translation(4, 5), 0, 0, func() { p.FillPath(path, shadow) })
		p.FillPath(path, fill)
		p.StrokePath(path, 2.4, ink)
		// emphasis marks off the top-right corner, a comic's "!"
		var m ui.Path
		cx, cy := body.X+body.W-6, body.Y+4
		for i, ang := range []float64{-1.95, -1.25, -.55} {
			l := float32(8)
			if i == 1 {
				l = 10
			}
			ca, sa := float32(math.Cos(ang)), float32(math.Sin(ang))
			m.MoveTo(cx+ca*6, cy+sa*6).LineTo(cx+ca*(6+l), cy+sa*(6+l))
		}
		p.StrokePath(m.Cap(ui.RoundCap), 2.2, ink)
	})
	chk := q.chk
	if live {
		chk = a.checkEmpty(q, items)
	}
	checking := chk == nil || !chk.done
	var risky []emptyRisk
	var rest []string
	if !checking {
		risky, rest = chk.risky, chk.safeIDs()
	}
	box.Children(func() {
		ui.Text(c, Tf("清空%s？", trashName())).FontSize(17).FontWeight(700).TextAlign(ui.Center)
		ui.Text(c, Tf("%s 将在原位置永久删除，不经过系统%s，无法还原", fmtSize(size), trashName())).FontSize(12.5).TextColor(pal.t2).TextAlign(ui.Center)
		switch {
		case checking:
			ui.Text(c, T("正在检查其中的 Git 工作区…")).FontSize(12).TextColor(pal.t3).TextAlign(ui.Center)
		case len(risky) > 0:
			ui.Column(c.Key("risk")).Gap(2).Padding(6, 0, 0, 0).Children(func() {
				ui.Text(c, Tn(int64(len(risky)), "%d 个 Git 工作区有尚未保存到别处的工作：", len(risky))).FontSize(12).
					TextColor(pal.level[safety.Risky]).FontWeight(600)
				for i, r := range risky[:min(3, len(risky))] {
					ui.Text(c.Key(i), r.name+"  "+r.line).FontSize(11.5).TextColor(pal.t2).SingleLine().Ellipsis("…")
				}
				if len(risky) > 3 {
					ui.Text(c, Tf("另有 %d 个", len(risky)-3)).FontSize(11.5).TextColor(pal.t3)
				}
			})
		}
		ui.Row(c.Key("bf")).Gap(8).Padding(10, 0, 0, 0).Children(func() {
			no := a.btn(c.Key("no"), T("算了"), btnPlain, false, nil).Radius(15).Border(1.6, pal.text.Alpha(.5))
			if no.Clicked() && live {
				a.askClose()
			}
			label, ids := T("清空"), []string(nil)
			if !checking {
				ids = chk.ids
			}
			if len(risky) > 0 && len(rest) > 0 {
				label, ids = T("保留它们，清空其余"), rest
			} else if len(risky) > 0 {
				label = T("仍然清空")
			}
			ok := a.btn(c.Key("ok"), label, btnDanger, false, nil).Radius(15).Border(1.6, pal.text).FontWeight(700).Disabled(checking)
			if ok.Pressed() {
				ok.Translate(0, 1)
			}
			if ok.Clicked() && live && !checking {
				a.askClose()
				a.emptyTrash(ids)
			}
		})
		if len(risky) > 0 && len(rest) > 0 {
			all := ui.Text(c.Key("all"), T("连同它们一起清空")).FontSize(11.5).TextColor(pal.t3).Underline().Cursor(ui.CursorPointer)
			if all.Clicked() && live {
				a.askClose()
				a.emptyTrash(chk.ids)
			}
		}
	})
	if live {
		a.frame = true
	}
}

// bubblePath is a rounded body with a curved tail running down to (tx, ty), one outline.
func bubblePath(b ui.Rect, tx, ty float32) *ui.Path {
	R := min(bubbleR, b.H/2)
	x0, y0, x1, y1 := b.X, b.Y, b.X+b.W, b.Y+b.H
	// the tail's base on the bottom edge, leaning toward the tip
	bc := max(x0+R+14, min(tx+10, x1-R-14))
	bl, br := bc-13, bc+11
	k := float32(kappa)
	var p ui.Path
	p.MoveTo(x0+R, y0).LineTo(x1-R, y0).
		CubeTo(x1-R+k*R, y0, x1, y0+R-k*R, x1, y0+R).
		LineTo(x1, y1-R).
		CubeTo(x1, y1-R+k*R, x1-R+k*R, y1, x1-R, y1).
		LineTo(br, y1).
		QuadTo(br-2, y1+(ty-y1)*.55, tx, ty).
		QuadTo(bl+2, y1+(ty-y1)*.35, bl, y1).
		LineTo(x0+R, y1).
		CubeTo(x0+R-k*R, y1, x0, y1-R+k*R, x0, y1-R).
		LineTo(x0, y0+R).
		CubeTo(x0, y0+R-k*R, x0+R-k*R, y0, x0+R, y0).Close()
	return &p
}

// easeBack overshoots a little and settles: the bubble's pop.
func easeBack(t float64) float64 {
	const c1 = 1.9
	u := t - 1
	return 1 + (c1+1)*u*u*u + c1*u*u
}

// ------------------------------------------------------------------------------------------
// annihilation

// startDump lines the pile up for annihilation, top first: a few go one by one, quicker each
// time, then all that is left goes at once. It returns when the last ball is gone.
func (a *app) startDump(keys map[string]bool) time.Duration {
	f := &a.binS
	if !a.trashFX() {
		return 0
	}
	f.dump = f.dump[:0]
	for _, e := range f.ents {
		if b, ok := f.pile[e.key]; ok && keys[e.key] {
			f.dump = append(f.dump, dumpBall{pileBall: b})
		}
	}
	sort.SliceStable(f.dump, func(i, j int) bool { return f.dump[i].y < f.dump[j].y })
	n := len(f.dump)
	singles := min(n, 7)
	if n > singles {
		singles = min(n-1, 6) // leave a crowd for the finale
	}
	at, gap := 120*time.Millisecond, 230*time.Millisecond
	for i := range f.dump {
		f.dump[i].delay = at
		if i < singles {
			at += gap
			gap = max(80*time.Millisecond, gap*4/5)
		}
		if i >= singles {
			f.dump[i].group = true
		}
	}
	end := at + zapLife
	if n > singles {
		end = at + zapLife + 60*time.Millisecond
	}
	if n == 0 {
		end = 200 * time.Millisecond
	}
	f.dumpT0, f.dumpDur = a.now, end
	return end
}

// paintZapBalls draws the pile while it annihilates, inside the bin's clip (pen space).
func (a *app) paintZapBalls(p *ui.Painter, ox, oy float32) {
	f := &a.binS
	d := a.now.Sub(f.dumpT0)
	white := ui.RGBA(255, 255, 255, 1)
	if a.ink() { // e-ink empties with a full refresh: what goes turns to ink, then to paper
		white = a.pal.ink
	}
	flash := float32(0)
	for _, b := range f.dump {
		c := a.pal.level[b.lv]
		x, y := ox+b.x, oy+b.y
		if d < b.delay {
			// waiting its turn: the next one in line trembles
			if b.delay-d < 160*time.Millisecond && !b.group {
				x += float32(math.Sin(float64(d)/float64(time.Millisecond)*.9)) * .6
			}
			a.paintBall(p, x, y, b.r, c, 1)
			continue
		}
		k := float64(d-b.delay) / float64(zapLife)
		if b.group && k < .5 {
			flash = max(flash, float32(1-k/.5))
		}
		if k >= 1 {
			continue
		}
		switch {
		case k < .3: // swells and whitens
			e := float32(easeStd(k / .3))
			r := b.r * (1 + .32*e)
			p.FillPath(&(&penPath{}).Circle(x, y, r*1.7).Path, c.Alpha(.18*e))
			a.paintBall(p, x, y, r, c.Mix(white, .75*e), 1)
		case k < .55: // and collapses to a point
			u := float32(easeIn((k - .3) / .25))
			r := b.r * 1.32 * (1 - u)
			p.FillPath(&(&penPath{}).Circle(x, y, r*1.7).Path, c.Alpha(.18*(1-u)))
			a.paintBall(p, x, y, r, white, 1)
		}
	}
	if flash > 0 {
		T := &binTiers[f.tier]
		al := .35 * flash
		if a.ink() {
			al = float32(1) // the panel's black frame, one step long
			if flash < .5 {
				al = 0
			}
		}
		fillR(p, ui.Rect{X: ox, Y: oy + T.m - 4, W: T.w, H: T.b - T.m + 4}, white.Alpha(al), 0)
	}
	if d > f.dumpDur {
		f.dumpT0, f.dump = time.Time{}, nil
	}
	a.frame = true
}

// paintZapSparks draws each annihilation's ring and sparks, outside the clip so they can
// fly over the rim (pen space).
func (a *app) paintZapSparks(p *ui.Painter, ox, oy float32) {
	f := &a.binS
	if f.dumpT0.IsZero() {
		return
	}
	d := a.now.Sub(f.dumpT0)
	for i, b := range f.dump {
		k := float64(d-b.delay) / float64(zapLife)
		if k < .3 || k >= 1 {
			continue
		}
		u := (k - .3) / .7
		e, fade := float32(easeOut(u)), float32(1-u)
		c := a.pal.level[b.lv]
		x, y := ox+b.x, oy+b.y
		ring := (&penPath{}).Circle(x, y, b.r*(1+2.4*e))
		rc := ui.RGBA(255, 255, 255, .85*fade)
		if a.ink() {
			rc = a.pal.ink.Alpha(.8 * fade)
		}
		p.StrokePath(&ring.Path, pen.w(1.6*fade), rc)
		const n = 7
		var sp penPath
		for j := 0; j < n; j++ {
			ang := float64(j)/n*tau + float64(i)*.7
			dist := b.r * (1.1 + 3.4*e)
			sx := x + float32(math.Cos(ang))*dist
			sy := y + float32(math.Sin(ang))*dist - 7*e
			sp.Circle(sx, sy, max(.3, 1.3*fade))
		}
		sc := c.Mix(ui.RGBA(255, 255, 255, 1), .3)
		if a.ink() {
			sc = a.pal.ink
		}
		p.FillPath(&sp.Path, sc.Alpha(fade))
	}
}

// ------------------------------------------------------------------------------------------
// done: the bin shines

// paintEmptyDone draws the shine over the bin once it is empty (window space, after the front).
func (a *app) paintEmptyDone(p *ui.Painter) {
	f := &a.binS
	if f.doneT0.IsZero() {
		return
	}
	k := float64(a.now.Sub(f.doneT0)) / float64(doneLife)
	if k >= 1 {
		f.doneT0 = time.Time{}
		return
	}
	a.frame = true
	T := &binTiers[f.tier]
	ox, oy := f.origin()
	_, _, s, _, _ := f.hero()
	cx, cy := f.toHero(ox+T.w/2, oy+bodyTop(T.body))
	w := T.w * s
	safe := a.pal.level[safety.Safe]
	e, fade := float32(easeOut(k)), float32(1-easeIn(k))
	// a glow rising out of the opening
	gr := w * (.5 + .4*e)
	if a.ink() {
		safe = a.pal.ink
	} else {
		p.FillRadialGradient(ui.Rect{X: cx - gr, Y: cy - gr, W: 2 * gr, H: 2 * gr}, ui.RadialGradient{
			Radius: gr,
			Stops:  []ui.GradientStop{{Color: safe.Alpha(.32 * fade), At: 0}, {Color: safe.Alpha(0), At: 1}},
		}, 0)
	}
	// a ring spreading flat from the rim
	rw := w * (.45 + .55*e)
	p.Transform(ui.Scaling(1, .28), cx, cy, func() {
		var ring ui.Path
		ring.Circle(cx, cy, rw)
		p.StrokePath(&ring, 2.4*fade/.28*.4, safe.Alpha(.75*fade))
	})
	// sparkles float up and twinkle
	const n = 9
	for i := 0; i < n; i++ {
		ph := float64(i) / n
		lk := (k - ph*.25) / .75
		if lk <= 0 || lk >= 1 {
			continue
		}
		x := cx + (float32(ph)-.5)*w*.85 + float32(math.Sin(lk*5+ph*9))*6
		y := cy - float32(easeOut(lk))*(30+40*float32(i%3))*max(1, s*.6)
		tw := float32(.5 + .5*math.Sin(lk*14+ph*20))
		sz := (3 + 2.5*float32(i%2)) * (1 - float32(lk)*.4) * max(1, s*.5)
		col := safe
		if i%3 == 1 && !a.ink() {
			col = ui.RGBA(255, 255, 255, 1)
		}
		sparkle(p, x, y, sz, col.Alpha(float32(1-easeIn(lk))*(.45+.55*tw)))
	}
}

// sparkle draws a four-point star at (x, y), r from centre to tip.
func sparkle(p *ui.Painter, x, y, r float32, c ui.Color) {
	if r <= .2 || c.A <= 0 {
		return
	}
	q := r * .28
	var s ui.Path
	s.MoveTo(x, y-r).QuadTo(x+q*.4, y-q*.4, x+r, y).QuadTo(x+q*.4, y+q*.4, x, y+r).
		QuadTo(x-q*.4, y+q*.4, x-r, y).QuadTo(x-q*.4, y-q*.4, x, y-r).Close()
	p.FillPath(&s, c)
}

// paintDiskDone sweeps a light along the disk line as the space comes back.
func (a *app) paintDiskDone(p *ui.Painter, r ui.Rect) {
	f := &a.binS
	if f.doneT0.IsZero() {
		return
	}
	k := a.progress(f.doneT0, 80*time.Millisecond, 900*time.Millisecond)
	if k >= 1 {
		return
	}
	safe := a.pal.level[safety.Safe]
	x := r.X - r.W*.4 + r.W*1.4*float32(easeStd(k))
	p.FillGradient(ui.Rect{X: x - r.W*.4, Y: r.Y, W: r.W * .4, H: r.H}, ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
		{Color: safe.Alpha(0), At: 0}, {Color: safe.Alpha(.95), At: 1}}}, 0)
	p.AnimationFrame()
}

// ------------------------------------------------------------------------------------------
// the clean button's glitter dust

// paintDust lets specks of glitter rise and twinkle across the button while it is hovered;
// they fade in with the hover and out after it.
func (a *app) paintDust(p *ui.Painter, r ui.Rect, fg ui.Color) {
	target := 0.0
	if a.dustHot && a.motion() {
		target = 1
	}
	k := float32(a.tweenTo(&a.dustK, target, 320*time.Millisecond))
	if k <= .001 {
		a.dustT0 = time.Time{}
		return
	}
	if a.dustT0.IsZero() {
		a.dustT0 = p.Now()
	}
	t := float64(p.Now().Sub(a.dustT0)) / float64(time.Second)
	safe := a.pal.level[safety.Safe]
	rnd := mulberry32(0x9e3779b9)
	const n = 26
	for i := 0; i < n; i++ {
		bx, per, ph, sz, kind := rnd(), 1.3+1.4*float64(rnd()), float64(rnd()), rnd(), rnd()
		u := math.Mod(t/per+ph, 1) // 0..1 along its rise
		x := r.X + 6 + bx*(r.W-12) + float32(math.Sin(t*1.7+float64(i)))*4
		y := r.Y + r.H + 2 - float32(u)*(r.H+6)
		tw := float32(.5 + .5*math.Sin(t*9+float64(i)*2.3))
		life := float32(math.Sin(math.Pi * u)) // in from the bottom, out at the top
		al := k * life * (.35 + .65*tw)
		col := fg
		if i%3 == 0 {
			col = safe
		}
		if kind < .3 {
			sparkle(p, x, y, (2.2+2.2*sz)*(.6+.4*tw), col.Alpha(.9*al))
		} else {
			var d ui.Path
			d.Circle(x, y, .6+.9*sz)
			p.FillPath(&d, col.Alpha(.7*al))
		}
	}
	p.AnimationFrame()
}
