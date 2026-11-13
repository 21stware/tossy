package main

import (
	"hash/fnv"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/21stware/tossy/engine/trash"
)

// The trash bin grows with what is in it: a desk basket, a waste basket, a pedal bin, a wheelie
// bin, a skip. Items lie in it as balls packed by a height map, coloured by safety level; the
// lid opens while things fly in, the bin bumps as each lands, and tips its pile out on empty.
// Ported from the prototype's TIERS, layoutPile and the bin CSS animations.

type binTier struct {
	name   string
	w, h   float32
	m, b   float32    // pile top and bottom
	tx, bx [2]float32 // opening and floor, left and right
	f      float64    // trash/capacity at which this tier starts
	lids   int
	pivots [2][2]float32
	body   string
}

var binTiers = []binTier{
	{name: "桌面纸篓", w: 54, h: 60, m: 16, b: 57, tx: [2]float32{6, 48}, bx: [2]float32{9, 45}, f: 0,
		body: "M6 15H48L45.2 55.5Q44.9 58 42.4 58H11.6Q9.1 58 8.8 55.5Z"},
	{name: "废纸篓", w: 70, h: 78, m: 18, b: 75, tx: [2]float32{5, 65}, bx: [2]float32{12, 58}, f: .004,
		body: "M5 17H65L58.6 73.6Q58.2 76 55.8 76H14.2Q11.8 76 11.4 73.6Z"},
	{name: "垃圾桶", w: 84, h: 100, m: 26, b: 95, tx: [2]float32{8, 76}, bx: [2]float32{14, 70}, f: .03, lids: 1,
		pivots: [2][2]float32{{6, 21}}, body: "M8 24H76L70.2 90.5Q69.6 96 64 96H20Q14.4 96 13.8 90.5Z"},
	{name: "大垃圾桶", w: 108, h: 136, m: 38, b: 121, tx: [2]float32{10, 98}, bx: [2]float32{16, 92}, f: .1, lids: 1,
		pivots: [2][2]float32{{6, 33}}, body: "M10 36H98L92.4 118Q92 122 88 122H20Q16 122 15.6 118Z"},
	{name: "垃圾箱", w: 172, h: 118, m: 38, b: 101, tx: [2]float32{9, 163}, bx: [2]float32{17, 155}, f: .22, lids: 2,
		pivots: [2][2]float32{{6, 33}, {166, 33}}, body: "M8 36H164L156 102H16Z"},
}

func tierFor(size, capacity int64) int {
	if size <= 0 || capacity <= 0 {
		return 0
	}
	f := float64(size) / float64(capacity)
	i := 0
	for i < len(binTiers)-1 && f > binTiers[i+1].f {
		i++
	}
	return i
}

// ballR is an item's ball radius in the pile: log of its size, bigger in bigger bins.
func ballR(size int64, tier int) float32 {
	mb := float64(size) / (1 << 20)
	r := max(2.6, min(8, 1.6+math.Log10(mb+1)*1.55))
	return float32(r * (.85 + float64(tier)*.08))
}

// ------------------------------------------------------------------------------------------
// what is in the trash, landed or on its way

type pileEnt struct {
	key  string // original path
	id   string // trash item id, "" while the move runs
	size int64
	lv   int
	src  trash.Source
}

type pileBall struct {
	x, y, r float32
	lv      int
}

// binState is the dock bin's animation state.
type binState struct {
	tier, prevTier int
	tierT0         time.Time
	right, bottom  float32 // the bin box's bottom-right corner in the window (last frame)

	pend     []pileEnt      // moved out of the tree, not in the journal yet
	inflight map[string]int // keys of balls in the air
	landed   map[string]time.Time
	lastLand time.Time
	bumpT0   time.Time

	lid     float32 // current angle magnitude, degrees
	lidFrom float32
	lidTo   float32
	lidT0   time.Time
	hot     bool // a drag hovers the bin

	dumpT0  time.Time // the pile annihilates (see startDump)
	dumpDur time.Duration
	dump    []dumpBall
	doneT0  time.Time // and the bin shines once it is empty
	freed   string
	freedT  time.Time

	pileK   [3]int64
	pile    map[string]pileBall
	ents    []pileEnt
	entsAt  time.Time
	shoveT0 time.Time

	// the hero bin of the trash scene: the dock bin grows to the column's bottom centre
	heroSp         spring
	heroK, heroS   float32
	tsaH           float32 // the trash panel's action row, under the hero's stage
	heroW          float32 // the hero's floor, wide enough for its tips
	heroTX, heroTY float32 // target bottom centre in the window
	textH          float32 // the dock's text column, last frame
	hov            string  // pile key under the pointer in the hero bin
	hovNew         bool    // hov changed this frame: its row scrolls into view
	rowHL          string  // pile key of the trash row under the pointer
	hlKey          string  // the lit ball, and when it lit: it swells over 150ms
	hlT0           time.Time

	// the trash size is one element shared by the dock and the trash panel's header: it flies
	// between the two while the hero grows or shrinks back (see flySize)
	bigR       ui.Rect // the header's size text, last frame
	bigF       float32 // and its font size
	szDX, szDY float32 // the dock's size text at rest, from the window's bottom-right corner
	szSet      bool
}

