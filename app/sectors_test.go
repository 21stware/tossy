package main

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The shader paints what the sector's path, filled and then stroked twice, would: drawn both
// ways, at the scales of a Retina and a plain display, translucent and as thick as the toon
// styles' ink, the pixels agree but for the antialiasing of the edges.
func TestSectorShaderMatchesPath(t *testing.T) {
	type sec struct{ r0, r1, b0, b1, corner float32 }
	secs := []sec{
		{40, 70, .1, 1.2, 3},          // rounded corners
		{72, 110, 1.3, 4.6, 3},        // wider than a half turn
		{40, 70, 1.3, 1.36, 3},        // thin: corners shrink
		{112, 140, 4.7, 6.2, 0},       // sharp corners
		{72, 110, 4.7, 4.7 + 1e-3, 3}, // a sliver
		{20, 38, 0, tau - .02, 3},     // nearly a whole ring
		{0, 150, 2.5, 2.536, 0},       // a wedge from the centre, as the radar's trail
		{0, 150, 3.3, 3.8, 3},         // a wider one: corners only outside
		{152, 156, 0, tau, 0},         // a whole ring, as the analysis' pulse
	}
	const W, H = 320, 320
	cx, cy := float32(W/2), float32(H/2)
	paints := []sectorPaint{
		{fill: ui.RGBA(220, 40, 60, .8)},
		{fill: ui.RGBA(190, 240, 220, .9), line: stroke{1.8, ui.RGBA(24, 74, 66, .92*.35)}},
		{fill: ui.RGBA(30, 200, 90, .12), glow: stroke{3.2, ui.RGBA(51, 255, 102, .09)}, line: stroke{.9, ui.RGBA(150, 255, 170, .9)}},
		{fill: ui.RGBA(255, 255, 255, .1), line: stroke{1.1, ui.RGBA(255, 255, 255, .95)}}, // white: the largest packed colour
	}
	for _, scale := range []float32{1, 2} {
		img := [2][]byte{}
		for k, viaShader := range []bool{false, true} {
			var fills sectorFills
			view := func(c *ui.Context) {
				ui.Box(c).Fill().Background(ui.RGB(255, 255, 255)).Draw(func(p *ui.Painter, r ui.Rect) {
					for i, s := range secs {
						sp := paints[i%len(paints)]
						if s.b1-s.b0 >= tau { // a whole ring is filled alone: its path has a seam a stroke would show
							sp = sectorPaint{fill: sp.fill}
						}
						fills.add((&shape{}).sector(cx, cy, s.r0, s.r1, s.b0, s.b1, s.corner), sp)
					}
					fills.draw(p, viaShader)
				})
			}
			tt := ui.NewTester(view, W, H)
			tt.SetScale(scale)
			img[k] = tt.Image().Pix
		}
		// the antialiasing of an edge differs, a path's by area and the shader's by distance:
		// no pixel may differ by more than half, and the paint laid down must be the same
		var edge, wrong int
		var ink [2]float64
		for i := 0; i < len(img[0]); i += 4 {
			d := 0
			for c := 0; c < 3; c++ {
				d = max(d, int(math.Abs(float64(img[0][i+c])-float64(img[1][i+c]))))
				ink[0] += 255 - float64(img[0][i+c])
				ink[1] += 255 - float64(img[1][i+c])
			}
			switch {
			case d > 128:
				wrong++
			case d > 8:
				edge++
			}
		}
		drift := math.Abs(ink[1]-ink[0]) / ink[0]
		t.Logf("scale %v: %d pixels differ at an edge, %d by more than half; ink %+.2f%%", scale, edge, wrong, 100*(ink[1]-ink[0])/ink[0])
		if wrong > 0 || drift > .005 {
			t.Errorf("scale %v: the shader's sectors differ from the paths': %d pixels wrong, ink off by %.2f%%", scale, wrong, 100*drift)
		}
	}
}

// The outline's points lie on the edge of the path's fill: within .15 DIP of the walk's
// geometry, whatever the corner.
func TestSectorPtsOnOutline(t *testing.T) {
	for _, corner := range []float32{0, 3, 8} {
		for _, r0 := range []float32{40, 0} {
			g := newSectorGeo(100, 100, r0, 90, .3, 2.1, corner)
			pts := sectorPts(nil, g)
			if pts[0] != pts[len(pts)-1] {
				t.Fatalf("corner %v: the outline is not closed", corner)
			}
			for _, q := range pts {
				if d := sectorDist(g, q[0], q[1]); math.Abs(d) > .15 {
					t.Errorf("corner %v, r0 %v: point %v is %.3f DIP off the sector's edge", corner, r0, q, d)
					break
				}
			}
		}
	}
}

// sectorDist is the shader's signed distance from the sector's edge, in Go.
func sectorDist(g sectorGeo, x, y float32) float64 {
	m, h := g.frame()
	px, py := float64(x-g.cx), float64(y-g.cy)
	qx, qy := px*math.Cos(m)+py*math.Sin(m), math.Abs(py*math.Cos(m)-px*math.Sin(m))
	nx, ny := math.Sin(h), -math.Cos(h)
	dx, dy := math.Cos(h), math.Sin(h)
	cr := float64(g.cr)
	R0, R1 := float64(g.r0)+cr, float64(g.r1)-cr
	ti, to := math.Sqrt(max(R0*R0-cr*cr, 0)), math.Sqrt(max(R1*R1-cr*cr, 0))
	vix, viy := cr*nx+ti*dx, cr*ny+ti*dy
	vox, voy := cr*nx+to*dx, cr*ny+to*dy
	r := math.Hypot(qx, qy)
	a := math.Atan2(qy, qx)
	dout := math.Abs(r - R1)
	if a > math.Atan2(voy, vox) {
		dout = math.Hypot(qx-vox, qy-voy)
	}
	din := math.Abs(r - R0)
	if a > math.Atan2(viy, vix) {
		din = math.Hypot(qx-vix, qy-viy)
	}
	if g.r0 <= 0 {
		din = math.Inf(1)
	}
	t := max(ti, min(to, (qx-cr*nx)*dx+(qy-cr*ny)*dy))
	dside := math.Hypot(qx-cr*nx-t*dx, qy-cr*ny-t*dy)
	if h >= 3.14159 {
		dside = math.Inf(1)
	}
	dist := min(dout, din, dside)
	if r >= R0 && r <= R1 && a <= h && (qx*nx+qy*ny >= cr || qx*dx+qy*dy < 0) {
		dist = -dist
	}
	return dist - cr
}
