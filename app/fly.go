package main

import (
	"math"
	"math/rand"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/fanfree/fanfree/engine/scan"
	"github.com/fanfree/fanfree/engine/trash"
)

// flight throws removed items into the bin, as the prototype's throwItems: the source dissolves
// (an arc shrinks to a shard, a list row folds into a disc, a bare point pops), a ball arcs over
// to hover above the bin's opening, then drops onto its own slot in the pile, turning from the
// source's colour to its safety colour on the way down. It all paints in a window-wide overlay.
type flight struct {
	balls  []ball
	shards []shard
	morphs []morph
}

type ball struct {
	sx, sy, mx, my, tx, ty, hy float32
	d, endS                    float32
	c0, c1                     ui.Color
	start                      time.Time
	dur                        time.Duration
	pop                        bool
	solo                       bool // one item thrown by hand: bigger, with a trail
	keys                       []string
}

type shard struct {
	path   *ui.Path
	c      ui.Color
	ox, oy float32
	t0     time.Time
}

type morph struct {
	r       ui.Rect
	x, y, d float32
	c       ui.Color
	t0      time.Time
}

// source is where a thrown item starts: its arc, its list row, or a point.
type source struct {
	arc  *arc
	rect ui.Rect
	x, y float32
	c    ui.Color
	ball bool // already a ball (dropped from a drag): no pop
}

func (f *flight) active() bool { return len(f.balls)+len(f.shards)+len(f.morphs) > 0 }

// lastLanding is when the last ball in the air lands.
func (f *flight) lastLanding() time.Time {
	var t time.Time
	for _, b := range f.balls {
		if e := b.start.Add(b.dur); e.After(t) {
			t = e
		}
	}
	return t
}

func (a *app) sourceOf(n int32) source {
	if d := &a.drop; d.node == n && d.set {
		d.set = false
		return source{x: d.x, y: d.y, c: d.c, ball: true}
	}
	if a.throwRow.W > 0 && a.throwNode == n {
		return source{rect: a.throwRow, c: a.swatchColor(n)}
	}
	for i := range a.sun.arcs {
		if h := &a.sun.arcs[i]; h.node == n {
			cp := *h
			return source{arc: &cp, c: hsl(h.h, h.s, h.l, 1)}
		}
	}
	x, y, _ := a.arcPoint(n)
	return source{x: x, y: y}
}

// throw sends items to the bin. many is a batch from a clean: a front-loaded, jittered stagger
// for the 28 biggest, the long tail as a few clumps.
func (a *app) throw(items []cleanItem, many bool) {
	f := &a.binS
	if f.inflight == nil {
		f.inflight, f.landed = map[string]int{}, map[string]time.Time{}
	}
	srcs := make([]source, len(items))
	keys := make([]string, len(items))
	for i, it := range items {
		srcs[i] = a.sourceOf(it.node)
		keys[i] = a.t.Path(it.node)
		f.pend = append(f.pend, pileEnt{keys[i], "", it.meta.Size, it.meta.Level, it.meta.Source})
	}
	a.syncBin(a.trashEnts())
	if !many || len(items) == 1 {
		a.throwOne(srcs[0], items, keys, 0, 0)
		if !many && len(items) == 1 && len(a.fly.balls) > 0 {
			b := &a.fly.balls[len(a.fly.balls)-1]
			b.solo = true
			d := max(16, min(28, ballR(items[0].meta.Size, f.tier)*3.4))
			b.endS *= b.d / d
			b.d = d
			if n := len(a.fly.morphs); n > 0 && a.fly.morphs[n-1].x == b.sx && a.fly.morphs[n-1].y == b.sy {
				a.fly.morphs[n-1].d = d
			}
			b.dur = b.dur * 115 / 100
		}
		return
	}
	show := min(28, len(items))
	span := float64(min(620, 140+show*20))
	rnd := rand.New(rand.NewSource(int64(len(items)*31 + 7)))
	last := 0.0
	for i := 0; i < show; i++ {
		u := 0.0
		if show > 1 {
			u = float64(i) / float64(show-1)
		}
		d := span*math.Pow(u, .72) + (rnd.Float64()-.5)*34*(1-u*.5)
		last = max(last, d)
		a.throwOne(srcs[i], items[i:i+1], keys[i:i+1], ms(max(0, d)), (rnd.Float64()-.5)*.22)
	}
	rest := items[show:]
	if len(rest) == 0 {
		return
	}
	s := &a.sun
	k := min(3, len(rest))
	for g := 0; g < k; g++ {
		var part []cleanItem
		var pk []string
		for j := g; j < len(rest); j += k {
			part = append(part, rest[j])
			pk = append(pk, keys[show+j])
		}
		ang := a0Top + rnd.Float64()*tau
		rr := float64(s.r0) + rnd.Float64()*float64(s.rw)*2
		src := source{x: s.cx + float32(math.Cos(ang)*rr), y: s.cy + float32(math.Sin(ang)*rr)}
		a.throwOne(src, part, pk, ms(last*.7+60+float64(g)*(40+rnd.Float64()*50)), (rnd.Float64()-.5)*.2)
	}
}

