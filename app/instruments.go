package main

import (
	"time"

	"fmt"
	"github.com/fanfree/fanfree/engine/safety"
	"math"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/fanfree/fanfree/engine/scan"
)

// Instrument layers drawn around the sunburst, one per style. They read a.sun's geometry from
// this frame's layout and never change state.

func (a *app) monoText(p *ui.Painter, x, y float32, s string, size float32, c ui.Color) {
	p.RichText(x, y, 0, ui.Span{Text: s, Size: size, Font: "monospace", Color: c})
}

func polar(cx, cy, r float32, ang float64) (float32, float32) {
	return cx + float32(math.Cos(ang))*r, cy + float32(math.Sin(ang))*r
}

func (a *app) modeName() string {
	if a.scene == sceneTrash {
		return "TRASH-ORIGIN"
	}
	if a.mode == modeSafety {
		return "SAFETY"
	}
	return "STRUCTURE"
}

// instrHUD: bezel ticks, counter-rotating dashed rings, reticle, corner readouts.
func (a *app) instrHUD(p *ui.Painter, r ui.Rect) {
	s, ink := &a.sun, a.pal.ink
	rb := s.rad + 12
	var tk ui.Path
	for i := 0; i < 180; i++ {
		l := float32(3)
		if i%15 == 0 {
			l = 9
		} else if i%5 == 0 {
			l = 6
		}
		ang := float64(i)*tau/180 + a0Top
		x0, y0 := polar(s.cx, s.cy, rb, ang)
		x1, y1 := polar(s.cx, s.cy, rb+l, ang)
		tk.MoveTo(x0, y0).LineTo(x1, y1)
	}
	p.StrokePath(&tk, 1, ink.Alpha(.22))
	for i, lab := range []string{"000", "090", "180", "270"} {
		x, y := polar(s.cx, s.cy, rb+20, float64(i)*math.Pi/2+a0Top)
		centerText(p, x, y, lab, 9, 500, ink.Alpha(.45), "monospace")
	}
	// dashed rings turn with the view: they move while zooming and rest otherwise
	v := s.cur
	rot := float64(v.x0+v.x1) * math.Pi
	var d1, d2 ui.Path
	circle(&d1, s.cx, s.cy, rb+32, a0Top+rot, a0Top+rot+tau).Dash(0, 2, 6)
	circle(&d2, s.cx, s.cy, rb+38, a0Top-2*rot, a0Top-2*rot+tau).Dash(0, 24, 10, 4, 10)
	p.StrokePath(&d1, 1, ink.Alpha(.18))
	p.StrokePath(&d2, 1, ink.Alpha(.12))
	var rt ui.Path
	for _, q := range [4][2]float32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		rt.MoveTo(s.cx+q[0]*(rb+44), s.cy+q[1]*(rb+44)).LineTo(s.cx+q[0]*(rb+56), s.cy+q[1]*(rb+56))
	}
	p.StrokePath(&rt, 1, ink.Alpha(.35))
	if a.pal.instr == instrMech {
		return // mech writes its own corners
	}
	a.monoText(p, r.X+16, r.Y+12, fmt.Sprintf("SECTORS %d · DEPTH %d", a.arcCount, a.depth(a.focus)), 10, ink.Alpha(.45))
	a.monoText(p, r.X+16, r.Y+26, "MODE "+a.modeName(), 10, ink.Alpha(.45))
	if a.mode == modeSafety || a.scene == sceneTrash {
		return // the legend owns the bottom-left corner
	}
	a.monoText(p, r.X+16, r.Y+r.H-22, fmt.Sprintf("SCAN %s · %s FILES", strings.ToUpper(fmtSize(a.t.Nodes[0].Size)), fmtNum(a.t.Stats.Files)), 10, ink.Alpha(.35))
}

