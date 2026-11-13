package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/fanfree/fanfree/engine/safety"
	"github.com/fanfree/fanfree/engine/scan"
)

const (
	rings   = 6
	minA    = 0.011 // radians: children narrower than this fold into one aggregate arc
	ringGap = 1.5
	tau     = 2 * math.Pi
	a0Top   = -math.Pi / 2
	zoomDur = 440 * time.Millisecond
)

// arc is one painted sector, kept for painting, hit testing and the HUD callout.
type arc struct {
	node   int32 // scan.None for an aggregate of small siblings
	parent int32 // the aggregate's parent
	count  int   // siblings in the aggregate
	size   int64
	a0, a1 float32 // radians clockwise from 12 o'clock
	r0, r1 float32
	ring   int
	h, s, l float32
	leaf    bool
	label   bool
	lv      safety.Level
	path    *ui.Path
}

type view struct{ x0, x1, d float64 }

type geoKey struct {
	ver        int
	v          view
	box        ui.Rect
	mode       int
	pal        *palette
	grow       float32
	morph      float32
}

type sunburst struct {
	arcs  []arc
	key   geoKey
	paths []*ui.Path // reused path storage

	box                 ui.Rect
	cx, cy, rad, r0, rw float32

	from    view
	zoomT0  time.Time
	zooming bool
	intro   time.Time
	cur     view

	// morph: after a removal or restore, arcs slide from where they were to the new layout
	prev   map[int32][2]float32
	mT0    time.Time
	mDur   time.Duration
	mk     float64
}

func arcKey(ar *arc) int32 {
	if ar.node == scan.None {
		return -2 - ar.parent
	}
	return ar.node
}

// chartMorph snapshots the painted arcs so the next layouts ease from them (remove 400ms,
// restore 420ms). Call it just before the tree changes.
func (a *app) chartMorph(dur time.Duration) {
	s := &a.sun
	if !a.motion() || len(s.arcs) == 0 {
		return
	}
	m := make(map[int32][2]float32, len(s.arcs))
	for i := range s.arcs {
		m[arcKey(&s.arcs[i])] = [2]float32{s.arcs[i].a0, s.arcs[i].a1}
	}
	s.prev, s.mT0, s.mDur = m, a.now, dur
}

func (s *sunburst) morphK(now time.Time) float64 {
	if s.prev == nil {
		return 1
	}
	k := float64(now.Sub(s.mT0)) / float64(s.mDur)
	if k >= 1 {
		s.prev = nil
		return 1
	}
	return max(0, k)
}

// applyMorph eases each arc's span from its snapshot; new arcs open from their middle.
func (a *app) applyMorph() {
	s := &a.sun
	if s.prev == nil || s.mk >= 1 {
		return
	}
	e := float32(ease(s.mk))
	cr := a.pal.corner
	keep := s.arcs[:0]
	for _, ar := range s.arcs {
		f0, f1 := (ar.a0+ar.a1)/2, (ar.a0+ar.a1)/2
		if o, ok := s.prev[arcKey(&ar)]; ok {
			f0, f1 = o[0], o[1]
		}
		ar.a0 = f0 + (ar.a0-f0)*e
		ar.a1 = f1 + (ar.a1-f1)*e
		if ar.a1-ar.a0 < .0008 {
			continue
		}
		ar.label = false
		padA := min(.8/ar.r0, (ar.a1-ar.a0)*.2)
		ar.path = sector(ar.path.Reset(), s.cx, s.cy, ar.r0, ar.r1, ar.a0+padA, ar.a1-padA, cr)
		keep = append(keep, ar)
	}
	s.arcs = keep
}

func (s *sunburst) reset() { s.zooming = false; s.key = geoKey{} }