func ms(v float64) time.Duration { return time.Duration(v * float64(time.Millisecond)) }

func (a *app) throwOne(src source, its []cleanItem, keys []string, delay time.Duration, vary float64) {
	f := &a.binS
	now := a.now
	if now.IsZero() {
		now = time.Now()
	}
	var size int64
	for _, it := range its {
		size += it.meta.Size
	}
	T := &binTiers[f.tier]
	ox, oy := f.origin()
	pos, ok := f.pile[keys[0]]
	r := ballR(size, f.tier)
	if ok {
		r = pos.r
	}
	d := max(12, min(30, ballR(size, f.tier)*2*1.8))
	c1 := a.pal.level[its[0].meta.Level]
	c0 := c1
	if src.c != (ui.Color{}) {
		c0 = src.c
	}
	b := ball{d: d, endS: 2 * r / d, c0: c0, c1: c1, keys: keys}
	morphT := time.Duration(0)
	s := &a.sun
	switch {
	case src.arc != nil:
		h := src.arc
		am := float64(a0Top + (h.a0+h.a1)/2)
		rm := (h.r0 + h.r1) / 2
		if h.a1-h.a0 > math.Pi {
			rm = h.r1 * .8
		}
		b.sx, b.sy = s.cx+float32(math.Cos(am))*rm, s.cy+float32(math.Sin(am))*rm
		pad := min(.8/h.r0, (h.a1-h.a0)*.2)
		path := sector(&ui.Path{}, s.cx, s.cy, h.r0, h.r1, h.a0+pad, min(h.a1-pad, h.a0+tau-.002), a.pal.corner)
		a.fly.shards = append(a.fly.shards, shard{path, c0, b.sx, b.sy, now.Add(delay)})
		morphT = 150 * time.Millisecond
	case src.rect.W > 0:
		b.sx, b.sy = src.rect.X+src.rect.W/2, src.rect.Y+src.rect.H/2
		a.fly.morphs = append(a.fly.morphs, morph{src.rect, b.sx, b.sy, d, c0, now.Add(delay)})
		morphT = 150 * time.Millisecond
	default:
		b.sx, b.sy, b.pop = src.x, src.y, !src.ball
	}
	b.tx, b.ty = ox+T.w/2, oy+T.b-10
	if ok {
		b.tx, b.ty = ox+pos.x, oy+pos.y
	}
	b.hy = oy + T.m - 28
	b.mx = (b.sx+b.tx)/2 + (rand.Float32()-.5)*90
	b.my = min(b.sy, b.hy) - 50 - rand.Float32()*80
	dist := math.Hypot(float64(b.tx-b.sx), float64(b.ty-b.sy))
	b.dur = ms((440 + dist*.13) * (1 + vary))
	b.start = now.Add(delay + morphT)
	for _, k := range keys {
		f.inflight[k]++
	}
	a.fly.balls = append(a.fly.balls, b)
}

