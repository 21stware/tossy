package main

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// The vehicles of truck.go are drawn with what a GPU draws natively and mygo never needs a
// coverage mask for (see pieces.go): rounded rectangles, plain, with corners of their own or in
// gradients; circles (rectangles rounded all the way); glows (box shadows); lines of one
// segment, or opaque ones of a few; and text. Slants are skews of rectangles, the bodies' lean
// and the craft's pitch transforms the GPU applies. truck_test.go keeps paths out of here.

// tk is a vehicle's drawing kit: its own units (x from the front, y up from its base, negative)
// to the window.
type tk struct {
	p      *ui.Painter
	x0, y0 float32 // the front, at the base
	s      float32 // window px per unit
}

func (k *tk) X(x float32) float32 { return k.x0 + x*k.s }
func (k *tk) Y(y float32) float32 { return k.y0 + y*k.s }
func (k *tk) R(x0, y0, x1, y1 float32) ui.Rect {
	return ui.Rect{X: k.X(x0), Y: k.Y(y0), W: (x1 - x0) * k.s, H: (y1 - y0) * k.s}
}

// radii scales corner radii (clockwise from the top left) to the window.
func (k *tk) radii(r [4]float32) [4]float32 {
	for i := range r {
		r[i] *= k.s
	}
	return r
}

// box fills a rounded rectangle; grad, a gradient across it.
func (k *tk) box(x0, y0, x1, y1, rad float32, c ui.Color) { k.p.Fill(k.R(x0, y0, x1, y1), c, rad*k.s) }
func (k *tk) grad(x0, y0, x1, y1, rad float32, g ui.LinearGradient) {
	k.p.FillGradient(k.R(x0, y0, x1, y1), g, rad*k.s)
}

// skewed runs fn slanted by deg about (x, y) in units: a rectangle drawn in it is a raked
// windscreen or a sloped nose.
func (k *tk) skewed(deg, x, y float32, fn func()) {
	k.p.Transform(ui.Skewing(deg, 0), k.X(x), k.Y(y), fn)
}

// lit fills a strip and haloes it, as a lamp or a light bar.
func (k *tk) lit(x0, y0, x1, y1 float32, c ui.Color, blur float32) {
	r := k.R(x0, y0, x1, y1)
	rad := min(r.W, r.H) / 2
	k.p.Shadow(r, rad, 0, 0, blur, 0, c.Alpha(.7))
	k.p.Fill(r, c, rad)
}

func grow(r ui.Rect, d float32) ui.Rect {
	return ui.Rect{X: r.X - d, Y: r.Y - d, W: r.W + 2*d, H: r.H + 2*d}
}

// opaque is c at full alpha: a line of several segments draws without a mask only opaque.
func opaque(c ui.Color) ui.Color { c.A = 255; return c }

// dotAt fills a circle in window px.
func dotAt(p *ui.Painter, x, y, r float32, c ui.Color) {
	if r <= 0 {
		return
	}
	p.Fill(ui.Rect{X: x - r, Y: y - r, W: 2 * r, H: 2 * r}, c, r)
}

// ballGPU draws an item's ball as paintBall does, from circles alone: its shaded rim, its face
// (a little up and left) and a highlight. It is drawn about the origin and moved to (x, y) by
// a transform: mygo rounds each rectangle to whole pixels, and three circles placed in the
// window would round apart, frame by frame, as the ball flies.
func ballGPU(p *ui.Painter, x, y, r float32, c ui.Color) {
	if r <= .2 {
		return
	}
	p.Transform(ui.Translation(x, y), 0, 0, func() {
		dotAt(p, 0, 0, r, c.Mix(ui.RGB(0, 0, 0), .22))
		o := min(1.5, r*.13)
		dotAt(p, -o, -o, r-o*1.15, c)
		dotAt(p, -r*.35, -r*.35, r*.32, ui.RGBA(255, 255, 255, .22))
	})
}