// xOf returns i's span as fractions of the root's size.
func (a *app) xOf(i int32) (float64, float64) {
	t := a.t
	total := float64(max(t.Nodes[0].Size, 1))
	var x float64
	for _, n := range a.chain(i)[1:] {
		p := t.Nodes[n].Parent
		for c := t.Nodes[p].First; c != scan.None && c != n; c = t.Nodes[c].Next {
			x += float64(t.Nodes[c].Size) / total
		}
	}
	return x, x + float64(t.Nodes[i].Size)/total
}

func (a *app) targetView() view {
	x0, x1 := a.xOf(a.focus)
	return view{x0, x1, float64(a.depth(a.focus))}
}

func ease(t float64) float64 {
	if t < .5 {
		return 4 * t * t * t
	}
	return 1 - math.Pow(-2*t+2, 3)/2
}
func easeOut(t float64) float64 { return 1 - math.Pow(1-t, 3) }

// viewAt is the view at now: the focus, or on the way to it.
func (a *app) viewAt(now time.Time) (view, bool) {
	tv := a.targetView()
	s := &a.sun
	if !s.zooming {
		s.cur = tv
		return tv, false
	}
	k := float64(now.Sub(s.zoomT0)) / float64(zoomDur)
	if k >= 1 {
		s.zooming = false
		s.cur = tv
		return tv, false
	}
	e := ease(max(0, k))
	f := s.from
	s.cur = view{f.x0 + (tv.x0-f.x0)*e, f.x1 + (tv.x1-f.x1)*e, f.d + (tv.d-f.d)*e}
	return s.cur, true
}

func (s *sunburst) zoomFrom(a *app) {
	if s.cur.x1 == 0 {
		s.cur = a.targetView()
	}
	s.from = s.cur
	s.zoomT0 = a.now
	s.zooming = a.motion()
}

func (s *sunburst) introK(now time.Time) float64 {
	if s.intro.IsZero() {
		return 1
	}
	k := float64(now.Sub(s.intro)) / float64(900*time.Millisecond)
	if k >= 1 {
		s.intro = time.Time{}
		return 1
	}
	return max(0, k)
}

func growAt(k float64, ring float64) float32 {
	if k >= 1 {
		return 1
	}
	t := (k - (ring-1)*.1) / .42
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	return float32(easeOut(t))
}

