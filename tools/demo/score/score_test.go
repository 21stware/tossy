package score

import (
	"math"
	"testing"
)

// TestRender writes a short track with every kind of section and checks that it is sound: no
// sample out of range or not a number, something to hear in each section, silence at the end.
func TestRender(t *testing.T) {
	names := []string{"open", "full", "flurry", "scan", "see", "click", "throw", "undo", "haul", "payoff", "end"}
	var c Cues
	for i, name := range names {
		c.Sections = append(c.Sections, Section{Name: name, T: float64(i) * 2})
		c.Hits = append(c.Hits, Hit{T: float64(i) * 2, K: .1 + .02*float64(i%8)})
	}
	c.Ticks = []float64{4, 4.5, 5, 5.25, 5.5, 5.625, 5.75, 5.875}
	c.End = float64(len(names)) * 2
	l, r := Render(c)
	if len(l) != len(r) || len(l) < int(c.End*SR) {
		t.Fatalf("%d and %d samples for %.0fs", len(l), len(r), c.End)
	}
	for i := range l {
		for _, x := range []float32{l[i], r[i]} {
			if v := float64(x); math.IsNaN(v) || math.Abs(v) > 1 {
				t.Fatalf("sample %d is %v", i, x)
			}
		}
	}
	rms := func(t0, t1 float64) float64 {
		var s float64
		a, b := int(t0*SR), min(len(l), int(t1*SR))
		for i := a; i < b; i++ {
			s += float64(l[i]) * float64(l[i])
		}
		return math.Sqrt(s / float64(b-a))
	}
	for i, name := range names {
		if v := rms(float64(i)*2, float64(i)*2+2); v < .003 {
			t.Errorf("%s is silent: rms %.4f", name, v)
		}
	}
	if v := rms(c.End-.02, c.End+.1); v > .002 {
		t.Errorf("the track does not end in silence: rms %.4f", v)
	}
}