// tossyMark writes TOSSY in the vehicles' own wordmark: wide, thin, geometric capitals, the T's
// stem standing clear of its bar, an O that is a stadium, square-cut S's and a Y of two
// strokes meeting on its stem. h is the cap height; it returns the width.
func tossyMark(p *ui.Painter, x, y, h float32, c ui.Color) float32 {
	w := max(1, h*.12) // stroke
	W := h * 1.3       // a letter
	gap := h * .7
	for i, ch := range "TOSSY" {
		lx := x + float32(i)*(W+gap)
		switch ch {
		case 'T':
			p.Fill(ui.Rect{X: lx, Y: y, W: W, H: w}, c, w/2)
			p.Fill(ui.Rect{X: lx + W/2 - w/2, Y: y + w*2.2, W: w, H: h - w*2.2}, c, w/2)
		case 'O':
			p.Stroke(ui.Rect{X: lx, Y: y, W: W, H: h}, c, h/2, w)
		case 'S':
			for _, r := range []ui.Rect{{X: lx, Y: y, W: W, H: w}, {X: lx, Y: y, W: w, H: h/2 + w/2},
				{X: lx, Y: y + h/2 - w/2, W: W, H: w}, {X: lx + W - w, Y: y + h/2 - w/2, W: w, H: h/2 + w/2}, {X: lx, Y: y + h - w, W: W, H: w}} {
				p.Fill(r, c, w/2)
			}
		case 'Y':
			cx, cy := lx+W/2, y+h*.5
			strokeLine(p, lx+w/2, y+w/2, cx, cy, w, c)
			strokeLine(p, lx+W-w/2, y+w/2, cx, cy, w, c)
			p.Fill(ui.Rect{X: cx - w/2, Y: cy, W: w, H: h - h*.5}, c, w/2)
		}
	}
	return 5*W + 4*gap
}

// brackets draws HUD corner brackets around r, arm long.
func brackets(p *ui.Painter, r ui.Rect, arm, w float32, c ui.Color) {
	for _, q := range [][4]float32{{0, 0, 1, 1}, {1, 0, -1, 1}, {0, 1, 1, -1}, {1, 1, -1, -1}} {
		cx, cy := r.X+q[0]*r.W, r.Y+q[1]*r.H
		strokeLine(p, cx, cy, cx+q[2]*arm, cy, w, c)
		strokeLine(p, cx, cy, cx, cy+q[3]*arm, w, c)
	}
}

// ------------------------------------------------------------------------------------------
// the frame

// paintTruck draws the vehicle, what it kicks up, the items going into it and its readout
// (window space, under the bin's front).
func (a *app) paintTruck(p *ui.Painter) {
	f := &a.binS
	a.stepTruck()
	r := f.truck
	if r == nil {
		return
	}
	now := a.now
	dt := float32(max(1.0/240, min(now.Sub(r.paintT).Seconds(), 1.0/20)))
	first := r.paintT.IsZero()
	r.paintT = now
	g := a.truckGeo()
	front, base := a.truckPose(r, g)
	if !first {
		r.wheel += (front - r.frontX) / (17 * g.sc)
		v := (front - r.frontX) / dt
		r.speed += (v - r.speed) * min(1, dt*12)
	}
	r.frontX = front
	// mygo rounds each rectangle to whole pixels: placed where the vehicle is, every part would
	// round on its own as it glides, and the parts would jitter against one another. They are
	// placed from a whole pixel instead, where each rounds the same way every frame, and the
	// fraction left over moves the whole vehicle at once.
	ps := p.Scale()
	ix, iy := float32(math.Floor(float64(front*ps)))/ps, float32(math.Floor(float64(base*ps)))/ps
	k := &tk{p: p, x0: ix, y0: iy, s: g.sc}
	var mx, my float32 // where the items go in
	p.Transform(ui.Translation(front-ix, base-iy), 0, 0, func() {
		a.truckWake(k, r, g)
		switch r.kind {
		case vehVan:
			mx, my = a.paintVan(k, r, g)
		case vehEV:
			mx, my = a.paintEV(k, r, g)
		default:
			mx, my = a.paintSkiff(k, r, g)
		}
	})
	mx, my = mx+front-ix, my+base-iy
	a.truckHops(p, r, mx, my, k)
	a.truckReadout(p, r, k, g, dt)
}