// layout computes the arcs of view v in box r.
func (a *app) layout(v view, r ui.Rect, intro float64) {
	s := &a.sun
	s.box = r
	s.cx, s.cy = r.X+r.W/2, r.Y+r.H/2
	pad := float32(30)
	if a.pal.instr != instrNone {
		pad = 62
	}
	gap, cr := a.pal.gap, a.pal.corner
	s.rad = max(60, min(r.W, r.H)/2-pad)
	s.r0 = s.rad * .27
	s.rw = (s.rad - s.r0) / rings
	s.arcs = s.arcs[:0]
	t := a.t
	total := float64(max(t.Nodes[0].Size, 1))
	k := tau / max(1e-12, v.x1-v.x0)
	np := 0
	newPath := func() *ui.Path {
		if np < len(s.paths) {
			p := s.paths[np]
			np++
			return p.Reset()
		}
		p := &ui.Path{}
		s.paths = append(s.paths, p)
		np++
		return p
	}
	var rec func(n int32, nx0 float64, depth int, idx int)
	rec = func(n int32, nx0 float64, depth int, idx int) {
		nd := &t.Nodes[n]
		nx1 := nx0 + float64(nd.Size)/total
		a0, a1 := (nx0-v.x0)*k, (nx1-v.x0)*k
		if a1 <= 0 || a0 >= tau || a1-a0 <= 0 {
			return
		}
		ring := float64(depth) - v.d
		if ring > rings+.001 {
			return
		}
		if ring > 0 {
			g := growAt(intro, ring)
			if g <= 0 {
				return
			}
			r0 := s.r0 + float32(max(0, ring-1))*s.rw + gap
			r1 := r0 + (s.r0+float32(ring)*s.rw-r0)*g
			if r1-r0 > .5 {
				b0, b1 := float32(max(0, a0)), float32(min(tau, a1))
				h, sat, l := a.arcColor(n, (b0+b1)/2/tau, int(math.Round(ring)), idx)
				ar := arc{node: n, size: nd.Size, a0: b0, a1: b1, r0: r0, r1: r1, ring: int(math.Round(ring)), h: h, s: sat, l: l, leaf: nd.First == scan.None}
				ar.label = g >= 1 && r1-r0 > 15 && (b1-b0)*(r0+r1)/2 > 54
				padA := min(.8/r0, (b1-b0)*.2)
				ar.lv = a.ev.Verdict(n).Level()
				ar.path = sector(newPath(), s.cx, s.cy, r0, r1, b0+padA, b1-padA, cr)
				s.arcs = append(s.arcs, ar)
			}
		}
		if ring >= rings || nd.First == scan.None {
			return
		}
		x := nx0
		i := 0
		for c := nd.First; c != scan.None; c = t.Nodes[c].Next {
			cs := float64(t.Nodes[c].Size) / total
			c0, c1 := (x-v.x0)*k, (x+cs-v.x0)*k
			if c1-c0 < minA {
				// sorted by size: everything from here on is small → one quiet aggregate
				ringA := ring + 1
				g0, g1 := float32(max(0, c0)), float32(min(tau, a1))
				gg := growAt(intro, ringA)
				if ringA > 0 && ringA <= rings && g1-g0 > .0008 && gg > 0 {
					r0 := s.r0 + float32(max(0, ringA-1))*s.rw + gap
					r1 := r0 + (s.r0+float32(ringA)*s.rw-r0)*gg
					var sz int64
					cnt := 0
					for d := c; d != scan.None; d = t.Nodes[d].Next {
						sz += t.Nodes[d].Size
						cnt++
					}
					padA := min(.8/r0, (g1-g0)*.2)
					s.arcs = append(s.arcs, arc{node: scan.None, parent: n, count: cnt, size: sz, a0: g0, a1: g1, r0: r0, r1: r1, ring: int(math.Round(ringA)),
						path: sector(newPath(), s.cx, s.cy, r0, r1, g0+padA, g1-padA, cr)})
				}
				break
			}
			rec(c, x, depth+1, i)
			x += cs
			i++
		}
	}
	rec(0, 0, 0, 0)
	a.applyMorph()
	a.arcCount = len(s.arcs)
}

// arcColor: structure mode follows the style's ramp by angle; safety mode colours by verdict,
// in the style's own level colours.
func (a *app) arcColor(n int32, t float32, ring, idx int) (h, s, l float32) {
	pal := a.pal
	rr := float32(min(rings, max(1, ring)))
	light := !pal.dark
	leaf := a.t.Nodes[n].First == scan.None
	if a.mode == modeSafety {
		lv := a.ev.Verdict(n).Level()
		var L float32
		h, s, L = toHSL(pal.level[lv])
		h += float32(idx%5-2) * 2
		if light {
			l = L + 8 - rr*2.2
		} else {
			l = L + 4 - rr*2.8
		}
	} else {
		var L float32
		h, s, L = pal.rampAt(t)
		l = L + 6 - rr*3.6
		switch pal.art {
		case artTE:
			l = L + 4 - rr*1.6
			if idx == 0 && ring <= 2 {
				h, s, l = 16, 100, 56+rr*3 // the biggest of each inner ring wears the orange
			}
		case artSwiss, artPlate:
			l = L
		case artInk:
			l = L - rr*2.5
		}
	}
	l += float32(idx&1) * 1.6
	if leaf {
		s *= .5
		if light {
			l += 8
		} else {
			l -= 6
		}
	}
	return
}