// instrMech: caution band, cobalt and signal-red stripes on the bezel, registration marks.
func (a *app) instrMech(p *ui.Painter, r ui.Rect) {
	s, ink := &a.sun, a.pal.ink
	rb := s.rad + 46
	band := func(from, to float64, w float32, c ui.Color) *ui.Path {
		var b ui.Path
		arcTo(b.MoveTo(polar(s.cx, s.cy, rb, a0Top+from*tau)), s.cx, s.cy, rb, a0Top+from*tau, a0Top+to*tau)
		arcTo(b.LineTo(polar(s.cx, s.cy, rb+w, a0Top+to*tau)), s.cx, s.cy, rb+w, a0Top+to*tau, a0Top+from*tau)
		b.Close()
		p.FillPath(&b, c)
		return &b
	}
	yellow := ui.Hex("#f2b705")
	cb := band(.62, .74, 7, yellow)
	p.ClipPath(cb, func() {
		var d ui.Path
		for t := .62; t < .74; t += .006 {
			x0, y0 := polar(s.cx, s.cy, rb-2, a0Top+t*tau)
			x1, y1 := polar(s.cx, s.cy, rb+9, a0Top+(t+.004)*tau)
			d.MoveTo(x0, y0).LineTo(x1, y1)
		}
		p.StrokePath(&d, 2.4, ui.Hex("#1a1a1a"))
	})
	band(.06, .2, 4, ui.Hex("#1f4fa8"))
	band(.205, .235, 4, ui.Hex("#d7262e"))
	// registration marks at the chart's corners
	var m ui.Path
	for _, c := range [4][3]float32{{r.X + 14, r.Y + 14, 1}, {r.X + r.W - 14, r.Y + 14, -1}, {r.X + 14, r.Y + r.H - 14, 1}, {r.X + r.W - 14, r.Y + r.H - 14, -1}} {
		dy := float32(1)
		if c[1] > r.Y+r.H/2 {
			dy = -1
		}
		m.MoveTo(c[0], c[1]+dy*14).LineTo(c[0], c[1]).LineTo(c[0]+c[2]*14, c[1])
	}
	p.StrokePath(&m, 1.6, ink.Alpha(.55))
	a.monoText(p, r.X+24, r.Y+20, "UNIT·DISK  /  ARMOR MAP", 10, ink.Alpha(.7))
	a.monoText(p, r.X+24, r.Y+34, fmt.Sprintf("SEC %03d · LV %02d · %s", a.arcCount, a.depth(a.focus), a.modeName()), 9.5, ink.Alpha(.45))
	label := fmt.Sprintf("LOAD %s  ·  %s FILES", strings.ToUpper(fmtSize(a.t.Nodes[0].Size)), fmtNum(a.t.Stats.Files))
	w, _ := p.MeasureText(0, ui.Span{Text: label, Size: 9.5, Font: "monospace"})
	p.Fill(ui.Rect{X: r.X + r.W - w - 38, Y: r.Y + r.H - 44, W: w + 14, H: 16}, ink.Alpha(.9), 1)
	a.monoText(p, r.X+r.W-w-31, r.Y+r.H-42, label, 9.5, ui.Hex("#f2b705"))
}

// instrBlueprint: centre lines across the sheet, a radius dimension, ring index and title block.
func (a *app) instrBlueprint(p *ui.Painter, r ui.Rect) {
	s, ink := &a.sun, a.pal.ink
	var cl ui.Path
	cl.MoveTo(r.X+21, s.cy).LineTo(r.X+r.W-21, s.cy)
	cl.MoveTo(s.cx, r.Y+21).LineTo(s.cx, r.Y+r.H-21)
	cl.Dash(0, 18, 4, 3, 4)
	p.StrokePath(&cl, .8, ink.Alpha(.35))
	// radius dimension at 45° below right, with arrowheads and the total size
	ang := math.Pi / 4
	x0, y0 := polar(s.cx, s.cy, s.r0, ang)
	x1, y1 := polar(s.cx, s.cy, s.rad, ang)
	var dim ui.Path
	dim.MoveTo(x0, y0).LineTo(x1, y1)
	arrow := func(x, y float32, dir float64) {
		ax, ay := polar(x, y, 7, dir+.35)
		bx, by := polar(x, y, 7, dir-.35)
		dim.MoveTo(ax, ay).LineTo(x, y).LineTo(bx, by)
	}
	arrow(x0, y0, ang)
	arrow(x1, y1, ang+math.Pi)
	ex, ey := polar(s.cx, s.cy, s.rad+34, ang)
	dim.MoveTo(x1, y1).LineTo(ex, ey).LineTo(ex+70, ey)
	p.StrokePath(&dim, .8, ink.Alpha(.85))
	a.monoText(p, ex+4, ey-14, "R = "+fmtSize(a.t.Nodes[a.focus].Size), 10, ink)
	// ring index along the vertical centre line
	for i := 1; i <= rings; i++ {
		y := s.cy - s.r0 - (float32(i)-.5)*s.rw
		if y < r.Y+8 {
			break
		}
		a.monoText(p, s.cx+3, y-6, fmt.Sprintf("R%d", i), 8, ink.Alpha(.5))
	}
	// title block
	tb := ui.Rect{X: r.X + r.W - 15 - 222, Y: r.Y + r.H - 15 - 58, W: 222, H: 58} // in the sheet's corner
	p.Fill(tb, a.pal.bg, 0)
	var box ui.Path
	box.MoveTo(tb.X, tb.Y).LineTo(tb.X+tb.W, tb.Y).LineTo(tb.X+tb.W, tb.Y+tb.H).LineTo(tb.X, tb.Y+tb.H).Close()
	box.MoveTo(tb.X, tb.Y+22).LineTo(tb.X+tb.W, tb.Y+22)
	box.MoveTo(tb.X+110, tb.Y+22).LineTo(tb.X+110, tb.Y+tb.H)
	p.StrokePath(&box, .9, ink.Alpha(.8))
	a.monoText(p, tb.X+8, tb.Y+5, "FANFREE · DISK SURVEY", 10, ink)
	a.monoText(p, tb.X+8, tb.Y+27, fmt.Sprintf("SHEET %d/%d", a.depth(a.focus)+1, a.depth(a.focus)+1), 9, ink.Alpha(.75))
	a.monoText(p, tb.X+8, tb.Y+40, a.modeName(), 9, ink.Alpha(.75))
	a.monoText(p, tb.X+118, tb.Y+27, fmt.Sprintf("SECT %d", a.arcCount), 9, ink.Alpha(.75))
	a.monoText(p, tb.X+118, tb.Y+40, fmtNum(a.t.Stats.Files)+" F", 9, ink.Alpha(.75))
}

