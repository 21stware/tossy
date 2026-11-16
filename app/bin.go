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

// The trash bin grows with what is in it: a desk basket, a waste basket (the app icon's bin), a
// pedal bin, a wheelie bin, a skip and a roll-off container, all of one moulded plastic: a rolled
// rim, a tinted body the pile shows through, pressed grooves. Items lie in it as balls packed by a
// height map, coloured by safety level; lids open and rims light while things fly in, the bin
// bumps as each lands, and tips its pile out on empty. Ported from the prototype's TIERS,
// layoutPile and the bin CSS animations.

type binTier struct {
	w, h float32
	f    float64 // trash/capacity at which this tier starts

	// the rim: left, top, width; its corners' radius, its depth (the ellipse's half height) and
	// the rolled lip's thickness
	rx, ry, rw, rr, rd, rt float32
	// the body: top y and sides, bottom y and sides (where the sides would meet the floor), the
	// bottom corners' radius and the floor's sag
	y0, l0, r0, y1, l1, r1, bc, sag float32

	grooves []groove
	gw      float32 // their width

	lids   int     // 0, 1 (hinged on the left) or 2 (on the left and on the right)
	lidT   float32 // a lid's thickness
	handle bool    // a bar behind the lid's hinge
	pedal  bool
	wheels [][3]float32 // x, y, r
	rail   bool         // a skid under the body

	// derived (see derive): the pile's top and bottom, its sides at the top and at the floor,
	// and the opening's middle
	m, b   float32
	tx, bx [2]float32
	open   float32
}

// groove is a pressed line in the body: vertical at fraction at of the body's width from y a to
// y b, or (flat) horizontal at y at from fraction a of the width to fraction b.
type groove struct {
	at, a, b float32
	flat     bool
}

func (T *binTier) sideL(y float32) float32 { return T.l0 + (T.l1-T.l0)*(y-T.y0)/(T.y1-T.y0) }
func (T *binTier) sideR(y float32) float32 { return T.r0 + (T.r1-T.r0)*(y-T.y0)/(T.y1-T.y0) }

func (T binTier) derive() binTier {
	T.m = T.ry + 2*T.rd + T.rt*.5
	T.b = T.y1 - 1.5
	T.tx = [2]float32{T.sideL(T.m) + 2, T.sideR(T.m) - 2}
	T.bx = [2]float32{T.l1 + T.bc*.75, T.r1 - T.bc*.75}
	T.open = T.ry + T.rd
	return T
}

// vgrooves spaces n vertical grooves across the body, from y a to y b.
func vgrooves(n int, lo, hi, a, b float32) []groove {
	g := make([]groove, n)
	for i := range g {
		g[i] = groove{at: lo + (hi-lo)*float32(i)/float32(max(1, n-1)), a: a, b: b}
	}
	return g
}