// dockS is the bin's scale at rest in the dock: the tiers are drawn at 1 and zoomed by it.
const dockS = 1.3

// dockFS is the font size of the dock's trash size text.
const dockFS = 26

// hero is the zoom from the dock slot: the slot's bottom centre, the scale and the shift.
func (f *binState) hero() (bx, by, s, dx, dy float32) {
	T := &binTiers[f.tier]
	bx, by = f.right-T.w/2, f.bottom
	k := f.heroK
	return bx, by, dockS + (f.heroS-dockS)*k, (f.heroTX - bx) * k, (f.heroTY - by) * k
}

// toHero maps a point of the bin in its dock slot to where the hero bin shows it.
func (f *binState) toHero(x, y float32) (float32, float32) {
	bx, by, s, dx, dy := f.hero()
	return bx + (x-bx)*s + dx, by + (y-by)*s + dy
}

type dumpBall struct {
	pileBall
	delay time.Duration // when it starts to go, from dumpT0
	group bool          // in the finale, with all that is left
}

// trashEnts lists the journal (oldest first) plus moves still running.
func (a *app) trashEnts() []pileEnt {
	f := &a.binS
	if a.bin == nil {
		return nil
	}
	items := a.bin.List()
	sort.SliceStable(items, func(i, j int) bool { return items[i].When.Before(items[j].When) })
	out := f.ents[:0]
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		out = append(out, pileEnt{it.Orig, it.ID, it.Size, it.Level, it.Source})
		seen[it.Orig] = true
	}
	for _, p := range f.pend {
		if !seen[p.key] {
			out = append(out, p)
		}
	}
	f.ents = out
	return out
}

func (a *app) capacity() int64 {
	if a.disk.ok && a.disk.total > 0 {
		return a.disk.total
	}
	if a.t != nil {
		return a.t.Nodes[0].Size * 6 / 5
	}
	return 1
}

// syncBin updates the tier and the pile layout for this frame's trash.
func (a *app) syncBin(ents []pileEnt) {
	f := &a.binS
	var total int64
	for _, e := range ents {
		total += e.size
	}
	t := tierFor(total, a.capacity())
	if t != f.tier {
		f.prevTier, f.tier = f.tier, t
		if a.trashFX() && !a.now.IsZero() {
			f.tierT0 = a.now
			if t > f.prevTier {
				f.shoveT0 = a.now
			}
		}
	}
	k := [3]int64{int64(len(ents)), total, int64(f.tier)}
	if f.pile != nil && k == f.pileK {
		return
	}
	f.pileK = k
	f.pile = layoutPile(ents, f.tier)
}

func layoutPile(ents []pileEnt, tier int) map[string]pileBall {
	T := &binTiers[tier]
	H := make([]float32, int(T.w)+2)
	room := T.b - T.m
	lo, hi := (T.tx[0]+T.bx[0])/2+1, (T.tx[1]+T.bx[1])/2-1
	out := make(map[string]pileBall, len(ents))
	for _, e := range ents {
		r := ballR(e.size, tier)
		h := fnv.New32a()
		h.Write([]byte(e.key))
		rr := mulberry32(h.Sum32())
		var bestX, bestBase float32
		found := false
		for k := 0; k < 6; k++ {
			cx := lo + r + rr()*(hi-lo-2*r)
			var base float32
			for x := int(math.Ceil(float64(cx - r))); x <= int(math.Floor(float64(cx+r))); x++ {
				if x < 0 || x >= len(H) {
					continue
				}
				dx := float32(x) - cx
				base = max(base, H[x]-r+float32(math.Sqrt(float64(max(0, r*r-dx*dx)))))
			}
			if !found || base < bestBase {
				bestX, bestBase, found = cx, base, true
			}
		}
		if bestBase+2*r > room {
			continue // visually full; the item still counts
		}
		for x := int(math.Ceil(float64(bestX - r))); x <= int(math.Floor(float64(bestX+r))); x++ {
			if x < 0 || x >= len(H) {
				continue
			}
			dx := float32(x) - bestX
			H[x] = max(H[x], bestBase+r+float32(math.Sqrt(float64(max(0, r*r-dx*dx)))))
		}
		out[e.key] = pileBall{bestX, T.b - bestBase - r, r, e.lv}
	}
	return out
}