// sector builds an annular sector with softly rounded corners. Angles are clockwise from 12 o'clock.
func sector(p *ui.Path, cx, cy, r0, r1, b0, b1, corner float32) *ui.Path {
	s0, s1 := float64(a0Top+b0), float64(a0Top+b1)
	pt := func(r float32, ang float64) (float32, float32) {
		return cx + float32(math.Cos(ang))*r, cy + float32(math.Sin(ang))*r
	}
	cr := min(corner, (r1-r0)/2.5, (b1-b0)*r0/2.5)
	if cr < 1 {
		p.MoveTo(pt(r1, s0))
		arcTo(p, cx, cy, r1, s0, s1)
		p.LineTo(pt(r0, s1))
		arcTo(p, cx, cy, r0, s1, s0)
		return p.Close()
	}
	dO, dI := float64(cr/r1), float64(cr/r0)
	p.MoveTo(pt(r0+cr, s0))
	p.LineTo(pt(r1-cr, s0))
	qx, qy := pt(r1, s0)
	ex, ey := pt(r1, s0+dO)
	p.QuadTo(qx, qy, ex, ey)
	arcTo(p, cx, cy, r1, s0+dO, s1-dO)
	qx, qy = pt(r1, s1)
	ex, ey = pt(r1-cr, s1)
	p.QuadTo(qx, qy, ex, ey)
	p.LineTo(pt(r0+cr, s1))
	qx, qy = pt(r0, s1)
	ex, ey = pt(r0, s1-dI)
	p.QuadTo(qx, qy, ex, ey)
	arcTo(p, cx, cy, r0, s1-dI, s0+dI)
	qx, qy = pt(r0, s0)
	ex, ey = pt(r0+cr, s0)
	p.QuadTo(qx, qy, ex, ey)
	return p.Close()
}

// arcTo appends a circular arc (the current point must be at its start) as cubic Béziers of ≤ 90°.
func arcTo(p *ui.Path, cx, cy, r float32, from, to float64) {
	n := int(math.Ceil(math.Abs(to-from) / (math.Pi / 2)))
	if n < 1 {
		n = 1
	}
	step := (to - from) / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	R := float64(r)
	for i := 0; i < n; i++ {
		a := from + float64(i)*step
		b := a + step
		ca, sa, cb, sb := math.Cos(a), math.Sin(a), math.Cos(b), math.Sin(b)
		p.CubeTo(cx+float32(R*(ca-k*sa)), cy+float32(R*(sa+k*ca)),
			cx+float32(R*(cb+k*sb)), cy+float32(R*(sb-k*cb)),
			cx+float32(R*cb), cy+float32(R*sb))
	}
}

func circle(p *ui.Path, cx, cy, r float32, from, to float64) *ui.Path {
	p.MoveTo(cx+float32(math.Cos(from))*r, cy+float32(math.Sin(from))*r)
	arcTo(p, cx, cy, r, from, to)
	return p
}

// hit finds what is at (x, y) in window coordinates: an arc index, -2 for the centre, -1 for nothing.
func (s *sunburst) hit(x, y float32) int {
	dx, dy := float64(x-s.cx), float64(y-s.cy)
	r := float32(math.Hypot(dx, dy))
	if r < s.r0-3 {
		return -2
	}
	ang := math.Atan2(dy, dx) - a0Top
	for ang < 0 {
		ang += tau
	}
	for ang >= tau {
		ang -= tau
	}
	a := float32(ang)
	for i := len(s.arcs) - 1; i >= 0; i-- {
		h := &s.arcs[i]
		if r >= h.r0-1 && r < h.r1 && a >= h.a0 && a < h.a1 {
			return i
		}
	}
	return -1
}

// arcPoint is the middle of node i's arc (or its nearest painted ancestor's), for flights.
func (a *app) arcPoint(i int32) (float32, float32, bool) {
	s := &a.sun
	for j := i; ; j = a.t.Nodes[j].Parent {
		for k := range s.arcs {
			if s.arcs[k].node == j {
				h := &s.arcs[k]
				m := float64(a0Top + (h.a0+h.a1)/2)
				r := (h.r0 + h.r1) / 2
				return s.cx + float32(math.Cos(m))*r, s.cy + float32(math.Sin(m))*r, true
			}
		}
		if j <= 0 {
			break
		}
	}
	return s.cx, s.cy, s.rad > 0
}