var binTiers = []binTier{
	// desk basket: short and wide
	binTier{w: 46, h: 42, f: 0, rx: 0, ry: 0, rw: 46, rr: 6.3, rd: 4.5, rt: 3.3,
		y0: 8.6, l0: 2, r0: 44, y1: 41, l1: 8, r1: 38, bc: 6.5, sag: .8,
		grooves: vgrooves(3, .3, .7, 18, 33), gw: 4}.derive(),
	// waste basket: the app icon's bin
	binTier{w: 58, h: 59.2, f: .004, rx: 0, ry: 0, rw: 58, rr: 7.9, rd: 5.7, rt: 4.2,
		y0: 11.1, l0: 2.5, r0: 55.5, y1: 58.2, l1: 8.1, r1: 49.9, bc: 8.6, sag: 1.2,
		grooves: vgrooves(3, .33, .67, 25.7, 51.6), gw: 5.2}.derive(),
	// pedal bin: taller, a lid and a pedal
	binTier{w: 70, h: 100, f: .03, rx: 2, ry: 9, rw: 66, rr: 9, rd: 6.4, rt: 4.8,
		y0: 22, l0: 4.5, r0: 65.5, y1: 96, l1: 10.5, r1: 59.5, bc: 9, sag: 1.2,
		grooves: vgrooves(3, .33, .67, 44, 84), gw: 5.6, lids: 1, lidT: 7, pedal: true}.derive(),
	// wheelie bin: tall and nearly straight, a lid, a handle and two wheels
	binTier{w: 88, h: 132, f: .08, rx: 2, ry: 12, rw: 84, rr: 10, rd: 7, rt: 5.5,
		y0: 26, l0: 4.5, r0: 83.5, y1: 121, l1: 9, r1: 79, bc: 8, sag: 1,
		grooves: vgrooves(3, .3, .7, 50, 108), gw: 6, lids: 1, lidT: 8, handle: true,
		wheels: [][3]float32{{13, 123, 9}, {75, 123, 9}}}.derive(),
	// skip: wide with sloped ends, two lids, ribs and casters
	binTier{w: 150, h: 104, f: .15, rx: 2, ry: 11, rw: 146, rr: 10, rd: 7, rt: 5,
		y0: 25, l0: 4, r0: 146, y1: 93, l1: 17, r1: 133, bc: 7, sag: .5,
		grooves: []groove{{at: 48, a: .1, b: .9, flat: true}, {at: 70, a: .12, b: .88, flat: true}}, gw: 5,
		lids: 2, lidT: 8, wheels: [][3]float32{{30, 97.5, 6}, {120, 97.5, 6}}}.derive(),
	// roll-off container: long and low, open, ribbed, on a skid
	binTier{w: 180, h: 92, f: .26, rx: 2, ry: 2, rw: 176, rr: 8, rd: 6, rt: 4.5,
		y0: 14, l0: 4, r0: 176, y1: 78, l1: 7, r1: 173, bc: 6,
		grooves: vgrooves(7, .1, .9, 25, 70), gw: 5, rail: true,
		wheels: [][3]float32{{22, 87, 5}, {90, 87, 5}, {158, 87, 5}}}.derive(),
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
	truck   *truckRun // or a vehicle takes the pile away (see truck.go)
	freed   string
	freedT  time.Time

	pileK   [3]int64
	pile    map[string]pileBall
	ents    []pileEnt
	entsAt  time.Time
	shoveT0 time.Time

	// the hero bin of the trash scene: the dock bin grows to the column's bottom centre
	heroSp         spring
	heroX          float32 // the spring as stepped at the start of the frame (see stepHero)
	heroK, heroS   float32 // heroK: heroX up to 1
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
	flying     bool // this frame the size is the flying copy: the dock's and the header's hide
	// the trash chart's centre says the same: it is hidden while the size flies, and fades
	// back in once it has landed
	centerSp spring
	centerA  float32
}

// stepHero moves the hero spring once per frame, before anything is built: the trash
// header is built before the dock, and both must see the same progress, or on landing
// neither copy of the size shows for a frame (and on the way back both do).
func (a *app) stepHero() {
	f := &a.binS
	if !a.shown || a.chartR.W == 0 { // no dock this frame (see the app's frame)
		f.flying = false
		return
	}
	hk := 0.0
	if a.sceneOn() == sceneTrash {
		hk = 1
	}
	k := max(0, float32(a.springTo(&f.heroSp, hk)))
	f.heroX, f.heroK = k, min(k, 1)
	f.flying = k > 0 && k < 1 && f.szSet && a.motion()
	if f.flying { // gone at once: no second copy while the size moves
		f.centerSp.snap(0)
	}
	f.centerA = max(0, min(1, float32(a.springTo(&f.centerSp, 1))))
}

// dockS is the bin's scale at rest in the dock: the tiers are drawn at 1 and zoomed by it.
// The small bins (the basket, the waste basket) are zoomed further, up to dockSmax, so the
// empty or nearly empty dock still shows a real bin.
const dockS, dockSmax = 1.3, 1.6

// dockScale is the dock zoom for a tier.
func dockScale(tier int) float32 {
	return max(dockS, min(dockSmax, 96/binTiers[tier].h))
}

// dockFS is the font size of the dock's trash size text.
const dockFS = 26

