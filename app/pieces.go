package main

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// Decorations draw piece by piece. mygo rasterizes a path's coverage into a mask of its bounds,
// in an atlas each frame shares (see sectorFills), so one path of many small pieces spread
// around the chart or across the window, as a ring of ticks or a dotted backdrop, takes a mask
// of all that area though it covers little of it: a few of them fill the atlas, and mygo then
// draws the frame's paths on the CPU. Drawn piece by piece, a tick or a short dash is a line of
// one segment, which a GPU draws without a mask (strokeLine), and a dot, a heart or a corner
// mark is a path of its own, whose small mask mygo keeps from frame to frame.

// strokeLine strokes a line from (x0, y0) to (x1, y1), width wide with round caps, as
// StrokePath strokes one: on a GPU without a mask, translucent too, being one segment.
func strokeLine(p *ui.Painter, x0, y0, x1, y1, width float32, c ui.Color) {
	pts := [2][2]float32{{x0, y0}, {x1, y1}}
	p.StrokePolyline(pts[:], width, c)
}

// dashedRing strokes the circle of radius r about (cx, cy) in dashes, as StrokePath strokes a
// circle(…, from, from+tau) path with Dash(0, pattern...): the dashes and gaps alternate along
// it clockwise from the angle from, with round caps. A dash short enough to look straight is a
// line of one segment; a longer one, a small path of its own.
func dashedRing(p *ui.Painter, cx, cy, r float32, from float64, pattern []float32, width float32, c ui.Color) {
	R := float64(r)
	total := 2 * math.Pi * R
	at := func(s float64) (float32, float32) { return polar(cx, cy, r, from+s/R) }
	s, i := 0.0, 0
	for s < total {
		l := float64(pattern[i%len(pattern)])
		if i%2 == 0 { // a dash
			e := min(s+l, total)
			if e-s <= 6 {
				x0, y0 := at(s)
				x1, y1 := at(e)
				strokeLine(p, x0, y0, x1, y1, width, c)
			} else {
				var d ui.Path
				p.StrokePath(circle(&d, cx, cy, r, from+s/R, from+e/R), width, c)
			}
		}
		s += l
		i++
	}
}

// strokeRect strokes the rectangle r width wide, centred on its edge, as StrokePath strokes a
// closed rectangular path with mitred corners: a rectangle's border, without a mask.
func strokeRect(p *ui.Painter, r ui.Rect, width float32, c ui.Color) {
	h := width / 2
	p.Stroke(ui.Rect{X: r.X - h, Y: r.Y - h, W: r.W + width, H: r.H + width}, c, 0, width)
}

// hLine and vLine fill a horizontal or vertical line width wide, as StrokePath strokes one
// with butt caps: a thin rectangle, without a mask.
func hLine(p *ui.Painter, x0, x1, y, width float32, c ui.Color) {
	p.Fill(ui.Rect{X: min(x0, x1), Y: y - width/2, W: abs32(x1 - x0), H: width}, c, 0)
}

func vLine(p *ui.Painter, x, y0, y1, width float32, c ui.Color) {
	p.Fill(ui.Rect{X: x - width/2, Y: min(y0, y1), W: width, H: abs32(y1 - y0)}, c, 0)
}