func mulberry32(seed uint32) func() float32 {
	a := seed
	return func() float32 {
		a += 0x6D2B79F5
		t := a
		t = (t ^ t>>15) * (t | 1)
		t ^= t + (t^t>>7)*(t|61)
		return float32(t^t>>14) / 4294967296
	}
}

// origin is the tier's top-left in the window: the box's bottom-right corner never moves.
func (f *binState) origin() (float32, float32) {
	T := &binTiers[f.tier]
	return f.right - T.w, f.bottom - T.h
}

// ------------------------------------------------------------------------------------------
// animation state

func (a *app) lidAngle() float32 {
	f := &a.binS
	target := float32(0)
	switch {
	case f.hot:
		target = 58
	case len(f.inflight) > 0 || (!f.lastLand.IsZero() && a.now.Sub(f.lastLand) < 220*time.Millisecond):
		target = 42
	}
	if target != f.lidTo {
		f.lidFrom, f.lidTo, f.lidT0 = f.lid, target, a.now
	}
	k := a.progress(f.lidT0, 0, 220*time.Millisecond)
	f.lid = f.lidFrom + (f.lidTo-f.lidFrom)*float32(easeStd(k))
	if !f.dumpT0.IsZero() && f.dumpDur > 0 {
		// open wide while the pile goes, shut as the last one does
		d := a.now.Sub(f.dumpT0)
		if d < f.dumpDur {
			a.frame = true
			v := min(1, float64(d)/float64(180*time.Millisecond))
			if rest := f.dumpDur - d; rest < 260*time.Millisecond {
				v = min(v, float64(rest)/float64(260*time.Millisecond))
			}
			return float32(float64(f.lid) + (55-float64(f.lid))*ease(v))
		}
	}
	return f.lid
}

// bump is the landing squash: 0..1 peaking at 40%, cubic out and cubic back, no rebound.
func (a *app) binBump() float32 {
	k := a.progress(a.binS.bumpT0, 0, 260*time.Millisecond)
	if k >= 1 {
		return 0
	}
	if k < .4 {
		return float32(easeStd(k / .4))
	}
	return float32(1 - ease((k-.4)/.6))
}

// tierScale is the tier-change morph: from the old size straight to the new one on a cubic.
func (a *app) tierScale() (sx, sy, op float32) {
	f := &a.binS
	k := a.progress(f.tierT0, 0, 380*time.Millisecond)
	if k >= 1 {
		return 1, 1, 1
	}
	e := float32(easeStd(k))
	P, T := &binTiers[f.prevTier], &binTiers[f.tier]
	s0x, s0y := P.w/T.w, P.h/T.h
	return s0x + (1-s0x)*e, s0y + (1-s0y)*e, .5 + .5*e
}

// ------------------------------------------------------------------------------------------
// painting

type binInk struct {
	back, open, openLine, bodyFill, bodyLine, rim, rimFill, lid, lid2, lidHi, handle, wheel, wheelLine, hub, detail ui.Color
}

func (a *app) binInk() binInk {
	ink := a.pal.ink
	shade := ui.RGBA(0, 0, 0, .45)
	if !a.pal.dark {
		shade = ink.Alpha(.1)
	}
	return binInk{
		back: ink.Alpha(.045), open: shade, openLine: ink.Alpha(.18), bodyFill: ink.Alpha(.028), bodyLine: ink.Alpha(.16),
		rim: ink.Alpha(.32), rimFill: ink.Alpha(.26), lid: ink.Alpha(.21), lid2: ink.Alpha(.18), lidHi: ink.Alpha(.08),
		handle: ink.Alpha(.34), wheel: ink.Alpha(.12), wheelLine: ink.Alpha(.12), hub: ink.Alpha(.28), detail: ink.Alpha(.07),
	}
}