// truckWake draws what the vehicle leaves behind it: a soft shadow under a wheeled one, dust
// as it pulls away, and speed lines.
func (a *app) truckWake(k *tk, r *truckRun, g truckGeo) {
	p, pal, now := k.p, a.pal, a.now
	L := vehLen[r.kind]
	if r.kind != vehSkiff {
		w := L * g.sc * .5
		cx := k.X(L / 2)
		sh := float32(.55)
		if !pal.dark {
			sh = .2
		}
		p.Transform(ui.Scaling(1, 8/w), cx, g.floor+.5, func() {
			p.FillRadialGradient(ui.Rect{X: cx - w, Y: g.floor + .5 - w, W: 2 * w, H: 2 * w}, ui.RadialGradient{
				Radius: w, Stops: []ui.GradientStop{{Color: ui.RGBA(0, 0, 0, sh), At: 0}, {Color: ui.RGBA(0, 0, 0, 0), At: 1}}}, w)
		})
		if r.speed < -120 && now.Sub(r.puffT) >= 70*time.Millisecond {
			r.puffT = now
			for _, wx := range []float32{48, L - 46} {
				r.puffs = append(r.puffs, truckPuff{x: k.X(wx + 16), y: g.floor - 4*g.sc, t0: now})
			}
		}
	}
	keep := r.puffs[:0]
	for _, u := range r.puffs {
		t := float64(now.Sub(u.t0)) / float64(800*time.Millisecond)
		if t >= 1 {
			continue
		}
		keep = append(keep, u)
		e := float32(easeOut(t))
		dotAt(p, u.x+e*34*g.sc, u.y-e*10*g.sc, (2+6*e)*g.sc*.8, pal.t3.Alpha(.3*(1-float32(t))))
	}
	r.puffs = keep
	if sp := float32(math.Abs(float64(r.speed))); sp > 260 {
		al := min(1, (sp-260)/500)
		for i, y := range []float32{-.8, -.55, -.3} {
			l := (30 + sp*.06 + float32(i%2)*16) * g.sc * .6
			x0 := k.X(L) + 12*g.sc
			if r.speed > 0 { // coming in backwards: they trail off its front
				x0 = k.X(0) - 12*g.sc - l
			}
			yy := k.Y(y * 110)
			strokeLine(p, x0, yy, x0+l, yy, 1.6, pal.text.Alpha(.3*al))
		}
	}
}

// truckHops draws the items going in: up the van's arm and in at its roof, in an arc into the
// hauler's pod, or straight up the craft's beam.
func (a *app) truckHops(p *ui.Painter, r *truckRun, mx, my float32, k *tk) {
	now := a.now
	keep := r.hops[:0]
	for _, b := range r.hops {
		t := float64(now.Sub(b.t0)) / float64(truckHop)
		if t >= 1 {
			r.rockT0 = now
			r.hauledB += b.size
			continue
		}
		keep = append(keep, b)
		u := float32(ease(t))
		rr := b.r
		if t > .82 {
			rr *= float32(1 - (t-.82)/.18*.75)
		}
		var x, y float32
		switch r.kind {
		case vehVan: // up to the gripper, then along the arm into the roof
			gx, gy := a.vanGrip(r, k, a.truckGeo())
			if t < .45 {
				v := float32(ease(t / .45))
				x, y = quad(b.sx, (b.sx+gx)/2, gx, v), quad(b.sy, min(b.sy, gy)-24*k.s, gy, v)
			} else {
				v := float32(ease((t - .45) / .55))
				x, y = gx+(mx-gx)*v, quad(gy, min(gy, my)-30*k.s, my, v)
			}
		case vehEV:
			x, y = quad(b.sx, (b.sx+mx)/2, mx, u), quad(b.sy, min(b.sy, my)-70*k.s, my, u)
		default: // the beam takes it up, drawing it to its middle
			x, y = b.sx+(mx-b.sx)*float32(easeOut(t)), b.sy+(my-b.sy)*u
			h := rr * 1.7
			p.Shadow(ui.Rect{X: x - rr, Y: y - rr, W: 2 * rr, H: 2 * rr}, rr, 0, 0, h, 0, a.pal.accent.Alpha(.6))
		}
		ballGPU(p, x, y, rr, b.c)
	}
	r.hops = keep
}