// hero is the zoom from the dock slot: the slot's bottom centre, the scale and the shift.
func (f *binState) hero() (bx, by, s, dx, dy float32) {
	T := &binTiers[f.tier]
	bx, by = f.right-T.w/2, f.bottom
	k := f.heroK
	ds := dockScale(f.tier)
	return bx, by, ds + (f.heroS-ds)*k, (f.heroTX - bx) * k, (f.heroTY - by) * k
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
	if f.truck != nil { // the pile stays as it was until the truck has gone
		f.ents = append(f.ents[:0], f.truck.ents...)
		return f.ents
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
	case f.truck != nil && a.now.Sub(f.truck.t0) > truckArrive-200*time.Millisecond && f.truck.leaveT0.IsZero():
		target = 55 // open while the truck loads
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

// binInk is the bin's material, in five shades of one colour, made the way the style draws its
// chart: the icon's mint plastic by default; a translucent body with glowing edges on the HUD;
// a cobalt plate inked in navy on the mech; graphite on classic; white or phosphor linework on
// the blueprint and the terminal; light grey hardware with an orange rim on TE; candy in the
// theme's colour, thickly inked, on the cartoon styles; and e-ink's outlines.
type binInk struct {
	deep, dark, base, light, hi ui.Color
	rubber                      ui.Color
	glass                       float32 // the body's opacity: the pile shows through it
	line                        bool    // linework, no light (e-ink, blueprint, terminal)
	ink                         ui.Color
	wash                        float32  // linework: the body's faint fill
	glow                        bool     // lines and edges glow
	edge                        ui.Color // an outline over the solid parts, if not zero
	edgeW                       float32
	rim                         ui.Color // the rim and the lids in a colour of their own, if not zero
	// soft shades it high-key, as clean light hardware: a colourless grey shaded with as
	// much black as tinted plastic takes turns to mud, its insides most of all
	soft bool
}

// shades sets the five shades from base.
func (ik binInk) shades(base ui.Color) binInk {
	black, white := ui.RGB(0, 0, 0), ui.RGB(255, 255, 255)
	deep, dark, light, hi := float32(.55), float32(.3), float32(.38), float32(.7)
	if ik.soft {
		deep, dark, light, hi = .24, .1, .55, .85
	}
	ik.deep, ik.dark, ik.base, ik.light, ik.hi = base.Mix(black, deep), base.Mix(black, dark), base, base.Mix(white, light), base.Mix(white, hi)
	return ik
}

// rimInk is the ink of the rim and the lids.
func (ik binInk) rimInk() binInk {
	if ik.rim.A == 0 {
		return ik
	}
	return ik.shades(ik.rim)
}

func (a *app) binInk() binInk {
	pal := a.pal
	black, white := ui.RGB(0, 0, 0), ui.RGB(255, 255, 255)
	base := pal.level[0]
	switch pal.art {
	case artHolo, artPlate:
		base = pal.accent
	case artSolid:
		base = pal.t2.Mix(black, .25)
	case artTE:
		base = ui.Hex("#e2e2de") // the panels' off-white
	case artToon:
		base = pal.accent.Mix(white, .35)
	}
	// on a light ground the heavy shading of tinted plastic reads as dirt (a murky body, a dark
	// inside through it): light themes shade high-key
	ik := binInk{glass: .4, ink: pal.ink, soft: !pal.dark}.shades(base)
	ik.rubber = base.Mix(black, .78)
	if !pal.dark {
		ik.glass = .34
	}
	switch pal.art {
	case artInk:
		ik.line = true
	case artBlueprint:
		ik.line, ik.wash = true, .07
	case artPhosphor:
		ik.line, ik.wash, ik.glow = true, .05, true
	case artHolo:
		ik.glass, ik.edge, ik.edgeW, ik.glow = .16, pal.ink.Alpha(.85), 1, true
	case artPlate:
		ik.glass, ik.edge, ik.edgeW = .5, pal.ink, 1.3
	case artTE:
		ik.glass, ik.rim, ik.rubber = .72, pal.accent, ui.Hex("#2a2a2a")
		ik.edge, ik.edgeW = ui.RGBA(0, 0, 0, .18), .8
	case artToon:
		ik.glass, ik.edge, ik.edgeW = .5, pal.ink, 1.8 // candy: its colour, not the ground's, through the shell
		ik.rubber = pal.ink
	}
	return ik
}

// inkLine strokes linework w wide, glowing where the style glows.
func (a *app) inkLine(p *ui.Painter, path *ui.Path, w float32, ik binInk) {
	if ik.glow {
		p.StrokePath(path, pen.w(w*4), ik.ink.Alpha(.16))
	}
	p.StrokePath(path, pen.w(w), ik.ink.Alpha(.9))
}

// edgeLine outlines a solid part, if the style inks them.
func (a *app) edgeLine(p *ui.Painter, path *ui.Path, ik binInk) {
	if ik.edge.A == 0 {
		return
	}
	if ik.glow {
		p.StrokePath(path, pen.w(ik.edgeW*4), ik.edge.Alpha(.18))
	}
	p.StrokePath(path, pen.w(ik.edgeW), ik.edge)
}

// rrXY is a rounded rectangle with elliptic corners, through the pen.
func rrXY(x, y, w, h, rx, ry float32) *ui.Path {
	var q penPath
	kx, ky := kappa*rx, kappa*ry
	q.MoveTo(x+rx, y).LineTo(x+w-rx, y).CubeTo(x+w-rx+kx, y, x+w, y+ry-ky, x+w, y+ry).
		LineTo(x+w, y+h-ry).CubeTo(x+w, y+h-ry+ky, x+w-rx+kx, y+h, x+w-rx, y+h).
		LineTo(x+rx, y+h).CubeTo(x+rx-kx, y+h, x, y+h-ry+ky, x, y+h-ry).
		LineTo(x, y+ry).CubeTo(x, y+ry-ky, x+rx-kx, y, x+rx, y).Close()
	return &q.Path
}

// band is the rolled front of a rim (or a lid) whose top is the ellipse-cornered box at (x, y),
// w wide and 2·d deep: its near edge, thickened down by t.
func band(x, y, w, r, d, t float32) *ui.Path {
	var q penPath
	kx, ky := kappa*r, kappa*d
	q.MoveTo(x, y+d).LineTo(x, y+d+t).CubeTo(x, y+d+t+ky, x+r-kx, y+2*d+t, x+r, y+2*d+t).
		LineTo(x+w-r, y+2*d+t).CubeTo(x+w-r+kx, y+2*d+t, x+w, y+d+t+ky, x+w, y+d+t).
		LineTo(x+w, y+d).CubeTo(x+w, y+d+ky, x+w-r+kx, y+2*d, x+w-r, y+2*d).
		LineTo(x+r, y+2*d).CubeTo(x+r-kx, y+2*d, x, y+d+ky, x, y+d).Close()
	return &q.Path
}

// opening is the hole inside a rim's top, as the icon has it: inset by 3/8 of the corner
// radius at the sides, and a little more at the near edge than at the far one.
func opening(x, y, w, r, d float32) (ox, oy, ow, oh, orx, ory float32) {
	ix, iy := r*.375, d*.26
	return x + ix, y + iy, w - 2*ix, 2*d - iy*2.67, r - ix*.67, d - iy*1.33
}

// rimNear is the near half of a rim's top, between the opening and the rim's edge.
func rimNear(x, y, w, r, d float32) *ui.Path {
	ox, oy, ow, oh, orx, ory := opening(x, y, w, r, d)
	var q penPath
	kx, ky, kix, kiy := kappa*r, kappa*d, kappa*orx, kappa*ory
	ib := oy + oh // the opening's near edge
	q.MoveTo(x, y+d).CubeTo(x, y+d+ky, x+r-kx, y+2*d, x+r, y+2*d).
		LineTo(x+w-r, y+2*d).CubeTo(x+w-r+kx, y+2*d, x+w, y+d+ky, x+w, y+d).
		LineTo(ox+ow, ib-ory).CubeTo(ox+ow, ib-ory+kiy, ox+ow-orx+kix, ib, ox+ow-orx, ib).
		LineTo(ox+orx, ib).CubeTo(ox+orx-kix, ib, ox, ib-ory+kiy, ox, ib-ory).Close()
	return &q.Path
}

// tubBody is the body's outline: tapering sides, rounded bottom corners and a sagging floor.
func tubBody(T *binTier, ox, oy float32) *ui.Path {
	var q penPath
	c, y1 := T.bc, oy+T.y1
	q.MoveTo(ox+T.l0, oy+T.y0).LineTo(ox+T.sideL(T.y1-c), y1-c).
		QuadTo(ox+T.l1+.12*c, y1, ox+T.l1+c, y1+.2*T.sag).QuadTo(ox+(T.l1+T.r1)/2, y1+T.sag, ox+T.r1-c, y1+.2*T.sag).
		QuadTo(ox+T.r1-.12*c, y1, ox+T.sideR(T.y1-c), y1-c).LineTo(ox+T.r0, oy+T.y0).Close()
	return &q.Path
}

// grooveLine is a groove's centre line, its caps' centres inset by half its width; lit is its
// lower, lit side (the light comes from the top left).
func grooveLine(T *binTier, g groove, ox, oy float32, lit bool) *ui.Path {
	var q penPath
	h := T.gw / 2
	if g.flat {
		y := g.at
		x0 := T.sideL(y) + (T.sideR(y)-T.sideL(y))*g.a + h
		x1 := T.sideL(y) + (T.sideR(y)-T.sideL(y))*g.b - h
		if lit {
			x0, x1, y = x0+h*.4, x1-h*.2, y+h*.45
		}
		q.MoveTo(ox+x0, oy+y).LineTo(ox+x1, oy+y)
		return &q.Path
	}
	ya, yb := g.a+h, g.b-h
	xa := T.sideL(ya) + (T.sideR(ya)-T.sideL(ya))*g.at
	xb := T.sideL(yb) + (T.sideR(yb)-T.sideL(yb))*g.at
	if lit {
		xa, xb, ya = xa+h*.35, xb+h*.35, ya+h*.8
	}
	q.MoveTo(ox+xa, oy+ya).LineTo(ox+xb, oy+yb)
	return &q.Path
}

// paintLid draws lid i, closed or turned up about its outer end by deg.
func (a *app) paintLid(p *ui.Painter, T *binTier, i int, ox, oy, deg float32, lit float32, ik binInk) {
	w := T.rw / float32(T.lids)
	x, y := ox+T.rx+w*float32(i)-1.5, oy+T.ry-T.lidT
	w += 3
	r := min(T.rr, w/2)
	px := x
	if i == 1 {
		px, deg = x+w, -deg
	}
	py := y + T.rd + T.lidT
	ppx, ppy := pen.pt(px, py)
	p.Transform(ui.Rotation(-deg), ppx, ppy, func() {
		top, front := rrXY(x, y, w, 2*T.rd, r, T.rd), band(x, y, w, r, T.rd, T.lidT)
		if ik.line {
			p.FillPath(top, a.pal.bg)
			p.FillPath(front, a.pal.bg)
			a.inkLine(p, top, 1.2, ik)
			a.inkLine(p, front, 1.2, ik)
			return
		}
		ik := ik.rimInk()
		defer func() {
			a.edgeLine(p, top, ik)
			a.edgeLine(p, front, ik)
		}()
		p.FillPathGradient(top, ui.LinearGradient{Angle: 180, From: ik.base.Mix(ik.light, .5+.3*lit), To: ik.light.Mix(ik.hi, .3*lit)})
		p.FillPathGradient(front, ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{
			{Color: ik.light, At: 0}, {Color: ik.base, At: .45}, {Color: ik.dark, At: 1}}})
		p.FillPathGradient(front, ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
			{Color: ik.deep.Alpha(.3), At: 0}, {Color: ik.deep.Alpha(0), At: .12}, {Color: ui.RGBA(255, 255, 255, 0), At: .7},
			{Color: ui.RGBA(255, 255, 255, .16), At: .86}, {Color: ui.RGBA(255, 255, 255, .05), At: 1}}})
		// a grip on the front
		gx := x + w*.5
		if T.lids == 2 {
			gx = x + w*float32(.7-.4*float32(i))
		}
		fillR(p, ui.Rect{X: gx - 6, Y: y + 2*T.rd + T.lidT*.35, W: 12, H: 2.4}, ik.dark.Alpha(.7), 1.2)
	})
}