// binXform runs fn in tier-local coordinates (origin at the tier's top-left), with the tier morph,
// the landing bump, the hot swell and the hero zoom applied. They go into the pen, which the
// drawing helpers apply to the geometry itself: the painter would draw the paths at their small
// size and magnify the pixels, and the hero bin, at up to 3.4×, came out blurred.
func (a *app) binXform(p *ui.Painter, fn func(ox, oy float32)) {
	f := &a.binS
	T := &binTiers[f.tier]
	ox, oy := f.origin()
	sx, sy, _ := a.tierScale()
	b := a.binBump()
	sx *= 1 + .03*b
	sy *= 1 - .03*b
	if f.hot && T.lids == 0 {
		sx, sy = sx*1.06, sy*1.06
	}
	cx, cy := ox+T.w/2, oy+T.h
	bx, by, s, dx, dy := f.hero()
	X := func(x float32) float32 { return bx + s*(cx+sx*(x-cx)-bx) + dx }
	Y := func(y float32) float32 { return by + s*(cy+sy*(y-cy)+1.5*b-by) + dy }
	saved := pen
	pen = affine{ax: X(1) - X(0), ay: Y(1) - Y(0), bx: X(0), by: Y(0)}
	fn(ox, oy)
	pen = saved
}

// affine maps the bin's drawing to the window: x·ax+bx, y·ay+by (no rotation).
type affine struct{ ax, ay, bx, by float32 }

// pen is the mapping the bin's drawing helpers apply; outside binXform it leaves points be.
var pen = affine{ax: 1, ay: 1}

func (t affine) pt(x, y float32) (float32, float32) { return t.ax*x + t.bx, t.ay*y + t.by }
func (t affine) w(v float32) float32                { return v * (t.ax + t.ay) / 2 }
func (t affine) rect(r ui.Rect) ui.Rect {
	x, y := t.pt(r.X, r.Y)
	return ui.Rect{X: x, Y: y, W: r.W * t.ax, H: r.H * t.ay}
}

// penPath builds a path through the pen.
type penPath struct{ ui.Path }

func (q *penPath) MoveTo(x, y float32) *penPath { q.Path.MoveTo(pen.pt(x, y)); return q }
func (q *penPath) LineTo(x, y float32) *penPath { q.Path.LineTo(pen.pt(x, y)); return q }
func (q *penPath) QuadTo(cx, cy, x, y float32) *penPath {
	x1, y1 := pen.pt(cx, cy)
	x2, y2 := pen.pt(x, y)
	q.Path.QuadTo(x1, y1, x2, y2)
	return q
}
func (q *penPath) CubeTo(c1x, c1y, c2x, c2y, x, y float32) *penPath {
	x1, y1 := pen.pt(c1x, c1y)
	x2, y2 := pen.pt(c2x, c2y)
	x3, y3 := pen.pt(x, y)
	q.Path.CubeTo(x1, y1, x2, y2, x3, y3)
	return q
}
func (q *penPath) Close() *penPath { q.Path.Close(); return q }

// Circle adds a circle, an ellipse if the pen stretches it.
func (q *penPath) Circle(cx, cy, r float32) *penPath {
	if pen.ax == pen.ay {
		x, y := pen.pt(cx, cy)
		q.Path.Circle(x, y, r*pen.ax)
		return q
	}
	q.MoveTo(cx-r, cy).
		CubeTo(cx-r, cy-kappa*r, cx-kappa*r, cy-r, cx, cy-r).
		CubeTo(cx+kappa*r, cy-r, cx+r, cy-kappa*r, cx+r, cy).
		CubeTo(cx+r, cy+kappa*r, cx+kappa*r, cy+r, cx, cy+r).
		CubeTo(cx-kappa*r, cy+r, cx-r, cy+kappa*r, cx-r, cy).Close()
	return q
}

// fillR fills a rounded rectangle through the pen.
func fillR(p *ui.Painter, r ui.Rect, c ui.Color, radius float32) {
	p.Fill(pen.rect(r), c, pen.w(radius))
}

// hoverBall finds the pile ball under a window point in the hero bin.
func (a *app) hoverBall(px, py float32) (pileEnt, bool) {
	f := &a.binS
	ox, oy := f.origin()
	_, _, s, _, _ := f.hero()
	for i := len(f.ents) - 1; i >= 0; i-- {
		e := f.ents[i]
		b, ok := f.pile[e.key]
		if !ok || f.inflight[e.key] > 0 || !f.dumpT0.IsZero() {
			continue
		}
		x, y := f.toHero(ox+b.x, oy+b.y)
		r := b.r*s + 2
		if dx, dy := px-x, py-y; dx*dx+dy*dy <= r*r {
			return e, true
		}
	}
	return pileEnt{}, false
}