// truckReadout shows what has gone, in the HUD's voice: the van's amber target lock on the
// bin, a cyan readout beside the hauler and the craft. The count rolls up with what the eye
// sees go: the items gone in.
func (a *app) truckReadout(p *ui.Painter, r *truckRun, k *tk, g truckGeo, dt float32) {
	pal, now := a.pal, a.now
	t0 := r.t0.Add(truckArrive - 150*time.Millisecond)
	if now.Before(t0) {
		return
	}
	want := float64(r.hauledB)
	if r.done() {
		r.mu.Lock()
		want = float64(min(r.freed, r.totalB))
		r.mu.Unlock()
		if r.doneT0.IsZero() {
			r.doneT0 = now
		}
	}
	r.shownB += (want - r.shownB) * float64(min(1, dt*7))
	if math.Abs(want-r.shownB) < 1<<10 {
		r.shownB = want
	}
	pct := int(math.Round(100 * r.shownB / float64(max(1, r.totalB))))
	status := fmt.Sprintf("%s · %d%%", T("正在运走"), pct)
	if r.done() {
		status = T("搞定！")
	}
	size := fmtSize(int64(r.shownB))
	in := float32(easeStd(a.progress(t0, 0, 320*time.Millisecond)))
	if r.kind == vehVan {
		// the lock: amber brackets close on the bin, a tag over it
		lock := pal.amber
		bin := ui.Rect{X: g.binL - 12, Y: g.binTop - 16, W: g.binR - g.binL + 24, H: g.floor - g.binTop + 20}
		bin = grow(bin, 26*(1-in))
		brackets(p, bin, 16, 2, lock.Alpha(in))
		cx, cy := bin.X+bin.W/2, bin.Y+bin.H/2
		for _, d := range [][4]float32{{-9, 0, -3, 0}, {3, 0, 9, 0}, {0, -9, 0, -3}, {0, 3, 0, 9}} {
			strokeLine(p, cx+d[0], cy+d[1], cx+d[2], cy+d[3], 1.4, lock.Alpha(.8*in))
		}
		s := ui.Span{Text: status + "  " + size, Size: 11, Weight: 700, Font: pal.mono, Color: ui.Hex("#141414")}
		w, h := p.MeasureText(0, s)
		tag := ui.Rect{X: bin.X, Y: bin.Y - h - 10, W: w + 14, H: h + 5}
		p.Fill(tag, lock.Alpha(in), 2)
		p.RichText(tag.X+7, tag.Y+2.5, 0, s)
		return
	}
	c := pal.accent
	if r.done() {
		c = pal.level[0]
	}
	s1 := ui.Span{Text: status, Size: 11, Font: pal.mono, Color: c.Alpha(.8 * in)}
	s2 := ui.Span{Text: size, Size: 24, Weight: 700, Font: pal.mono, Color: c.Alpha(in)}
	w1, h1 := p.MeasureText(0, s1)
	w2, h2 := p.MeasureText(0, s2)
	bw, bh := max(w1, w2)+28, h1+h2+20
	var x, y float32
	if r.kind == vehEV {
		x, y = k.X(116), k.Y(-100)-bh-22*k.s
	} else {
		x, y = k.X(0)-bw-22*k.s, k.Y(-40)-bh/2 // ahead of it: the bin stands at the window's right
	}
	box := ui.Rect{X: x, Y: y, W: bw, H: bh}
	brackets(p, grow(box, 10*(1-in)), 11, 1.6, c.Alpha(in))
	p.RichText(x+14, y+10, 0, s1)
	p.RichText(x+14, y+10+h1, 0, s2)
}

// ------------------------------------------------------------------------------------------
// the cleaner: a matte-black van with an arm off its roof

