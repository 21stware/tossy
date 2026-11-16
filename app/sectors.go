package main

import (
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// The sunburst's sectors draw without coverage masks. mygo fills and strokes a path through a
// mask of its bounds in an atlas the frame shares, and the chart's thousands of sectors, laid
// out anew on every frame of a zoom, a morph or the intro, need more masks than the atlas holds:
// mygo 0.9.0 left the paths that did not fit out of the frame, which flickered, and 0.9.1 draws
// them on the CPU (egoist/mygo#1841). So the fills are one instanced shader draw on the GPU,
// each sector's coverage computed from its geometry (sectorShader), and the outlines opaque
// StrokePolylines, which a GPU draws as rounded rectangles (outline). Where the frame draws on
// the CPU, fills fall back to paths, as before.

// sectorGeo is an annular sector with rounded corners: radii r0 < r1 around (cx, cy), from angle
// b0 to b1 (radians clockwise from 12 o'clock), its corners rounded with circles of radius cr.
type sectorGeo struct {
	cx, cy, r0, r1, b0, b1, cr float32
}

func newSectorGeo(cx, cy, r0, r1, b0, b1, corner float32) sectorGeo {
	cr := min(corner, (r1-r0)/2.5, (b1-b0)*r0/2.5)
	if cr < 1 {
		cr = 0
	}
	return sectorGeo{cx, cy, r0, r1, b0, b1, cr}
}

// frame returns the screen angle of the sector's middle and its half width: the sector, turned
// by -m about its centre, lies around the x axis, symmetric about it.
func (g sectorGeo) frame() (m, h float64) {
	return float64(a0Top + (g.b0+g.b1)/2), float64(g.b1-g.b0) / 2
}

// walk traces the outline: from the inner corner at b0 out along its edge, round the outer
// circle to b1, in along b1's edge and back round the inner circle, each corner a quarter-ish
// circle of radius cr about a point cr inside both its sides. arc goes about (x, y), radius r,
// from one screen angle to another, starting where the outline is.
func (g sectorGeo) walk(move, line func(x, y float32), arc func(x, y, r float32, from, to float64)) {
	m, h := g.frame()
	cm, sm := math.Cos(m), math.Sin(m)
	cr, R0, R1 := float64(g.cr), float64(g.r0+g.cr), float64(g.r1-g.cr)
	d := [2]float64{math.Cos(h), math.Sin(h)}  // along the b1 edge, outwards
	n := [2]float64{math.Sin(h), -math.Cos(h)} // across it, inwards
	vertex := func(R float64) [2]float64 {     // a corner's centre on the circle of radius R
		t := math.Sqrt(max(0, R*R-cr*cr))
		return [2]float64{cr*n[0] + t*d[0], cr*n[1] + t*d[1]}
	}
	foot := func(R float64) [2]float64 { // where its circle meets the edge
		t := math.Sqrt(max(0, R*R-cr*cr))
		return [2]float64{t * d[0], t * d[1]}
	}
	vo, vi, fo, fi := vertex(R1), vertex(R0), foot(R1), foot(R0)
	ang := func(v [2]float64) float64 { return math.Atan2(v[1], v[0]) }
	to, ti := ang(vo), ang(vi)
	// screen turns a point of the b1 side (s = 1) or, mirrored, the b0 side (s = -1)
	screen := func(v [2]float64, s float64) (float32, float32) {
		x, y := v[0], s*v[1]
		return g.cx + float32(x*cm-y*sm), g.cy + float32(x*sm+y*cm)
	}
	sa := func(a, s float64) float64 { return m + s*a } // a local angle on screen
	corner := func(v [2]float64, s, from, to float64) {
		if cr == 0 {
			return
		}
		x, y := screen(v, s)
		from, to = sa(from, s), sa(to, s)
		to = from + math.Remainder(to-from, 2*math.Pi) // the short way round
		arc(x, y, g.cr, from, to)
	}
	in := ang([2]float64{-n[0], -n[1]}) // from a corner's centre to its edge
	move(screen(fi, -1))
	line(screen(fo, -1))
	corner(vo, -1, in, to)
	arc(g.cx, g.cy, g.r1, m-to, m+to)
	corner(vo, 1, to, in)
	line(screen(fi, 1))
	corner(vi, 1, in, ti+math.Pi)
	arc(g.cx, g.cy, g.r0, m+ti, m-ti)
	corner(vi, -1, ti+math.Pi, in)
}

// sector builds an annular sector with softly rounded corners. Angles are clockwise from 12 o'clock.
func sector(p *ui.Path, cx, cy, r0, r1, b0, b1, corner float32) *ui.Path {
	newSectorGeo(cx, cy, r0, r1, b0, b1, corner).walk(
		func(x, y float32) { p.MoveTo(x, y) },
		func(x, y float32) { p.LineTo(x, y) },
		func(x, y, r float32, from, to float64) { arcTo(p, x, y, r, from, to) })
	return p.Close()
}

// sectorPts appends the sector's outline to dst as a closed line of points, its arcs flattened
// to within .1 DIP.
func sectorPts(dst [][2]float32, g sectorGeo) [][2]float32 {
	start := len(dst)
	pt := func(x, y float32) { dst = append(dst, [2]float32{x, y}) }
	g.walk(pt, pt, func(x, y, r float32, from, to float64) {
		step := 2 * math.Sqrt(2*.1/float64(max(r, .2))) // a chord whose sagitta is .1 DIP
		k := max(1, int(math.Ceil(math.Abs(to-from)/step)))
		for i := 1; i <= k; i++ {
			a := from + (to-from)*float64(i)/float64(k)
			pt(x+float32(math.Cos(a))*r, y+float32(math.Sin(a))*r)
		}
	})
	return append(dst, dst[start]) // closed: the line ends where it began
}

// shape is a sector three ways: its geometry, for the fill shader; its path, for clips, the
// fills of frames drawn on the CPU and the effects drawn over it; and its outline as points,
// for strokes.
type shape struct {
	ui.Path
	geo sectorGeo
	pts [][2]float32
	box ui.Rect // the outline's bounds
}

func (sh *shape) sector(cx, cy, r0, r1, b0, b1, corner float32) *shape {
	sh.geo = newSectorGeo(cx, cy, r0, r1, b0, b1, corner)
	sector(sh.Reset(), cx, cy, r0, r1, b0, b1, corner)
	sh.pts = sectorPts(sh.pts[:0], sh.geo)
	x0, y0, x1, y1 := sh.pts[0][0], sh.pts[0][1], sh.pts[0][0], sh.pts[0][1]
	for _, q := range sh.pts {
		x0, y0, x1, y1 = min(x0, q[0]), min(y0, q[1]), max(x1, q[0]), max(y1, q[1])
	}
	sh.box = ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
	return sh
}

// outline strokes sh's outline width wide in an opaque colour c, which draws without a mask:
// as StrokePath strokes the path (a translucent one would draw with a mask).
func outline(p *ui.Painter, sh *shape, width float32, c ui.Color) {
	p.StrokePolyline(sh.pts, width, c)
}

// over is c as it shows painted over the opaque colour under. mygo blends in sRGB, as Mix mixes.
func over(c, under ui.Color) ui.Color {
	return under.Mix(ui.RGBA(c.R, c.G, c.B, 1), float32(c.A)/255)
}

// stroke is an outline of a sector: width DIPs wide, centred on its edge, in a colour.
type stroke struct {
	w float32
	c ui.Color
}

// sectorPaint is how a sector is painted: a fill, then glow and then line over it, outlines
// as StrokePath strokes its path (none where their width is 0).
type sectorPaint struct {
	fill       ui.Color
	glow, line stroke
}

// sectorShader paints a sector from its distance to the sector's edge: in the sector's frame
// (see sectorGeo.frame), folded onto the b1 side, the sector is the set of points within cr
// of its core, the band between radii r0+cr and r1-cr on the inner side of a line cr in from
// the b1 edge, whose boundary is two arcs and a segment between the corners' centres. (Past
// the middle of a sector wider than a half turn, dot(q, d) < 0, the nearest of the edge is the
// centre, further than cr. A sector from the centre, r0 0, has no inner edge: its sides meet
// there; a whole ring, h π, has no sides.) The fill covers what is inside, an outline the band about the edge; each is
// composited over the one before, so the quad shows what the fill and the two strokes of the
// path would, drawn in turn. Its data:
//
//	0, 1    the centre, in DIPs from the quad's corner
//	2, 3    the screen angle of the sector's middle, and half its width
//	4 to 6  r0, r1 and cr
//	7, 8    the fill: its RGB, 8 bits each in one number (rgb24), and its alpha
//	9 to 11 the glow: rgb24, alpha and width
//	12 to 14 the line: rgb24, alpha and width
const sectorShaderSrc = `package main

// rgb24 unpacks a colour packed as R<<16 | G<<8 | B: whole numbers below 2^24, which a float
// holds exactly, divided by powers of two, which are exact too.
func rgb24(v float) vec3 {
	r := floor(v / 65536.0)
	g := floor((v - r*65536.0) / 256.0)
	return vec3(r, g, v-r*65536.0-g*256.0) / 255.0
}

// over composites the colour at slot i, covering cov, over c (premultiplied).
func over(c vec4, i int, cov float) vec4 {
	a := instanceData(i+1) * cov
	return vec4(rgb24(instanceData(i))*a, a) + c*(1-a)
}

// band is how much of the pixel a stroke w DIPs wide about the edge covers, sd away from it.
func band(sd float, w float) float {
	s := pixelScale()
	return clamp(min(w*s, 0.5+(w/2-abs(sd))*s), 0, 1)
}

func Fragment(dst vec4, pos vec2) vec4 {
	p := pos - vec2(instanceData(0), instanceData(1))
	m := instanceData(2)
	h := instanceData(3)
	cm := cos(m)
	sm := sin(m)
	q := vec2(p.x*cm+p.y*sm, abs(p.y*cm-p.x*sm))
	n := vec2(sin(h), -cos(h))
	d := vec2(cos(h), sin(h))
	cr := instanceData(6)
	R0 := instanceData(4) + cr
	R1 := instanceData(5) - cr
	ti := sqrt(max(R0*R0-cr*cr, 0))
	to := sqrt(max(R1*R1-cr*cr, 0))
	vi := cr*n + ti*d
	vo := cr*n + to*d
	r := length(q)
	a := atan2(q.y, q.x)
	dout := abs(r - R1)
	if a > atan2(vo.y, vo.x) {
		dout = length(q - vo)
	}
	din := abs(r - R0)
	if a > atan2(vi.y, vi.x) {
		din = length(q - vi)
	}
	if instanceData(4) <= 0 {
		din = 1e6
	}
	t := clamp(dot(q-cr*n, d), ti, to)
	dside := length(q - cr*n - t*d)
	if h >= 3.14159 {
		dside = 1e6
	}
	dist := min(min(dout, din), dside)
	if r >= R0 && r <= R1 && a <= h && (dot(q, n) >= cr || dot(q, d) < 0) {
		dist = -dist
	}
	sd := dist - cr
	c := over(vec4(0), 7, clamp(0.5-sd*pixelScale(), 0, 1))
	c = over(c, 9, band(sd, instanceData(11)))
	return over(c, 12, band(sd, instanceData(14)))
}
`

const sectorStride = 15

var sectorShader = shader.MustCompile(sectorShaderSrc)

// sectorFills gathers the sectors a frame paints, to draw them in one call (draw).
type sectorFills struct {
	rects  []ui.Rect
	data   []float32
	shapes []*shape
	paints []sectorPaint
}

// add paints sh as sp, when the sectors are drawn.
func (f *sectorFills) add(sh *shape, sp sectorPaint) {
	if sp.fill.A == 0 && (sp.glow.w <= 0 || sp.glow.c.A == 0) && (sp.line.w <= 0 || sp.line.c.A == 0) {
		return
	}
	f.shapes = append(f.shapes, sh)
	f.paints = append(f.paints, sp)
}

// fill adds sh filled with c alone.
func (f *sectorFills) fill(sh *shape, c ui.Color) { f.add(sh, sectorPaint{fill: c}) }

// draw draws the sectors added, in order: with the shader where the frame draws on a GPU
// (Painter.Accelerated), and as paths elsewhere, where package shader would run pixel by pixel.
func (f *sectorFills) draw(p *ui.Painter, gpu bool) {
	defer func() { f.shapes, f.paints = f.shapes[:0], f.paints[:0] }()
	if !gpu {
		for i, sh := range f.shapes {
			sp := f.paints[i]
			p.FillPath(&sh.Path, sp.fill)
			for _, st := range [2]stroke{sp.glow, sp.line} {
				if st.w > 0 && st.c.A > 0 {
					p.StrokePath(&sh.Path, st.w, st.c)
				}
			}
		}
		return
	}
	rgb24 := func(c ui.Color) float32 { return float32(int(c.R)<<16 | int(c.G)<<8 | int(c.B)) }
	alpha := func(c ui.Color) float32 { return float32(c.A) / 255 }
	f.rects, f.data = f.rects[:0], f.data[:0]
	sc := p.Scale()
	for i, sh := range f.shapes {
		g, sp := sh.geo, f.paints[i]
		// the quad on whole pixels, a pixel beyond the outlines: positions in it go from there
		e := 1 + max(sp.glow.w, sp.line.w)/2
		x0, y0 := float32(math.Floor(float64((sh.box.X-e)*sc)))/sc, float32(math.Floor(float64((sh.box.Y-e)*sc)))/sc
		x1, y1 := float32(math.Ceil(float64((sh.box.X+sh.box.W+e)*sc)))/sc, float32(math.Ceil(float64((sh.box.Y+sh.box.H+e)*sc)))/sc
		r := ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
		m, h := g.frame()
		f.rects = append(f.rects, r)
		f.data = append(f.data, g.cx-r.X, g.cy-r.Y, float32(m), float32(h), g.r0, g.r1, g.cr,
			rgb24(sp.fill), alpha(sp.fill),
			rgb24(sp.glow.c), alpha(sp.glow.c), max(sp.glow.w, 0),
			rgb24(sp.line.c), alpha(sp.line.c), max(sp.line.w, 0))
	}
	for i := 0; i < len(f.rects); i += shader.MaxInstances {
		j := min(len(f.rects), i+shader.MaxInstances)
		in := shader.Instances{Rects: f.rects[i:j], Data: f.data[i*sectorStride : j*sectorStride], Stride: sectorStride}
		sectorShader.DrawInstances(p, in, shader.Options{})
	}
}