// paintBallTip labels the hovered ball of the hero bin: its name and size, above it.
func (a *app) paintBallTip(p *ui.Painter) {
	f := &a.binS
	if f.hov == "" || f.heroK < .9 {
		return
	}
	var ent pileEnt
	for _, e := range f.ents {
		if e.key == f.hov {
			ent = e
		}
	}
	b, ok := f.pile[f.hov]
	if !ok || ent.key == "" {
		return
	}
	pal := a.pal
	ox, oy := f.origin()
	_, _, s, _, _ := f.hero()
	x, y := f.toHero(ox+b.x, oy+b.y)
	name := clipText(p, filepath.Base(ent.key), 220, 12, 600)
	info := fmtSize(ent.size) + " · " + T(levelTitles[max(0, min(3, ent.lv))])
	w1, _ := p.MeasureText(0, ui.Span{Text: name, Size: 12, Weight: 600})
	w2, _ := p.MeasureText(0, ui.Span{Text: info, Size: 10.5, Font: "monospace"})
	w, h := max(w1, w2)+20, float32(42)
	bx := max(f.heroTX-f.heroW/2+8, min(x-w/2, f.heroTX+f.heroW/2-8-w))
	by := y - b.r*s*1.35 - 10 - h
	p.Fill(ui.Rect{X: bx, Y: by, W: w, H: h}, pal.ink.Alpha(.12), 8)
	p.Fill(ui.Rect{X: bx + .5, Y: by + .5, W: w - 1, H: h - 1}, pal.panel, 7.5)
	lc := pal.level[max(0, min(3, ent.lv))]
	p.FillPath((&ui.Path{}).Circle(bx+13, by+15, 3.5), lc)
	p.RichText(bx+22, by+6, 0, ui.Span{Text: name, Size: 12, Weight: 600, Color: pal.text})
	p.RichText(bx+10, by+23, 0, ui.Span{Text: info, Size: 10.5, Font: "monospace", Color: pal.t3})
}

// paintBinBack draws the inside of the bin and its pile (in the dock, under flying balls).
func (a *app) paintBinBack(p *ui.Painter, ents []pileEnt) {
	f := &a.binS
	T := &binTiers[f.tier]
	ik := a.binInk()
	if k := f.heroK; k > 0 {
		// the bin's shadow on the floor
		_, _, s, _, _ := f.hero()
		w := T.w * s * .84
		sh := float32(.8)
		if !a.pal.dark {
			sh = .22
		}
		p.Transform(ui.Scaling(1, 9/w), f.heroTX, f.heroTY+.5, func() {
			p.FillRadialGradient(ui.Rect{X: f.heroTX - w/2, Y: f.heroTY + .5 - w/2, W: w, H: w}, ui.RadialGradient{
				Radius: w / 2,
				Stops:  []ui.GradientStop{{Color: ui.RGBA(0, 0, 0, sh*k), At: 0}, {Color: ui.RGBA(0, 0, 0, 0), At: 1}},
			}, 0)
		})
	}
	a.binXform(p, func(ox, oy float32) {
		p.FillPath(svgPath(T.body, ox, oy), ik.back)
		switch f.tier {
		case 0:
			ellipse(p, ox+27, oy+15, 21, 3.6, ik.open, ik.openLine, 1.6)
		case 1:
			ellipse(p, ox+35, oy+17, 30, 4.6, ik.open, ik.openLine, 1.6)
		case 2:
			p.FillPath(svgPath("M10 26H74L73 34H11Z", ox, oy), ui.RGBA(0, 0, 0, .25))
		case 3:
			p.FillPath(svgPath("M12 38H96L95 47H13Z", ox, oy), ui.RGBA(0, 0, 0, .25))
		case 4:
			p.FillPath(svgPath("M10 38H162L161 47H11Z", ox, oy), ui.RGBA(0, 0, 0, .25))
		}
		// the pile, clipped to the bin's inside
		var clip penPath
		clip.MoveTo(ox+T.tx[0], oy+T.m-2).LineTo(ox+T.tx[1], oy+T.m-2).LineTo(ox+T.bx[1], oy+T.b).LineTo(ox+T.bx[0], oy+T.b).Close()
		p.ClipPath(&clip.Path, func() {
			if !f.dumpT0.IsZero() {
				a.paintZapBalls(p, ox, oy)
				return
			}
			hl := f.hov
			if hl == "" {
				hl = f.rowHL
			}
			if hl != f.hlKey {
				f.hlKey, f.hlT0 = hl, a.now
			}
			sw := 1 + .35*float32(easeStd(a.progress(f.hlT0, 0, 150*time.Millisecond)))
			var hb *pileBall
			for _, e := range ents {
				if f.inflight[e.key] > 0 {
					continue
				}
				b, ok := f.pile[e.key]
				if !ok {
					continue
				}
				if e.key == hl && f.heroK > 0 {
					hb = &b
					continue
				}
				x, y, sx, sy := ox+b.x, oy+b.y, float32(1), float32(1)
				if t, ok := f.landed[e.key]; ok {
					k := a.progress(t, 0, 240*time.Millisecond)
					if k >= 1 {
						delete(f.landed, e.key)
					} else {
						// settle: translateY(-6) scale(1.1,.85) → rest on a cubic, no rebound
						v := 1 - float32(easeStd(k))
						sx, sy = 1+.1*v, 1-.15*v
						y -= 6 * v
					}
				}
				if sx != 1 || sy != 1 {
					px, py := pen.pt(x, y+b.r)
					p.Transform(ui.Scaling(sx, sy), px, py, func() { a.paintBall(p, x, y, b.r, a.pal.level[b.lv], 1) })
				} else {
					a.paintBall(p, x, y, b.r, a.pal.level[b.lv], 1)
				}
			}
			if hb != nil {
				x, y, r := ox+hb.x, oy+hb.y, hb.r*sw
				a.paintBall(p, x, y, r, a.pal.level[hb.lv], 1)
				p.StrokePath(&(&penPath{}).Circle(x, y, r+.6).Path, pen.w(1.2), ui.RGBA(255, 255, 255, .92))
			}
		})
		a.paintZapSparks(p, ox, oy)
	})
}