// ---------------------------------------------------------------------------------------------
// painting

func (a *app) paintSun(p *ui.Painter, r ui.Rect) {
	t0 := time.Now()
	pal := a.pal
	now := p.Now()
	a.now = now
	a.paintDeco(p, r)
	if a.t == nil {
		return
	}
	if a.scene == sceneTrash && a.bin != nil {
		a.paintTrashSun(p, r)
		return
	}
	v, moving := a.viewAt(now)
	intro := a.sun.introK(now)
	a.sun.mk = a.sun.morphK(now)
	key := geoKey{a.ver, v, r, a.mode, pal, float32(intro), float32(a.sun.mk)}
	if key != a.sun.key {
		a.layout(v, r, intro)
		a.sun.key = key
	}
	if moving || intro < 1 || a.sun.mk < 1 {
		p.AnimationFrame()
	}
	s := &a.sun

	// guide rings
	if pal.instr != instrNone && pal.art != artSwiss {
		var g ui.Path
		for i := 0; i <= rings; i++ {
			circle(&g, s.cx, s.cy, s.r0+float32(i)*s.rw+.5, 0, tau)
		}
		p.StrokePath(&g, 1, pal.ink.Alpha(.07))
	}

	dim := a.hover != scan.None
	hl := a.highlight()
	hlA := float32(1)
	if hl != nil { // the evaluation dims everything it will not touch, fading in
		k := a.progress(a.panelT0, 0, 260*time.Millisecond)
		if k < 1 {
			p.AnimationFrame()
		}
		hlA = 1 - .8*float32(easeOut(k))
	}
	sweep := float32(2 * tau) // analysis: sectors past the beam are not looked at yet
	if a.anal != nil {
		sweep = float32(a.anal.k) * tau
	}
	for i := range s.arcs {
		h := &s.arcs[i]
		node := h.node
		if node == scan.None {
			node = h.parent
		}
		alpha := float32(1)
		if dim && !a.isUnder(node, a.hover) {
			alpha = .35
		}
		if hl != nil && !a.underAny(node, hl) {
			alpha *= hlA
		}
		if h.node == scan.None {
			p.FillPath(h.path, pal.agg.Alpha(alpha))
			if pal.holo() || pal.art == artPlate || pal.art == artInk {
				p.StrokePath(h.path, .6, pal.ink.Alpha(.18*alpha))
			}
			continue
		}
		if (h.a0+h.a1)/2 > sweep {
			a.paintArc(p, h, 0, 0, a.unsweptL(), alpha*.8)
			continue
		}
		a.paintArc(p, h, h.h, h.s, h.l, alpha)
	}

	a.paintLabels(p)

	// selection outline
	w := float32(1.5)
	if pal.art == artInk || pal.art == artSwiss {
		w = 2.5
	}
	for i := range s.arcs {
		if h := &s.arcs[i]; h.node == a.selected && a.selected != a.focus {
			c := pal.text
			if pal.art == artSwiss {
				c = pal.accent
			}
			p.StrokePath(h.path, w, c)
		}
	}

	a.paintCenter(p)
	a.drawMs = float64(time.Since(t0).Microseconds()) / 1000
}

func (a *app) unsweptL() float32 {
	if a.pal.dark {
		return 24
	}
	return 84
}