// instrTerminal: bracketed frame, a tick ring and a command line with a blinking cursor.
func (a *app) instrTerminal(p *ui.Painter, r ui.Rect) {
	s, ink := &a.sun, a.pal.ink
	rb := s.rad + 14
	var tk ui.Path
	for i := 0; i < 72; i++ {
		ang := float64(i)*tau/72 + a0Top
		l := float32(4)
		if i%6 == 0 {
			l = 9
		}
		x0, y0 := polar(s.cx, s.cy, rb, ang)
		x1, y1 := polar(s.cx, s.cy, rb+l, ang)
		tk.MoveTo(x0, y0).LineTo(x1, y1)
	}
	p.StrokePath(&tk, 1, ink.Alpha(.35))
	var br ui.Path
	in := float32(12)
	for _, c := range [4][4]float32{{r.X + in, r.Y + in, 1, 1}, {r.X + r.W - in, r.Y + in, -1, 1}, {r.X + in, r.Y + r.H - in, 1, -1}, {r.X + r.W - in, r.Y + r.H - in, -1, -1}} {
		br.MoveTo(c[0], c[1]+c[3]*20).LineTo(c[0], c[1]).LineTo(c[0]+c[2]*20, c[1])
	}
	p.StrokePath(&br, 1.2, ink.Alpha(.6))
	a.monoText(p, r.X+24, r.Y+20, fmt.Sprintf("[ SECTORS %d ] [ DEPTH %d ] [ %s ]", a.arcCount, a.depth(a.focus), a.modeName()), 10.5, ink.Alpha(.75))
	line := "> du -sh " + clipText(p, a.t.Path(a.focus), r.W*.55, 10.5, 400) + "  " + strings.ToUpper(fmtSize(a.t.Nodes[a.focus].Size))
	line, done := a.typed(p, line)
	a.monoText(p, r.X+24, r.Y+r.H-30, line, 10.5, ink.Alpha(.85))
	w, _ := p.MeasureText(0, ui.Span{Text: line, Size: 10.5, Font: "monospace"})
	if !done || a.now.UnixMilli()/530%2 == 0 || !a.pal.motion { // solid while typing
		p.Fill(ui.Rect{X: r.X + 28 + w, Y: r.Y + r.H - 30, W: 7, H: 13}, ink.Alpha(.85), 0)
	}
	if a.pal.motion {
		p.After(time.Duration(530-a.now.UnixMilli()%530) * time.Millisecond)
	}
}

// instrTE: a knob scale 0–10 around the dial, an orange LED and small-caps labels.
func (a *app) instrTE(p *ui.Painter, r ui.Rect) {
	s, ink := &a.sun, a.pal.ink
	rb := s.rad + 16
	var tk ui.Path
	for i := 0; i <= 50; i++ {
		ang := a0Top + float64(i)/50*tau
		l := float32(3)
		if i%5 == 0 {
			l = 7
		}
		x0, y0 := polar(s.cx, s.cy, rb, ang)
		x1, y1 := polar(s.cx, s.cy, rb+l, ang)
		tk.MoveTo(x0, y0).LineTo(x1, y1)
	}
	p.StrokePath(&tk, 1.2, ink.Alpha(.55))
	for i := 0; i < 10; i++ {
		x, y := polar(s.cx, s.cy, rb+18, a0Top+float64(i)/10*tau)
		centerText(p, x, y, fmt.Sprint(i), 10, 600, ink.Alpha(.7), "monospace")
	}
	p.FillPath((&ui.Path{}).Circle(r.X+26, r.Y+24, 4), a.pal.accent)
	a.monoText(p, r.X+38, r.Y+17, "CAPACITY", 10, ink)
	a.monoText(p, r.X+38, r.Y+31, fmt.Sprintf("CH %02d  ·  %s", a.depth(a.focus)+1, strings.ToLower(a.modeName())), 9, ink.Alpha(.5))
	tag := fmt.Sprintf("%d SECT", a.arcCount)
	w, _ := p.MeasureText(0, ui.Span{Text: tag, Size: 9.5, Font: "monospace"})
	p.Fill(ui.Rect{X: r.X + r.W - w - 34, Y: r.Y + 16, W: w + 14, H: 18}, ink, 9)
	a.monoText(p, r.X+r.W-w-27, r.Y+19, tag, 9.5, a.pal.bg)
}