// vanGrip is where the arm's gripper is: folded on the roof while it drives, out over the bin
// while it works.
func (a *app) vanGrip(r *truckRun, k *tk, g truckGeo) (float32, float32) {
	out := float32(easeStd(a.progress(r.t0.Add(truckArrive), 0, 400*time.Millisecond)))
	if r.done() {
		out *= 1 - float32(easeStd(a.progress(r.leaveT0.Add(-420*time.Millisecond), 0, 400*time.Millisecond)))
	}
	fx, fy := k.X(122), k.Y(-122)
	bob := float32(math.Sin(float64(a.now.UnixMilli())*.008)) * 3 * k.s * out
	return fx + (g.binCX-fx)*out, fy + (g.binTop-56*k.s-fy)*out + bob
}

func (a *app) paintVan(k *tk, r *truckRun, g truckGeo) (float32, float32) {
	p, pal := k.p, a.pal
	lean, squash := a.truckLean(r)
	skew := -float32(math.Atan(float64(lean)/100) * 180 / math.Pi)
	body := ui.Scaling(1, squash).Then(ui.Skewing(skew, 0))
	black := ui.LinearGradient{Angle: 180, From: ui.Hex("#2c3137"), To: ui.Hex("#0b0c0e")}
	p.Transform(body, k.X(118), k.Y(-17), func() {
		p.FillRadii(k.R(0, -112, 236, -22), k.radii([4]float32{34, 6, 4, 10}), ui.Hex("#22262b"))
		k.grad(1, -111, 235, -23, 6, black)
		p.FillRadii(k.R(0, -64, 44, -22), k.radii([4]float32{16, 0, 0, 10}), ui.Hex("#1a1d21"))
		k.skewed(-32, 14, -66, func() {
			k.grad(16, -106, 60, -66, 4, ui.LinearGradient{Angle: 180, From: ui.Hex("#20262c"), To: ui.Hex("#050607")})
			k.box(26, -100, 52, -97, 1.5, ui.RGBA(255, 255, 255, .2))
		})
		k.grad(66, -104, 128, -72, 4, ui.LinearGradient{Angle: 180, From: ui.Hex("#151a1f"), To: ui.Hex("#050607")})
		hLine(p, k.X(70), k.X(124), k.Y(-99), 1.2, ui.RGBA(255, 255, 255, .14))
		vLine(p, k.X(132), k.Y(-104), k.Y(-26), 1, ui.RGBA(255, 255, 255, .07))
		hLine(p, k.X(6), k.X(230), k.Y(-58), 1, ui.RGBA(255, 255, 255, .06))
		p.RichText(k.X(150), k.Y(-52), 0, ui.Span{Text: "NO TRACE", Size: 9 * k.s, Font: pal.mono, Color: ui.RGBA(255, 255, 255, .22)})
		k.box(176, -116, 222, -112, 2, ui.Hex("#15181b")) // the roof hatch
		k.lit(0, -48, 10, -45.5, ui.RGB(235, 245, 255), 8)
		k.lit(232.5, -100, 236, -70, ui.RGB(255, 40, 60), 8)
	})
	for _, wx := range []float32{44, 192} {
		x, y, rr := k.X(wx), k.Y(-17), 17*k.s
		dotAt(p, x, y, rr, ui.Hex("#08090a"))
		dotAt(p, x, y, rr*.6, ui.Hex("#1c1f23"))
		p.Stroke(grow(ui.Rect{X: x, Y: y}, rr*.6), ui.Hex("#2f343a"), rr*.6, 1.2)
		for i := 0; i < 5; i++ {
			ang := float64(r.wheel) + float64(i)*tau/5
			ca, sa := float32(math.Cos(ang)), float32(math.Sin(ang))
			strokeLine(p, x+ca*rr*.2, y+sa*rr*.2, x+ca*rr*.5, y+sa*rr*.5, max(1, rr*.08), ui.Hex("#3a4047"))
		}
		dotAt(p, x, y, rr*.14, ui.Hex("#08090a"))
	}
	// the arm: two links off the roof, red-lit joints, a gripper
	bx, by := k.X(200), k.Y(-114)
	gx, gy := a.vanGrip(r, k, g)
	ex, ey := (bx+gx)/2, min(by, gy)-42*k.s
	for _, s := range [][4]float32{{bx, by, ex, ey}, {ex, ey, gx, gy - 12*k.s}} {
		strokeLine(p, s[0], s[1], s[2], s[3], 9*k.s, ui.Hex("#2a2f35"))
		strokeLine(p, s[0], s[1], s[2], s[3], 3*k.s, ui.Hex("#3d444c"))
	}
	for _, j := range [][2]float32{{bx, by}, {ex, ey}} {
		dotAt(p, j[0], j[1], 7*k.s, ui.Hex("#1b1f23"))
		p.Shadow(ui.Rect{X: j[0] - 2*k.s, Y: j[1] - 2*k.s, W: 4 * k.s, H: 4 * k.s}, 2*k.s, 0, 0, 8, 0, ui.RGBA(255, 60, 70, .8))
		dotAt(p, j[0], j[1], 2*k.s, ui.RGB(255, 60, 70))
	}
	jaw := 10 * k.s
	strokeLine(p, gx-jaw, gy-12*k.s, gx+jaw, gy-12*k.s, 4*k.s, ui.Hex("#2a2f35"))
	strokeLine(p, gx-jaw, gy-12*k.s, gx-jaw-2*k.s, gy+5*k.s, 3*k.s, ui.Hex("#2a2f35"))
	strokeLine(p, gx+jaw, gy-12*k.s, gx+jaw+2*k.s, gy+5*k.s, 3*k.s, ui.Hex("#2a2f35"))
	return k.X(199), k.Y(-114)
}