// paintArc paints one sector in the style's manner.
func (a *app) paintArc(p *ui.Painter, h *arc, H, S, L, alpha float32) {
	pal := a.pal
	switch pal.art {
	case artSolid, artSwiss:
		p.FillPath(h.path, hsl(H, S, L, alpha))
	case artHolo:
		fa, sa := float32(.26), float32(.9)
		if h.leaf {
			fa, sa = .16, .55
		}
		p.FillPath(h.path, hsl(H, S, L*.8, fa*alpha))
		p.StrokePath(h.path, .9, hsl(H, min(100, S+12), min(86, L+14), sa*alpha))
	case artPlate:
		p.FillPath(h.path, hsl(H, S, L, alpha))
		wa, w := float32(.62), float32(.9)
		if h.leaf {
			wa, w = .32, .6
		}
		p.StrokePath(h.path, w, pal.ink.Alpha(wa*alpha))
	case artBlueprint:
		fill := ui.RGBA(255, 255, 255, (.04+float32(h.ring)*.012)*alpha)
		if a.mode == modeSafety {
			fill = hsl(H, S, L, .22*alpha)
		}
		p.FillPath(h.path, fill)
		sa := float32(.85)
		if h.leaf {
			sa = .45
		}
		edge := pal.ink
		if a.mode == modeSafety {
			edge = hsl(H, S, min(92, L+10), 1)
		}
		p.StrokePath(h.path, .8, edge.Alpha(sa*alpha))
	case artPhosphor:
		p.FillPath(h.path, hsl(H, S, L*.6, .12*alpha))
		ea := float32(.9)
		if h.leaf {
			ea = .5
		}
		p.StrokePath(h.path, 3.2, hsl(H, S, L, .1*alpha*ea))
		p.StrokePath(h.path, .9, hsl(H, S, min(80, L+12), ea*alpha))
	case artTE:
		p.FillPath(h.path, hsl(H, S, L, alpha))
		p.StrokePath(h.path, .7, pal.ink.Alpha(.22*alpha))
	case artInk:
		p.FillPath(h.path, hsl(0, 0, L, alpha))
		if a.mode == modeSafety && (h.lv == 1 || h.lv == 2) {
			hatch(p, h.path, h.lv == 2, pal.ink.Alpha(.26*alpha))
		}
		p.StrokePath(h.path, .8, pal.ink.Alpha(.85*alpha))
	}
}

// hatch draws diagonal (cross when cross) lines inside path: e-ink cannot show hue, so
// review and personal data read by texture.
func hatch(p *ui.Painter, path *ui.Path, cross bool, c ui.Color) {
	b := path.Bounds()
	p.ClipPath(path, func() {
		var l ui.Path
		for x := b.X - b.H; x < b.X+b.W; x += 6 {
			l.MoveTo(x, b.Y+b.H).LineTo(x+b.H, b.Y)
			if cross {
				l.MoveTo(x, b.Y).LineTo(x+b.H, b.Y+b.H)
			}
		}
		p.StrokePath(&l, .7, c)
	})
}

// paintDeco paints the style's backdrop under the chart.
func (a *app) paintDeco(p *ui.Painter, r ui.Rect) {
	pal := a.pal
	switch pal.deco {
	case decoGrid:
		var minor, major ui.Path
		for x := r.X + float32(math.Mod(float64(r.W/2), 16)); x < r.X+r.W; x += 16 {
			minor.MoveTo(x, r.Y).LineTo(x, r.Y+r.H)
		}
		for y := r.Y + float32(math.Mod(float64(r.H/2), 16)); y < r.Y+r.H; y += 16 {
			minor.MoveTo(r.X, y).LineTo(r.X+r.W, y)
		}
		for x := r.X + float32(math.Mod(float64(r.W/2), 80)); x < r.X+r.W; x += 80 {
			major.MoveTo(x, r.Y).LineTo(x, r.Y+r.H)
		}
		for y := r.Y + float32(math.Mod(float64(r.H/2), 80)); y < r.Y+r.H; y += 80 {
			major.MoveTo(r.X, y).LineTo(r.X+r.W, y)
		}
		p.StrokePath(&minor, 1, pal.ink.Alpha(.05))
		p.StrokePath(&major, 1, pal.ink.Alpha(.1))
	case decoDots:
		var d ui.Path
		for y := r.Y + 9; y < r.Y+r.H; y += 18 {
			for x := r.X + 9; x < r.X+r.W; x += 18 {
				d.MoveTo(x-.8, y-.8).LineTo(x+.8, y-.8).LineTo(x+.8, y+.8).LineTo(x-.8, y+.8).Close()
			}
		}
		p.FillPath(&d, pal.ink.Alpha(.16))
	}
}