// paintBall draws one item: a flat disc with a soft inset shade, like the prototype's #pile i.
func (a *app) paintBall(p *ui.Painter, x, y, r float32, c ui.Color, alpha float32) {
	if r <= .2 || alpha <= 0 {
		return
	}
	var disc penPath
	disc.Circle(x, y, r)
	if a.pal.art == artInk { // e-ink: a flat disc and an ink line, no light on it
		p.FillPath(&disc.Path, c.Alpha(alpha))
		p.StrokePath(&disc.Path, max(.8, min(1.4, r*.12)), a.pal.ink.Alpha(.9*alpha))
		return
	}
	p.ClipPath(&disc.Path, func() {
		p.FillPath(&disc.Path, c.Mix(ui.RGBA(0, 0, 0, 1), .22).Alpha(alpha))
		p.FillPath(&(&penPath{}).Circle(x-min(1.5, r*.2), y-min(1.5, r*.2), r).Path, c.Alpha(alpha))
		p.FillPath(&(&penPath{}).Circle(x-r*.35, y-r*.35, r*.32).Path, ui.RGBA(255, 255, 255, .22*alpha))
	})
}

// paintBinFront draws the glass, rim and lid over the flying balls (in the window overlay).
func (a *app) paintBinFront(p *ui.Painter) {
	f := &a.binS
	if f.right == 0 {
		return
	}
	T := &binTiers[f.tier]
	ik := a.binInk()
	lid := a.lidAngle()
	_, _, op := a.tierScale()
	if op < 1 {
		ik.bodyLine = ik.bodyLine.Alpha(op * .16)
	}
	if f.hot {
		ik.bodyLine = a.pal.ink.Alpha(.33)
	}
	a.binXform(p, func(ox, oy float32) {
		P := func(d string) *ui.Path { return svgPath(d, ox, oy) }
		body := P(T.body)
		p.FillPath(body, ik.bodyFill)
		p.StrokePath(body, pen.w(1.2), ik.bodyLine)
		rim := ik.rim
		if T.lids == 0 && lid > 0 {
			rim = a.pal.ink.Alpha(.45)
		}
		switch f.tier {
		case 0:
			halfEllipse(p, ox+27, oy+15, 21, 3.6, rim, 1.8)
			p.StrokePath(P("M10.5 21L12.6 52").Cap(ui.RoundCap), pen.w(2.2), ik.detail)
		case 1:
			p.ClipPath(body, func() {
				var m penPath
				for x := float32(-70); x < 140; x += 7 {
					m.MoveTo(ox+x, oy+17).LineTo(ox+x+60, oy+77).MoveTo(ox+x+60, oy+17).LineTo(ox+x, oy+77)
				}
				p.StrokePath(&m.Path, pen.w(.9), ik.detail)
			})
			p.StrokePath(P("M11.2 69H58.8"), pen.w(3), ik.detail)
			halfEllipse(p, ox+35, oy+17, 30, 4.6, rim, 2.2)
		case 2:
			p.StrokePath(P("M17.7 28L22.7 92M27.4 28L30.4 93M37.1 28L38.1 93M46.9 28L45.9 93M56.6 28L53.6 93M66.3 28L61.3 92"), pen.w(1), ik.detail)
			p.StrokePath(P("M12.5 34L16 88").Cap(ui.RoundCap), pen.w(2.5), ik.detail)
			fillR(p, ui.Rect{X: ox + 5.5, Y: oy + 21, W: 73, H: 5}, ik.rimFill, 2.5)
		case 3:
			p.StrokePath(P("M24 42L27 116M38 42L39.5 117M54 42V117M70 42L68.5 117M84 42L81 116"), pen.w(1), ik.detail)
			p.StrokePath(P("M13.5 60H94.5"), pen.w(5), ik.detail)
			p.StrokePath(rrect(ox+42, oy+74, 24, 15, 2), pen.w(1), ik.detail)
			p.StrokePath(P("M15 46L19.5 112").Cap(ui.RoundCap), pen.w(3), ik.detail)
			fillR(p, ui.Rect{X: ox + 7, Y: oy + 33, W: 94, H: 5.5}, ik.rimFill, 2.5)
			for _, cx := range []float32{25, 83} {
				wheel(p, ox+cx, oy+126, 9, 3, ik)
			}
		case 4:
			p.StrokePath(P("M12 58H160M14.5 79H157.5"), pen.w(1.2), ik.detail)
			p.StrokePath(P("M50 40L52 100M86 40V100M122 40L120 100"), pen.w(1), ik.detail)
			for _, x := range []float32{28, 110} {
				fillR(p, ui.Rect{X: ox + x, Y: oy + 85, W: 34, H: 9}, ik.open, 1.5)
				p.StrokePath(rrect(ox+x, oy+85, 34, 9, 1.5), pen.w(1), ik.detail)
			}
			p.StrokePath(P("M13 44L18 96").Cap(ui.RoundCap), pen.w(3), ik.detail)
			fillR(p, ui.Rect{X: ox + 5, Y: oy + 33, W: 162, H: 5.5}, ik.rimFill, 2.5)
			fillR(p, ui.Rect{X: ox + 27, Y: oy + 102, W: 7, H: 5}, ik.wheel, 0)
			fillR(p, ui.Rect{X: ox + 138, Y: oy + 102, W: 7, H: 5}, ik.wheel, 0)
			for _, cx := range []float32{30.5, 141.5} {
				wheel(p, ox+cx, oy+110, 6.5, 0, ik)
			}
		}
		// lids
		for i := 0; i < T.lids; i++ {
			pv := T.pivots[i]
			deg := -lid
			if i == 1 {
				deg = lid
			}
			px, py := pen.pt(ox+pv[0], oy+pv[1])
			p.Transform(ui.Rotation(deg), px, py, func() {
				switch f.tier {
				case 2:
					var h penPath
					h.MoveTo(ox+33, oy+14.5).LineTo(ox+33, oy+11).QuadTo(ox+33, oy+8, ox+36, oy+8).LineTo(ox+48, oy+8).
						QuadTo(ox+51, oy+8, ox+51, oy+11).LineTo(ox+51, oy+14.5)
					p.StrokePath(&h.Path, pen.w(2.6), ik.handle)
					fillR(p, ui.Rect{X: ox + 4, Y: oy + 14, W: 76, H: 7.5}, ik.lid, 3.5)
					fillR(p, ui.Rect{X: ox + 4, Y: oy + 14, W: 76, H: 2.5}, ik.lidHi, 1.25)
				case 3:
					fillR(p, ui.Rect{X: ox + 42, Y: oy + 19.5, W: 24, H: 4.5}, ik.handle, 2.2)
					fillR(p, ui.Rect{X: ox + 4, Y: oy + 24, W: 100, H: 10}, ik.lid, 3)
					fillR(p, ui.Rect{X: ox + 4, Y: oy + 24, W: 100, H: 3}, ik.lidHi, 1.5)
				case 4:
					x, c := float32(4), ik.lid
					if i == 1 {
						x, c = 86, ik.lid2
					}
					fillR(p, ui.Rect{X: ox + x, Y: oy + 25, W: 82, H: 9}, c, 2.5)
					fillR(p, ui.Rect{X: ox + x, Y: oy + 25, W: 82, H: 2.6}, ik.lidHi, 1.3)
				}
			})
		}
	})
}

