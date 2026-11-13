package main

import (
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/fanfree/fanfree/engine/scan"
)

// The scan readout: each new path rolls up from below as a whole, pushing the last one up and
// out, and each path the scanner reports leaves a dot where the beam was.

const (
	scanTick  = 140 * time.Millisecond // the path is sampled this often, not every frame
	scanSlide = 220 * time.Millisecond // a new path rolls in this long
	blipLife  = 1600 * time.Millisecond
	blipMax   = 28
)

type blip struct {
	ang float64
	r   float32 // 0..1 of the radar radius, by depth
	t0  time.Time
}

type scanText struct {
	full      string // the last sampled path
	line, old string // shown, and the one it replaced
	x, oldX   float32
	t0, last  time.Time
	blips     []blip
}

// sample takes a new path at most every scanTick; it returns true when the text changed.
func (s *scanText) sample(cur string, now time.Time, fit func([]string) []string) bool {
	if cur == "" || cur == s.full || now.Sub(s.last) < scanTick {
		return false
	}
	s.full, s.last = cur, now
	s.old, s.oldX = s.line, s.x
	s.line, s.t0 = strings.Join(fit(strings.Split(displayHome(cur), "/")), "/"), now
	return true
}

// paintScanPath writes the path at y, centred on cx and at most maxW wide. On a change the
// whole line rolls: the old one up and out, the new one up from below and in.
func (a *app) paintScanPath(p *ui.Painter, cx, y, maxW float32, cur string, ang float64) {
	pal := a.pal
	s := &a.scanTxt
	now := p.Now()
	const size = 10.5
	span := func(t string, c ui.Color) ui.Span { return ui.Span{Text: t, Size: size, Font: "monospace", Color: c} }
	width := func(t string) float32 { w, _ := p.MeasureText(0, span(t, pal.t4)); return w }
	fit := func(segs []string) []string { // drop leading folders until it fits
		for len(segs) > 2 && width(strings.Join(segs, "/")) > maxW {
			segs = append([]string{"…"}, segs[2:]...)
		}
		return segs
	}
	if s.sample(cur, now, fit) {
		depth := len(strings.Split(cur, "/"))
		s.blips = append(s.blips, blip{ang: ang, r: .22 + .72*min(1, float32(depth)/14), t0: now})
		if len(s.blips) > blipMax {
			s.blips = s.blips[len(s.blips)-blipMax:]
		}
	}
	if s.line == "" {
		return
	}
	k := float32(1)
	if a.pal.motion {
		k = float32(easeStd(min(1, float64(now.Sub(s.t0))/float64(scanSlide))))
	}
	s.x = cx - width(s.line)/2
	_, lh := p.MeasureText(0, span("/", pal.t4))
	top := y - lh/2
	box := ui.Rect{X: cx - maxW/2 - 4, Y: top - 1, W: maxW + 8, H: lh + 2}
	p.Clip(box, 0, func() {
		d := lh + 2
		if k < 1 && s.old != "" {
			p.RichText(s.oldX, top-d*k, 0, span(s.old, pal.t4.Alpha(1-k)))
		}
		p.RichText(s.x, top+d*(1-k), 0, span(s.line, pal.t2))
	})
	if k < 1 {
		p.AnimationFrame()
	}
}

// paintScanWire is the radar's wireframe: a slow tick ring, the crosshair, corner brackets,
// and a blip for each path the beam passed.
func (a *app) paintScanWire(p *ui.Painter, cx, cy, rad float32, ang float64, ink ui.Color) {
	now := p.Now()
	spin := float64(now.Sub(a.scanStart)) / float64(time.Second) * tau / 40
	// tick ring just outside the radar, turning the other way to the beam
	var tk, tl ui.Path
	for i := 0; i < 72; i++ {
		b := -spin + float64(i)*tau/72
		c, s := float32(math.Cos(b)), float32(math.Sin(b))
		r1 := rad + 6
		if i%6 == 0 {
			tl.MoveTo(cx+c*(r1), cy+s*(r1)).LineTo(cx+c*(r1+9), cy+s*(r1+9))
		} else {
			tk.MoveTo(cx+c*(r1), cy+s*(r1)).LineTo(cx+c*(r1+4), cy+s*(r1+4))
		}
	}
	p.StrokePath(&tk, 1, ink.Alpha(.14))
	p.StrokePath(&tl, 1, ink.Alpha(.3))
	// crosshair and spokes, dashed
	var ch ui.Path
	for i := 0; i < 12; i++ {
		b := float64(i) * tau / 12
		c, s := float32(math.Cos(b)), float32(math.Sin(b))
		for d := float32(14); d < rad; d += 9 {
			e := min(rad, d+4)
			ch.MoveTo(cx+c*d, cy+s*d).LineTo(cx+c*e, cy+s*e)
		}
	}
	p.StrokePath(&ch, 1, ink.Alpha(.06))
	// corner brackets on the radar's square
	var br ui.Path
	e, l := rad+22, float32(14)
	for _, q := range [4][2]float32{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
		x, y := cx+q[0]*e, cy+q[1]*e
		br.MoveTo(x-q[0]*l, y).LineTo(x, y).LineTo(x, y-q[1]*l)
	}
	p.StrokePath(&br, 1.2, ink.Alpha(.32))
	// the beam's edge
	var bm ui.Path
	bm.MoveTo(cx, cy).LineTo(cx+float32(math.Cos(ang))*rad, cy+float32(math.Sin(ang))*rad)
	p.StrokePath(&bm, 1, ink.Alpha(.5))
	// blips: a dot where the beam passed, fading in blipLife
	for _, b := range a.scanTxt.blips {
		age := now.Sub(b.t0)
		if age > blipLife {
			continue
		}
		f := 1 - float32(age)/float32(blipLife)
		r := b.r * rad
		x, y := cx+float32(math.Cos(b.ang))*r, cy+float32(math.Sin(b.ang))*r
		var d ui.Path
		d.Circle(x, y, 1.2+1.3*f)
		p.FillPath(&d, ink.Alpha(.75*f))
	}
}