// paintScreen draws effects over the whole chart (CRT scanlines and vignette).
func (a *app) paintScreen(p *ui.Painter, r ui.Rect) {
	if a.pal.deco != decoScan {
		return
	}
	var l ui.Path
	for y := r.Y + 1; y < r.Y+r.H; y += 3 {
		l.MoveTo(r.X, y).LineTo(r.X+r.W, y)
	}
	p.StrokePath(&l, 1, ui.RGBA(0, 0, 0, .28))
	p.FillRadialGradient(r, ui.RadialGradient{
		Center: ui.Point{X: .5, Y: .5}, Radius: .75,
		Stops: []ui.GradientStop{{Color: ui.RGBA(0, 0, 0, 0), At: .55}, {Color: ui.RGBA(0, 0, 0, .55), At: 1}},
	}, 0)
}

// highlight is the set of nodes the chart should bring forward (the evaluation's selection),
// or nil.
func (a *app) highlight() map[int32]bool {
	return nil
}

func (a *app) underAny(i int32, set map[int32]bool) bool {
	for {
		if set[i] {
			return true
		}
		if i <= 0 {
			return false
		}
		i = a.t.Nodes[i].Parent
	}
}

func (a *app) paintCenter(p *ui.Painter) {
	s, pal := &a.sun, a.pal
	if a.focus != 0 {
		dep := min(a.depth(a.focus), 5)
		if a.centerHot {
			var c ui.Path
			p.FillPath(circle(&c, s.cx, s.cy, s.r0-2, 0, tau), pal.ink.Alpha(.035))
		}
		for i := 0; i < dep; i++ {
			al := float32(.17)
			if a.centerHot && i == 0 {
				al = .55
			}
			var c ui.Path
			p.StrokePath(circle(&c, s.cx, s.cy, s.r0-5-float32(i)*4.5, 0, tau), 2, pal.ink.Alpha(al-float32(i)*.03))
		}
	}
	info := a.focus
	if a.hover != scan.None {
		info = a.hover
	}
	nd := &a.t.Nodes[info]
	name := a.name(info)
	sub := ""
	switch {
	case info == a.focus && a.centerHot && a.focus != 0:
		sub = "↑ 返回 " + a.name(a.t.Nodes[a.focus].Parent)
	case nd.IsDir():
		sub = fmtNum(int64(nd.Count-1)) + " 个项目"
	default:
		sub = kindOf(a.t.Name(info))
	}
	if info != a.focus && a.t.Nodes[a.focus].Size > 0 {
		sub += " · " + pct(nd.Size, a.t.Nodes[a.focus].Size)
	}
	if a.hoverAgg != nil {
		name, sub = fmtNum(int64(a.hoverAgg.count))+" 个较小项目", "聚合 · "+pct(a.hoverAgg.size, a.t.Nodes[a.focus].Size)
	}
	fs := max(11, s.r0*.11)
	big := max(18, s.r0*.25)
	size := fmtSize(nd.Size)
	if a.hoverAgg != nil {
		size = fmtSize(a.hoverAgg.size)
	}
	centerText(p, s.cx, s.cy-s.r0*.28, clipText(p, name, s.r0*1.5, fs, 400), fs, 400, pal.t3, "")
	centerText(p, s.cx, s.cy+s.r0*.02, size, big, 600, pal.text, "")
	col := pal.t3
	if a.centerHot && info == a.focus && a.focus != 0 {
		col = pal.text
	}
	centerText(p, s.cx, s.cy+s.r0*.28, clipText(p, sub, s.r0*1.6, max(10, s.r0*.1), 400), max(10, s.r0*.1), 400, col, "")
}

func pct(a, b int64) string {
	if b <= 0 {
		return ""
	}
	return trimFloat(100*float64(a)/float64(b)) + "%"
}

func trimFloat(f float64) string {
	return fmtFloat1(f)
}