// ------------------------------------------------------------------------------------------
// the hauler: a low electric van in pearl, a glass roof, aero wheels, a light bar that fills

func (a *app) paintEV(k *tk, r *truckRun, g truckGeo) (float32, float32) {
	p, pal := k.p, a.pal
	acc := pal.accent
	lean, squash := a.truckLean(r)
	skew := -float32(math.Atan(float64(lean)/100) * 180 / math.Pi)
	body := ui.Scaling(1, squash).Then(ui.Skewing(skew, 0))
	if r.rockT0 != (time.Time{}) {
		if t := a.progress(r.rockT0, 0, 220*time.Millisecond); t < 1 {
			body = body.Then(ui.Translation(0, float32(math.Sin(t*math.Pi))*1.4))
		}
	}
	pearl := ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{
		{Color: ui.Hex("#fbfcfd"), At: 0}, {Color: ui.Hex("#e3e6ea"), At: .55}, {Color: ui.Hex("#b9bfc7"), At: 1}}}
	glass := ui.LinearGradient{Angle: 180, From: ui.Hex("#323b45"), To: ui.Hex("#06080a")}
	prog := float32(r.shownB / float64(max(1, r.totalB)))
	p.Transform(body, k.X(122), k.Y(-17), func() {
		// the pod, its roof glass running into the cabin's
		p.FillRadii(k.R(110, -98, 240, -40), k.radii([4]float32{10, 26, 4, 4}), ui.Hex("#c9ced5"))
		p.FillRadii(k.R(111, -97, 239, -41), k.radii([4]float32{9, 25, 4, 4}), ui.Hex("#eef0f3"))
		k.grad(112, -96, 238, -42, 8, pearl)
		k.grad(112, -98, 204, -90, 4, glass) // the glass roof
		k.skewed(-50, 42, -46, func() { k.grad(42, -78, 124, -46, 5, glass) })
		k.skewed(-50, 42, -46, func() { k.box(56, -74, 108, -72, 1, ui.RGBA(255, 255, 255, .28)) })
		// the slot the items go in at, lit while it loads
		open := float32(1)
		if r.done() {
			open = 1 - float32(a.progress(r.leaveT0.Add(-300*time.Millisecond), 0, 250*time.Millisecond))
		}
		k.box(206, -99, 234, -95, 2, ui.Hex("#06080a"))
		if open > 0 {
			k.lit(208, -98.5, 232, -97, acc.Alpha(open), 10)
		}
		tossyMark(p, k.X(154), k.Y(-66), 8*k.s, ui.Hex("#2b3036")) // behind the glass's rake
		// the body: a long low shell, its nose sloped
		p.FillRadii(k.R(0, -48, 244, -14), k.radii([4]float32{22, 8, 12, 20}), ui.Hex("#c9ced5"))
		k.grad(1, -47, 243, -15, 12, pearl)
		k.skewed(-62, 0, -34, func() { k.grad(0, -52, 40, -34, 6, pearl) })
		hLine(p, k.X(14), k.X(238), k.Y(-41), 1.2, ui.RGBA(255, 255, 255, .8)) // the shoulder catches the light
		k.box(4, -24, 240, -14, 5, ui.Hex("#14171b"))                          // the dark sill
		hLine(p, k.X(72), k.X(88), k.Y(-36), 1.4, ui.Hex("#8e959e"))           // a flush handle
		// the light bar along the sill: dim, lit from the back as far as the items have gone
		bar := k.R(16, -20.5, 230, -18)
		p.Fill(bar, acc.Alpha(.18), bar.H/2)
		if prog > 0 {
			bar.X, bar.W = bar.X+bar.W*(1-prog), bar.W*prog
			p.Shadow(bar, bar.H/2, 0, 0, 10, 0, acc.Alpha(.7))
			p.Fill(bar, acc, bar.H/2)
		}
		k.lit(1, -40, 26, -38, ui.RGB(240, 250, 255), 10) // the light blade
		k.lit(238, -62, 243, -52, ui.RGB(255, 50, 70), 8)
	})
	// aero wheels: a flat cover in pearl grey, five shallow grooves, turning
	for _, wx := range []float32{46, 198} {
		x, y, rr := k.X(wx), k.Y(-17), 18*k.s
		dotAt(p, x, y, rr, ui.Hex("#0b0d0f"))
		dotAt(p, x, y, rr*.74, ui.Hex("#9aa1aa"))
		dotAt(p, x, y, rr*.7, ui.Hex("#d2d6db"))
		for i := 0; i < 5; i++ {
			ang := float64(r.wheel) + float64(i)*tau/5
			ca, sa := float32(math.Cos(ang)), float32(math.Sin(ang))
			strokeLine(p, x+ca*rr*.22, y+sa*rr*.22, x+ca*rr*.62, y+sa*rr*.62, max(1, rr*.07), ui.Hex("#a6adb5"))
		}
		dotAt(p, x, y, rr*.16, ui.Hex("#5a626b"))
	}
	return k.X(220), k.Y(-97)
}