// A rescan with a tree on screen plays on the sunburst itself: a faint beam turns over the
// rings, each arc the scanner is reading again lights up and dims, and the counters and the
// path run along the chart's foot.
type rescanFX struct {
	key  geoKey
	idx  map[string]int32 // full path -> node, for the arcs on screen
	lit  map[int32]time.Time
	last time.Time
}

const rescanGlow = 1400 * time.Millisecond

func (a *app) paintRescan(p *ui.Painter, r ui.Rect) {
	s, pal := &a.sun, a.pal
	fx := &a.rescan
	now := p.Now()
	if fx.idx == nil || fx.key != s.key {
		fx.key, fx.idx = s.key, map[string]int32{}
		for _, h := range s.arcs {
			if h.node != scan.None {
				fx.idx[a.t.Path(h.node)] = h.node
			}
		}
	}
	if fx.lit == nil {
		fx.lit = map[int32]time.Time{}
	}
	pr := a.prog
	cur := pr.Current()
	if cur != "" && now.Sub(fx.last) >= 90*time.Millisecond {
		fx.last = now
		for q := cur; ; q = filepath.Dir(q) { // the deepest arc on screen that holds it
			if n, ok := fx.idx[q]; ok {
				fx.lit[n] = now
				break
			}
			if d := filepath.Dir(q); d == q {
				break
			}
		}
	}
	ink := pal.amber
	if pal.instr == instrNone {
		ink = pal.text
	}
	ang := float64(now.Sub(a.scanStart)) / float64(time.Second) * tau / 2.4
	// the beam: a thin fan over the rings, under the lit arcs
	for i := 0; i < 16; i++ {
		b := ang - float64(i)*.03
		var w ui.Path
		w.MoveTo(s.cx, s.cy)
		w.LineTo(s.cx+float32(math.Cos(b))*s.rad, s.cy+float32(math.Sin(b))*s.rad)
		arcTo(&w, s.cx, s.cy, s.rad, b, b-.031)
		w.Close()
		p.FillPath(&w, ink.Alpha(.07*(1-float32(i)/16)))
	}
	var bm ui.Path
	bm.MoveTo(s.cx, s.cy).LineTo(s.cx+float32(math.Cos(ang))*(s.rad+10), s.cy+float32(math.Sin(ang))*(s.rad+10))
	p.StrokePath(&bm, 1, ink.Alpha(.45))
	for _, h := range s.arcs {
		t0, ok := fx.lit[h.node]
		if !ok || h.path == nil {
			continue
		}
		age := now.Sub(t0)
		if age > rescanGlow {
			delete(fx.lit, h.node)
			continue
		}
		f := 1 - float32(easeOut(float64(age)/float64(rescanGlow)))
		p.FillPath(h.path, ink.Alpha(.16*f))
		p.StrokePath(h.path, 1.5, ink.Alpha(.8*f))
	}
	// the readout along the foot
	y := r.Y + r.H - 22
	line := Tf("重新扫描 · %s · %s 个文件", fmtSize(pr.Bytes.Load()), fmtNum(pr.Files.Load()))
	centerText(p, r.X+r.W/2, y-20, line, 11, 500, pal.t2, "")
	a.paintScanPath(p, r.X+r.W/2, y, min(r.W-60, 560), cur, ang)
	if pal.motion {
		p.AnimationFrameRate(30)
	} else {
		p.AnimationFrame()
	}
}