func centerText(p *ui.Painter, x, y float32, s string, size float32, weight int, c ui.Color, font string) {
	sp := ui.Span{Text: s, Size: size, Weight: weight, Color: c, Font: font}
	w, h := p.MeasureText(0, sp)
	p.RichText(x-w/2, y-h/2, 0, sp)
}

func clipText(p *ui.Painter, s string, maxW, size float32, weight int) string {
	w, _ := p.MeasureText(0, ui.Span{Text: s, Size: size, Weight: weight})
	if w <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 2 {
		r = r[:len(r)-1]
		if w, _ := p.MeasureText(0, ui.Span{Text: string(r) + "…", Size: size, Weight: weight}); w <= maxW {
			break
		}
	}
	return string(r) + "…"
}

// paintLabels writes names (and sizes, on thick rings) along wide arcs; the lower half reads left to right.
func (a *app) paintLabels(p *ui.Painter) {
	s, pal := &a.sun, a.pal
	for i := range s.arcs {
		h := &s.arcs[i]
		if !h.label || h.node == scan.None {
			continue
		}
		am := float64(a0Top + (h.a0+h.a1)/2)
		rm := (h.r0 + h.r1) / 2
		flip := math.Sin(am) > .05
		two := h.r1-h.r0 >= 30
		up := float32(1)
		if flip {
			up = -1
		}
		rN, rS := rm, rm-up*7
		if two {
			rN = rm + up*6
		}
		maxW := (h.a1-h.a0)*min(rN, rm) - 14
		if maxW < 34 {
			continue
		}
		name := clipText(p, a.t.Name(h.node), maxW, 11, 500)
		if len([]rune(name)) < 3 {
			continue
		}
		alpha := float32(1)
		if a.hover != scan.None && !a.isUnder(h.node, a.hover) {
			alpha = .35
		}
		col, sc := pal.labelText.Alpha(alpha), pal.labelText.Alpha(.6*alpha)
		if !pal.holo() {
			// ink by the sector's lightness: dark on light plates, white on dark ones
			if luma(hsl(h.h, h.s, h.l, 1)) > .5 {
				col, sc = ui.RGBA(15, 15, 18, .85*alpha), ui.RGBA(15, 15, 18, .55*alpha)
			} else {
				col, sc = ui.RGBA(255, 255, 255, .95*alpha), ui.RGBA(255, 255, 255, .7*alpha)
			}
		}
		font := pal.font
		arcText(p, name, s.cx, s.cy, rN, am, flip, 11, 500, col, font)
		if two {
			arcText(p, fmtSize(h.size), s.cx, s.cy, rS, am, flip, 9.5, 400, sc, "monospace")
		}
	}
}

// arcText lays text out glyph by glyph along a circle of radius r centred on angle am.
func arcText(p *ui.Painter, str string, cx, cy, r float32, am float64, flip bool, size float32, weight int, c ui.Color, font string) {
	type g struct {
		s    string
		w, h float32
	}
	var gs []g
	var total float32
	for _, ch := range str {
		sp := ui.Span{Text: string(ch), Size: size, Weight: weight, Font: font}
		w, h := p.MeasureText(0, sp)
		gs = append(gs, g{string(ch), w, h})
		total += w
	}
	acc := -total / 2
	dir := 1.0
	if flip {
		dir = -1
	}
	for _, x := range gs {
		ang := am + dir*float64(acc+x.w/2)/float64(r)
		px, py := cx+float32(math.Cos(ang))*r, cy+float32(math.Sin(ang))*r
		rot := ang + math.Pi/2
		if flip {
			rot = ang - math.Pi/2
		}
		p.Transform(ui.Rotation(float32(rot*180/math.Pi)), px, py, func() {
			p.RichText(px-x.w/2, py-x.h/2, 0, ui.Span{Text: x.s, Size: size, Weight: weight, Color: c, Font: font})
		})
		acc += x.w
	}
}

func levelColor(pal *palette, lv safety.Level) ui.Color { return pal.level[lv] }
