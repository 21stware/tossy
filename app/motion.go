package main

import (
	"math"
	"os"
	"time"

	"github.com/egoist/mygo/ui"
)

// Eases used across the app. Every tween is a cubic and none overshoots: easeStd (cubic out)
// for things arriving or settling, ease (cubic in-out, sunburst.go) for things that go and
// come back, easeIn (cubic in) for things leaving.
func easeStd(t float64) float64 { return easeOut(t) }
func easeIn(t float64) float64  { return t * t * t }

// progress is how far along an animation that started at t0 (after delay) is, 0..1; it asks
// for another frame while running.
func (a *app) progress(t0 time.Time, delay, dur time.Duration) float64 {
	if t0.IsZero() || !a.motion() {
		return 1
	}
	k := float64(a.now.Sub(t0)-delay) / float64(dur)
	if k < 1 {
		a.frame = true
	}
	return max(0, min(1, k))
}

// stagger lets panel part i rise in after a scene change (see rise).
func (a *app) stagger(el ui.Element, i int) ui.Element { return a.rise(el, a.panelT0, i) }

// rise lets part i of a group that (re)appeared at t0 come in: each part 24ms after the one
// before, sliding in from the right edge of its (clipped) panel like a drawer, on a cubic and
// without a fade. A group that changes again mid-way simply restarts from where the eye is.
func (a *app) rise(el ui.Element, t0 time.Time, i int) ui.Element {
	k := a.progress(t0, time.Duration(min(i, 10))*24*time.Millisecond, 340*time.Millisecond)
	if k >= 1 {
		return el
	}
	return el.Translate(max(a.rightW, 160)*(1-float32(easeStd(k))), 0)
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// spring is a value that follows its target like a damped spring: it can be retargeted mid-way
// without a restart or a jolt, keeps its velocity, and answers within the first frame.
type spring struct {
	x, v, to float64
	t        time.Time
	set      bool
}

const (
	springK    = 520 // stiffness: settles in about 250ms
	springZeta = 1   // critically damped: the fastest settle that never overshoots
)

func (a *app) springTo(s *spring, to float64) float64 {
	if !s.set || !a.motion() {
		s.x, s.v, s.to, s.t, s.set = to, 0, to, a.now, true
		return to
	}
	s.to = to
	dt := a.now.Sub(s.t).Seconds()
	s.t = a.now
	dt = max(0, min(dt, 1.0/30))
	w := math.Sqrt(springK)
	for dt > 0 {
		h := min(dt, 1.0/240)
		acc := -springK*(s.x-s.to) - 2*springZeta*w*s.v
		s.v += acc * h
		s.x += s.v * h
		dt -= h
	}
	span := max(1, math.Abs(s.to))
	if math.Abs(s.x-s.to) < 1e-3*span && math.Abs(s.v) < 1e-2*span {
		s.x, s.v = s.to, 0
	} else {
		a.frame = true
	}
	return s.x
}

// snap puts a spring at its target at once.
func (s *spring) snap(to float64) { s.x, s.v, s.to, s.set = to, 0, to, true }