func wheel(p *ui.Painter, x, y, r, hub float32, ik binInk) {
	var w penPath
	w.Circle(x, y, r)
	p.FillPath(&w.Path, ik.wheel)
	p.StrokePath(&w.Path, pen.w(1), ik.wheelLine)
	if hub > 0 {
		p.FillPath(&(&penPath{}).Circle(x, y, hub).Path, ik.hub)
	}
}

const kappa = .5523

func ellipse(p *ui.Painter, cx, cy, rx, ry float32, fill, line ui.Color, w float32) {
	var e penPath
	e.MoveTo(cx-rx, cy).
		CubeTo(cx-rx, cy-kappa*ry, cx-kappa*rx, cy-ry, cx, cy-ry).
		CubeTo(cx+kappa*rx, cy-ry, cx+rx, cy-kappa*ry, cx+rx, cy).
		CubeTo(cx+rx, cy+kappa*ry, cx+kappa*rx, cy+ry, cx, cy+ry).
		CubeTo(cx-kappa*rx, cy+ry, cx-rx, cy+kappa*ry, cx-rx, cy).Close()
	p.FillPath(&e.Path, fill)
	p.StrokePath(&e.Path, pen.w(w), line)
}

// halfEllipse strokes the front (lower) half of a rim.
func halfEllipse(p *ui.Painter, cx, cy, rx, ry float32, c ui.Color, w float32) {
	var e penPath
	e.MoveTo(cx-rx, cy).
		CubeTo(cx-rx, cy+kappa*ry, cx-kappa*rx, cy+ry, cx, cy+ry).
		CubeTo(cx+kappa*rx, cy+ry, cx+rx, cy+kappa*ry, cx+rx, cy)
	p.StrokePath(e.Path.Cap(ui.RoundCap), pen.w(w), c)
}