// paintFly draws shards, row morphs and balls, then the bin's front over them.
func (a *app) paintFly(p *ui.Painter) {
	f := &a.fly
	now := a.now
	// shards: scale 1 → 1.05 (30%) → .08, fading to .2, 210ms
	keepS := f.shards[:0]
	for _, s := range f.shards {
		k := float64(now.Sub(s.t0)) / float64(210*time.Millisecond)
		if k >= 1 {
			continue
		}
		keepS = append(keepS, s)
		sc, op := float32(1), float32(1)
		if k > 0 {
			e := float32(easeIn(k))
			if e < .3 {
				sc = 1 + .05*e/.3
			} else {
				u := (e - .3) / .7
				sc, op = 1.05+(.08-1.05)*u, 1-.8*u
			}
		}
		p.Transform(ui.Scaling(sc, sc), s.ox, s.oy, func() { p.FillPath(s.path, s.c.Alpha(op)) })
	}
	f.shards = keepS
	// list rows fold into the ball, 170ms
	keepM := f.morphs[:0]
	for _, m := range f.morphs {
		k := float64(now.Sub(m.t0)) / float64(170*time.Millisecond)
		if k >= 1 {
			continue
		}
		keepM = append(keepM, m)
		e := float32(bez(.4, 0, .2, 1, max(0, k)))
		x := m.r.X + (m.x-m.d/2-m.r.X)*e
		y := m.r.Y + (m.y-m.d/2-m.r.Y)*e
		w := m.r.W + (m.d-m.r.W)*e
		h := m.r.H + (m.d-m.r.H)*e
		p.Fill(ui.Rect{X: x, Y: y, W: w, H: h}, m.c.Alpha(.35+.65*e), 6+(m.d/2-6)*e)
	}
	f.morphs = keepM
	// balls
	bs := &a.binS
	keepB := f.balls[:0]
	for _, b := range f.balls {
		t := float32(now.Sub(b.start)) / float32(b.dur)
		if t >= 1 {
			for _, k := range b.keys {
				if bs.inflight[k]--; bs.inflight[k] <= 0 {
					delete(bs.inflight, k)
				}
				bs.landed[k] = now
			}
			bs.lastLand, bs.bumpT0 = now, now
			continue
		}
		keepB = append(keepB, b)
		if t < 0 {
			if !b.pop {
				a.paintBall(p, b.sx, b.sy, b.d/2, b.c0, 1)
			}
			continue
		}
		f0, f1 := float32(0), float32(.74)
		if b.pop {
			f0 = .08
		}
		var x, y, sc, op float32 = b.sx, b.sy, 1, 1
		c := b.c0
		switch {
		case b.pop && t < f0:
			u := t / f0
			y, sc, op = b.sy-4*u, .2+.9*u, u
		case t < f1:
			u := (t - f0) / (f1 - f0)
			x, y, sc = quad(b.sx, b.mx, b.tx, u), quad(b.sy, b.my, b.hy, u), 1-.15*u
		default:
			u := (t - f1) / (1 - f1)
			x, y = b.tx, b.hy+(b.ty-b.hy)*u*u
			sc = .85 + (b.endS-.85)*u
			c = b.c0.Mix(b.c1, u)
		}
		if b.solo && t > f0 && t < f1 { // a short fading trail along the arc
			for i := 1; i <= 4; i++ {
				u := (t - f0 - float32(i)*.025) / (f1 - f0)
				if u <= 0 {
					break
				}
				a.paintBall(p, quad(b.sx, b.mx, b.tx, u), quad(b.sy, b.my, b.hy, u), b.d/2*(1-.15*u)*(1-.12*float32(i)), b.c0, .32-.07*float32(i))
			}
		}
		a.paintBall(p, x, y, b.d/2*sc, c, op)
	}
	f.balls = keepB
	a.paintBinFront(p)
	a.paintBallTip(p)
	// "已释放 X" rises off the bin after an empty
	if !bs.freedT.IsZero() {
		k := float64(now.Sub(bs.freedT)) / float64(1300*time.Millisecond)
		if k >= 1 {
			bs.freedT = time.Time{}
		} else {
			e := bez(0, 0, .58, 1, k)
			var dy, op float64
			if e < .25 {
				dy, op = -40*e/.25, e/.25
			} else {
				u := (e - .25) / .75
				dy, op = -40-60*u, 1-u
			}
			ox, oy := bs.origin()
			T := &binTiers[bs.tier]
			sp := ui.Span{Text: bs.freed, Size: 13, Weight: 600, Color: a.pal.level[0].Alpha(float32(op))}
			w, _ := p.MeasureText(0, sp)
			hx, hy := bs.toHero(ox+T.w/2, oy)
			p.RichText(hx-w/2, hy+float32(dy), 0, sp)
			a.frame = true
		}
	}
	if f.active() || len(bs.inflight) > 0 {
		a.frame = true
	}
	if a.frame {
		p.AnimationFrame()
	}
}

func quad(a, c, b, t float32) float32 { u := 1 - t; return u*u*a + 2*u*t*c + t*t*b }

// emptyBin tips the pile out, then forgets it.
func (a *app) startDump(size int64) {
	f := &a.binS
	if !a.motion() {
		return
	}
	f.dump = f.dump[:0]
	for _, e := range f.ents {
		if b, ok := f.pile[e.key]; ok {
			f.dump = append(f.dump, dumpBall{b, 18 + rand.Float32()*14, 0})
		}
	}
	n := len(f.dump)
	for i := range f.dump {
		f.dump[i].delay = ms(float64(n-i)*3 + rand.Float64()*90)
	}
	f.dumpT0 = a.now
	f.freed, f.freedT = "已释放 "+fmtSize(size), a.now
}

var _ = scan.None
var _ trash.Source