// paintAnalysis draws the sweep that precedes a clean: a beam turns once around the chart, a
// fading wake behind it, brackets locking onto the biggest items as it passes (the first six
// tagged with their size), and a ring pulsing out when the plan is ready.
func (a *app) paintAnalysis(p *ui.Painter, r ui.Rect) {
	s, pal := &a.sun, a.pal
	c1, amb := pal.accent, pal.amber
	if pal.instr == instrNone || pal.art == artInk {
		amb = pal.level[safety.Review]
	}
	rb := s.rad + 12
	if an := a.anal; an != nil && an.k > 0 {
		beam := a0Top + an.k*tau
		if an.k < 1 && a.motion() {
			// the wake: 1 rad behind the beam, fading out
			const n = 24
			for i := 0; i < n; i++ {
				b0 := beam - float64(i+1)/n
				if b0 < a0Top {
					break
				}
				var w ui.Path
				w.MoveTo(polar(s.cx, s.cy, s.r0, b0))
				arcTo(w.LineTo(polar(s.cx, s.cy, s.rad, b0)), s.cx, s.cy, s.rad, b0, b0+1.0/n+.004)
				arcTo(w.LineTo(polar(s.cx, s.cy, s.r0, b0+1.0/n+.004)), s.cx, s.cy, s.r0, b0+1.0/n+.004, b0)
				w.Close()
				p.FillPath(&w, c1.Alpha(.14*(1-float32(i)/n)))
			}
		}
		// the beam: a line from the hole out past the bezel, brightening outwards
		x0, y0 := polar(s.cx, s.cy, s.r0, beam)
		x1, y1 := polar(s.cx, s.cy, rb+40, beam)
		const seg = 12
		for i := 0; i < seg; i++ {
			u0, u1 := float32(i)/seg, float32(i+1)/seg
			var l ui.Path
			l.MoveTo(x0+(x1-x0)*u0, y0+(y1-y0)*u0).LineTo(x0+(x1-x0)*u1, y0+(y1-y0)*u1)
			p.StrokePath(&l, 1.5, c1.Alpha(.9*u1))
		}
		shown := 0
		var tags [][2]float32
		for _, n := range an.locks {
			h := a.arcOf(n)
			if h == nil || h.node != n {
				continue
			}
			m := float64(h.a0+h.a1) / 2
			if m/tau > an.k {
				continue
			}
			age := float32(min(1, (an.k-m/tau)*8))
			px, py := polar(s.cx, s.cy, (h.r0+h.r1)/2, a0Top+m)
			d := 9 - age*3
			col := c1
			if shown < 6 {
				col = amb
			}
			if why := an.inuse[n]; why != "" {
				col = pal.t3
			}
			var b ui.Path
			for _, q := range [4][2]float32{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
				b.MoveTo(px+q[0]*d, py+q[1]*(d-4)).LineTo(px+q[0]*d, py+q[1]*d).LineTo(px+q[0]*(d-4), py+q[1]*d)
			}
			p.StrokePath(&b, 1.2, col.Alpha(.95-age*.35))
			if shown < 6 && age > .2 {
				free := true
				for _, t := range tags {
					if abs32(t[0]-px) <= 70 && abs32(t[1]-py) <= 16 {
						free = false
					}
				}
				if free {
					tags = append(tags, [2]float32{px, py})
					a.monoText(p, px+d+4, py-d-6, fmtSize(a.t.Nodes[n].Size), 9.5, amb.Alpha(.85))
				}
			}
			shown++
		}
	}
	if k := a.progress(a.pulse, 0, 700*time.Millisecond); k < 1 && !a.pulse.IsZero() {
		e := float32(easeOut(k))
		ring := circle(&ui.Path{}, s.cx, s.cy, s.r0+(rb+40-s.r0)*e, 0, tau)
		p.StrokePath(ring, 2-float32(k)*1.5, c1.Alpha(.7*(1-float32(k))))
		p.AnimationFrame()
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// arcOf finds the visible sector for node i, or the nearest visible ancestor's.
func (a *app) arcOf(i int32) *arc {
	for {
		for k := range a.sun.arcs {
			if a.sun.arcs[k].node == i {
				return &a.sun.arcs[k]
			}
		}
		if i <= 0 || i == a.focus {
			return nil
		}
		i = a.t.Nodes[i].Parent
		if i == scan.None {
			return nil
		}
	}
}