func rrect(x, y, w, h, r float32) *ui.Path {
	var p penPath
	p.MoveTo(x+r, y).LineTo(x+w-r, y).QuadTo(x+w, y, x+w, y+r).LineTo(x+w, y+h-r).QuadTo(x+w, y+h, x+w-r, y+h).
		LineTo(x+r, y+h).QuadTo(x, y+h, x, y+h-r).LineTo(x, y+r).QuadTo(x, y, x+r, y).Close()
	return &p.Path
}

// svgPath parses SVG path data (M L H V Q C Z, absolute or relative), offset by (ox, oy).
var svgCache = map[string][]svgOp{}

type svgOp struct {
	c byte
	v [6]float32
}

func svgPath(d string, ox, oy float32) *ui.Path {
	ops, ok := svgCache[d]
	if !ok {
		ops = parseSVG(d)
		svgCache[d] = ops
	}
	var p penPath
	var x, y, sx, sy float32
	for _, o := range ops {
		rel := o.c >= 'a'
		var bx, by float32
		if rel {
			bx, by = x, y
		}
		v := o.v
		switch o.c | 0x20 {
		case 'm':
			x, y = bx+v[0], by+v[1]
			sx, sy = x, y
			p.MoveTo(ox+x, oy+y)
		case 'l':
			x, y = bx+v[0], by+v[1]
			p.LineTo(ox+x, oy+y)
		case 'h':
			x = bx + v[0]
			p.LineTo(ox+x, oy+y)
		case 'v':
			y = by + v[0]
			p.LineTo(ox+x, oy+y)
		case 'q':
			p.QuadTo(ox+bx+v[0], oy+by+v[1], ox+bx+v[2], oy+by+v[3])
			x, y = bx+v[2], by+v[3]
		case 'c':
			p.CubeTo(ox+bx+v[0], oy+by+v[1], ox+bx+v[2], oy+by+v[3], ox+bx+v[4], oy+by+v[5])
			x, y = bx+v[4], by+v[5]
		case 'z':
			p.Close()
			x, y = sx, sy
		}
	}
	return &p.Path
}

var svgArity = map[byte]int{'m': 2, 'l': 2, 'h': 1, 'v': 1, 'q': 4, 'c': 6, 'z': 0}

func parseSVG(d string) []svgOp {
	var ops []svgOp
	var cmd byte
	var nums []float32
	flush := func() {
		if cmd == 0 {
			return
		}
		n := svgArity[cmd|0x20]
		if n == 0 {
			ops = append(ops, svgOp{c: cmd})
			return
		}
		for i := 0; i+n <= len(nums); i += n {
			var o svgOp
			o.c = cmd
			if cmd|0x20 == 'm' && i > 0 {
				o.c = 'L' | cmd&0x20 // further pairs after a move are lines
			}
			copy(o.v[:], nums[i:i+n])
			ops = append(ops, o)
		}
	}
	i := 0
	for i < len(d) {
		ch := d[i]
		switch {
		case (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z'):
			flush()
			cmd, nums = ch, nums[:0]
			i++
		case ch == '-' || ch == '.' || (ch >= '0' && ch <= '9'):
			j := i + 1
			dot := ch == '.'
			for j < len(d) {
				c := d[j]
				if c >= '0' && c <= '9' {
					j++
				} else if c == '.' && !dot {
					dot = true
					j++
				} else {
					break
				}
			}
			v, _ := strconv.ParseFloat(d[i:j], 32)
			nums = append(nums, float32(v))
			i = j
		default:
			i++
		}
	}
	flush()
	return ops
}
