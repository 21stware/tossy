package main

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The callout shelf must always run outward and fit; with no room beside the hovered arc
// the leader bends to another angle rather than folding back over the chart.
func TestCalloutPlacement(t *testing.T) {
	lines := []calloutLine{{h: 15}, {h: 13}}
	box := ui.Rect{X: 0, Y: 0, W: 560, H: 560} // square: little room at the sides
	cx, cy := float32(280), float32(280)
	rad := box.W/2 - 62
	re := rad + 50
	want := float32(180)
	for deg := -180; deg < 180; deg += 5 {
		am := float64(deg) * math.Pi / 180
		g := placeCallout(cx, cy, re, am, box, want, lines)
		ex, ey := polar(cx, cy, re, g.b)
		if math.Abs(math.Cos(g.b)) > 1e-6 && g.right != (math.Cos(g.b) > 0) {
			t.Fatalf("am=%d°: shelf runs inward (b=%.2f right=%v)", deg, g.b, g.right)
		}
		if g.room < want {
			t.Fatalf("am=%d°: room %.0f < %.0f", deg, g.room, want)
		}
		end := ex + want
		if !g.right {
			end = ex - want
		}
		if end < box.X || end > box.X+box.W {
			t.Fatalf("am=%d°: shelf leaves the box (end %.0f)", deg, end)
		}
		tops := lineTops(ey, g.stack, lines)
		if tops[0] < box.Y || tops[1]+13 > box.Y+box.H {
			t.Fatalf("am=%d°: text leaves the box vertically", deg)
		}
		if g.b != am && g.stack == stackSplit {
			t.Fatalf("am=%d°: bent leader with split text", deg)
		}
	}
	// a wide box needs no bend anywhere
	wide := ui.Rect{X: 0, Y: 0, W: 1200, H: 560}
	for deg := -180; deg < 180; deg += 5 {
		am := float64(deg) * math.Pi / 180
		if g := placeCallout(600, 280, re, am, wide, want, lines); g.b != am {
			t.Fatalf("wide am=%d°: unexpected bend", deg)
		}
	}
}