// ------------------------------------------------------------------------------------------
// the skiff: a hover craft with a tractor beam

func (a *app) paintSkiff(k *tk, r *truckRun, g truckGeo) (float32, float32) {
	p, pal, now := k.p, a.pal, a.now
	acc := pal.accent
	el := now.Sub(r.t0)
	hx, hy := k.X(178), k.Y(5) // the hatch
	// the beam: on once it is over the bin, off as it goes
	beam := float32(0)
	if el > truckArrive {
		beam = float32(a.progress(r.t0.Add(truckArrive), 0, 250*time.Millisecond))
		if r.done() {
			beam *= 1 - float32(a.progress(r.leaveT0, 0, 200*time.Millisecond))
		}
	}
	// the glow on the floor under it, fading with its height
	alt := max(0, g.floor-k.y0)
	gl := max(0, 1-alt/(420*k.s)) * (.4 + .6*beam)
	p.Transform(ui.Scaling(1, .16), hx, g.floor, func() {
		w := 170 * k.s
		p.FillRadialGradient(ui.Rect{X: hx - w, Y: g.floor - w, W: 2 * w, H: 2 * w}, ui.RadialGradient{Radius: w,
			Stops: []ui.GradientStop{{Color: acc.Alpha(.4 * gl), At: 0}, {Color: acc.Alpha(0), At: 1}}}, w)
	})
	if beam > 0 {
		bot, wBot := g.binTop+4*k.s, (g.binR-g.binL)*1.1
		wTop := 50 * k.s
		const n = 32
		span := bot - hy
		for i := 0; i < n; i++ {
			u := float32(i) / n
			w := wTop + (wBot-wTop)*u
			p.Fill(ui.Rect{X: hx - w/2, Y: hy + span*u, W: w, H: span/n + .5}, acc.Alpha((.04+.07*u)*beam), 0)
		}
		for _, sg := range []float32{-1, 1} {
			strokeLine(p, hx+sg*wTop/2, hy, hx+sg*wBot/2, bot, 1.4, acc.Alpha(.55*beam))
		}
		// rings climbing it
		ph := float32(math.Mod(float64(now.UnixMilli())/900, 1))
		for i := 0; i < 4; i++ {
			u := 1 - float32(math.Mod(float64(ph)+float64(i)/4, 1))
			w := wTop + (wBot-wTop)*u
			y := hy + span*u
			p.Stroke(ui.Rect{X: hx - w/2, Y: y - 4, W: w, H: 8}, acc.Alpha(.3*beam*min(1, u*4)), 4, 1.2)
		}
	}
	// the craft: bobbing, pitched down as it comes in and up as it climbs away
	pitch := float32(0)
	if el < truckArrive {
		pitch = -4 * (1 - float32(easeStd(float64(el)/float64(truckArrive))))
	}
	if r.done() && now.After(r.leaveT0) {
		pitch = 7 * float32(easeStd(min(1, float64(now.Sub(r.leaveT0))/float64(400*time.Millisecond))))
	}
	bob := float32(math.Sin(float64(now.UnixMilli())*.0024)) * 2.5 * k.s
	t := ui.Rotation(pitch).Then(ui.Translation(0, bob))
	for _, px := range []float32{34, 150} { // thruster wash
		cx := k.X(px)
		p.FillGradient(ui.Rect{X: cx - 15*k.s, Y: k.Y(6) + bob, W: 30 * k.s, H: 40 * k.s}, ui.LinearGradient{Angle: 180, From: acc.Alpha(.5), To: acc.Alpha(0)}, 10*k.s)
	}
	metal := ui.LinearGradient{Angle: 180, From: ui.Hex("#4a5561"), To: ui.Hex("#161a1f")}
	p.Transform(t, k.X(112), k.Y(-18), func() {
		k.skewed(-38, 200, -34, func() { k.grad(186, -70, 212, -34, 3, metal) }) // the tail fin
		k.grad(44, -48, 196, -30, 10, metal)
		p.FillRadii(k.R(0, -36, 224, 0), k.radii([4]float32{30, 14, 22, 26}), ui.Hex("#1d2329"))
		k.grad(2, -35, 222, -14, 14, ui.LinearGradient{Angle: 180, From: ui.Hex("#56636f"), To: ui.Hex("#232a31")})
		p.FillRadii(k.R(14, -52, 84, -30), k.radii([4]float32{24, 16, 2, 10}), acc.Alpha(.3))
		k.grad(16, -50, 82, -32, 12, ui.LinearGradient{Angle: 180, From: acc.Alpha(.55), To: acc.Alpha(.1)})
		k.skewed(-30, 40, -46, func() { k.box(34, -49, 60, -46, 1.5, ui.RGBA(255, 255, 255, .45)) })
		k.lit(10, -12, 214, -9.5, acc, 10)
		tossyMark(p, k.X(102), k.Y(-29), 6.5*k.s, ui.RGBA(255, 255, 255, .55))
		for _, px := range []float32{34, 150} {
			k.grad(px-28, -8, px+28, 6, 7, ui.LinearGradient{Angle: 180, From: ui.Hex("#3a444e"), To: ui.Hex("#0f1215")})
			k.lit(px-16, 4, px+16, 7, acc, 12)
		}
		k.lit(156, -1, 200, 5, acc.Mix(ui.RGB(255, 255, 255), .2+.4*beam), 10+10*beam) // the hatch
		dotAt(p, k.X(4), k.Y(-20), 2.5*k.s, ui.RGB(255, 255, 255))
		if math.Sin(float64(now.UnixMilli())*.012) > 0 {
			k.lit(218, -27, 222, -23, ui.RGB(255, 80, 90), 8)
		}
	})
	return hx, hy
}