func (a *app) paintWheel(p *ui.Painter, x, y, r float32, ik binInk) {
	var w penPath
	w.Circle(x, y, r)
	if ik.line {
		p.FillPath(&w.Path, a.pal.bg)
		a.inkLine(p, &w.Path, 1.2, ik)
		return
	}
	p.FillPath(&w.Path, ik.rubber)
	p.FillPath(&(&penPath{}).Circle(x-r*.08, y-r*.08, r*.86).Path, ik.rubber.Mix(ui.RGB(255, 255, 255), .1))
	p.FillPath(&(&penPath{}).Circle(x, y, r*.42).Path, ik.dark)
	p.FillPath(&(&penPath{}).Circle(x-r*.08, y-r*.1, r*.3).Path, ik.base)
	a.edgeLine(p, &w.Path, ik)
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
		if !ok || f.inflight[e.key] > 0 || !f.dumpT0.IsZero() || f.truck != nil {
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
		sx := f.heroTX
		p.Transform(ui.Scaling(1, 9/w), sx, f.heroTY+.5, func() {
			p.FillRadialGradient(ui.Rect{X: sx - w/2, Y: f.heroTY + .5 - w/2, W: w, H: w}, ui.RadialGradient{
				Radius: w / 2,
				Stops:  []ui.GradientStop{{Color: ui.RGBA(0, 0, 0, sh*k), At: 0}, {Color: ui.RGBA(0, 0, 0, 0), At: 1}},
			}, 0)
		})
	}
	a.binXform(p, func(ox, oy float32) {
		x, y, w, r, d := ox+T.rx, oy+T.ry, T.rw, T.rr, T.rd
		ox2, oy2, ow, oh, orx, ory := opening(x, y, w, r, d)
		if T.handle {
			hc := ik.dark
			if ik.line {
				hc = ik.ink.Alpha(.6)
			}
			p.StrokePath((&penPath{}).MoveTo(ox+T.w*.24, oy+T.ry-T.lidT-1.5).LineTo(ox+T.w*.76, oy+T.ry-T.lidT-1.5).Cap(ui.RoundCap), pen.w(4.5), hc)
		}
		if ik.line {
			if ik.wash > 0 {
				p.FillPath(tubBody(T, ox, oy), ik.ink.Alpha(ik.wash))
			}
			a.inkLine(p, rrXY(x, y, w, 2*d+T.rt, r, d), 1.2, ik)
			a.inkLine(p, rrXY(ox2, oy2, ow, oh, orx, ory), 1, ik)
		} else {
			// the inside: the back wall through the body, then the rim seen from above
			inside := ui.LinearGradient{Angle: 180, From: ik.deep.Mix(ui.RGB(0, 0, 0), .4).Alpha(.7), To: ik.deep.Alpha(.45)}
			if ik.soft { // a clean inside, shaded only under the rim
				inside = ui.LinearGradient{Angle: 180, From: ik.deep.Alpha(.45), To: ik.dark.Alpha(.12)}
			}
			p.FillPathGradient(tubBody(T, ox, oy), inside)
			rk := ik.rimInk()
			p.FillPath(rrXY(x, y, w, 2*d+T.rt, r, d), rk.dark)
			p.FillPathGradient(rrXY(x, y, w, 2*d, r, d), ui.LinearGradient{Angle: 180, From: rk.base, To: rk.base.Mix(rk.light, .5)})
			p.FillPathGradient(rrXY(ox2, oy2, ow, oh, orx, ory), ui.LinearGradient{Angle: 180, From: rk.deep, To: rk.dark})
			a.edgeLine(p, rrXY(x, y, w, 2*d+T.rt, r, d), rk)
			a.edgeLine(p, rrXY(ox2, oy2, ow, oh, orx, ory), rk)
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
				if f.inflight[e.key] > 0 || (f.truck != nil && f.truck.out[e.key]) {
					continue
				}
				b, ok := f.pile[e.key]
				if !ok {
					continue
				}
				if f.truck != nil && e.key == f.truck.working {
					// being deleted: it trembles, its turn is next
					b.x += float32(math.Sin(float64(a.now.UnixMilli())*.9)) * .5
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

// paintBinFront draws the tinted body, its grooves, wheels, the rim's front and the lids over the
// pile and the flying balls (in the window overlay).
func (a *app) paintBinFront(p *ui.Painter) {
	f := &a.binS
	if f.right == 0 {
		return
	}
	T := &binTiers[f.tier]
	ik := a.binInk()
	lid := a.lidAngle()
	// an open rim lights while things go in, and more while a drag hovers the bin
	lit := min(1, lid/58)
	if f.hot {
		lit = 1
	}
	a.binXform(p, func(ox, oy float32) {
		body := tubBody(T, ox, oy)
		x, y, w, r, d := ox+T.rx, oy+T.ry, T.rw, T.rr, T.rd
		// what stands behind the body's foot: the skid, then the wheels
		if T.rail {
			c := ik.rubber
			if ik.line {
				c = ik.ink.Alpha(.6)
			}
			fillR(p, ui.Rect{X: ox + T.l1 + 2, Y: oy + T.y1 - 1, W: T.r1 - T.l1 - 4, H: 5}, c, 2)
		}
		for _, wh := range T.wheels {
			a.paintWheel(p, ox+wh[0], oy+wh[1], wh[2], ik)
		}
		if ik.line {
			a.inkLine(p, body, 1.2, ik)
			for _, g := range T.grooves {
				p.StrokePath(grooveLine(T, g, ox, oy, false).Cap(ui.RoundCap), pen.w(T.gw), ik.ink.Alpha(.15))
			}
			p.FillPath(band(x, y, w, r, d, T.rt), a.pal.bg)
			a.inkLine(p, band(x, y, w, r, d, T.rt), 1.2, ik)
		} else {
			gl := ik.glass
			p.FillPathGradient(body, ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
				// thick at the sides, where the eye looks through more plastic, clear in the middle
				{Color: ik.dark.Alpha(gl * 1.6), At: 0}, {Color: ik.base.Alpha(gl * .9), At: .1}, {Color: ik.base.Alpha(gl * .3), At: .32},
				{Color: ik.base.Alpha(gl * .3), At: .62}, {Color: ik.light.Alpha(gl), At: .86}, {Color: ik.base.Alpha(gl * 1.4), At: 1}}})
			p.FillPathGradient(body, ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{
				{Color: ik.deep.Alpha(.3), At: 0}, {Color: ik.deep.Alpha(0), At: .12}, {Color: ik.deep.Alpha(0), At: .75}, {Color: ik.deep.Alpha(.28), At: 1}}})
			// the grooves, pressed in
			for _, g := range T.grooves {
				p.StrokePathGradient(grooveLine(T, g, ox, oy, false).Cap(ui.RoundCap), pen.w(T.gw), ui.LinearGradient{Angle: 180, From: ik.deep.Alpha(.55), To: ik.dark.Alpha(.45)})
				p.StrokePath(grooveLine(T, g, ox, oy, true).Cap(ui.RoundCap), pen.w(T.gw*.57), ik.base.Alpha(.32))
			}
			p.StrokePath(body, pen.w(1), ik.light.Alpha(.45))
			a.edgeLine(p, body, ik)
			// the rim: its near top, then the rolled front
			rk := ik.rimInk()
			if T.lids == 0 {
				p.FillPathGradient(rimNear(x, y, w, r, d), ui.LinearGradient{Angle: 180, From: rk.base, To: rk.light.Mix(rk.hi, lit*.5)})
			}
			front := band(x, y, w, r, d, T.rt)
			p.FillPathGradient(front, ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{
				{Color: rk.light.Mix(rk.hi, lit*.6), At: 0}, {Color: rk.base.Mix(rk.light, lit*.4), At: .45}, {Color: rk.dark, At: 1}}})
			p.FillPathGradient(front, ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
				{Color: rk.deep.Alpha(.3), At: 0}, {Color: rk.deep.Alpha(0), At: .12}, {Color: ui.RGBA(255, 255, 255, 0), At: .7},
				{Color: ui.RGBA(255, 255, 255, .16), At: .86}, {Color: ui.RGBA(255, 255, 255, .05), At: 1}}})
			a.edgeLine(p, front, rk)
		}
		if T.pedal {
			c := ik.dark
			if ik.line {
				c = ik.ink.Alpha(.6)
			}
			fillR(p, ui.Rect{X: ox + T.w*.36, Y: oy + T.y1 - 1, W: T.w * .28, H: 4.5}, c, 2)
		}
		for i := 0; i < T.lids; i++ {
			a.paintLid(p, T, i, ox, oy, lid, lit, ik)
		}
	})
}

const kappa = .5523

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
