package main

import (
	"math"
	"os"
	"time"

	"github.com/egoist/mygo/ui"
)

// bez evaluates a CSS cubic-bezier(x1,y1,x2,y2) easing at t.
func bez(x1, y1, x2, y2, t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	// solve x(u) = t by Newton, fall back to bisection
	cx := 3 * x1
	bx := 3*(x2-x1) - cx
	ax := 1 - cx - bx
	cy := 3 * y1
	by := 3*(y2-y1) - cy
	ay := 1 - cy - by
	xf := func(u float64) float64 { return ((ax*u+bx)*u + cx) * u }
	u := t
	for i := 0; i < 8; i++ {
		x := xf(u) - t
		if math.Abs(x) < 1e-5 {
			break
		}
		d := (3*ax*u+2*bx)*u + cx
		if math.Abs(d) < 1e-6 {
			break
		}
		u -= x / d
	}
	if u < 0 || u > 1 || math.Abs(xf(u)-t) > 1e-3 {
		lo, hi := 0.0, 1.0
		u = t
		for i := 0; i < 30; i++ {
			if xf(u) < t {
				lo = u
			} else {
				hi = u
			}
			u = (lo + hi) / 2
		}
	}
	return ((ay*u+by)*u + cy) * u
}

// Eases used across the app, named after where the prototype uses them.
func easeStd(t float64) float64    { return bez(.2, .8, .2, 1, t) } // panels, bars
func easeBack(t float64) float64   { return bez(.3, 1.5, .5, 1, t) } // lid
func easeBounce(t float64) float64 { return bez(.3, 1.6, .5, 1, t) } // bin bump
func easeTier(t float64) float64   { return bez(.3, 1.2, .5, 1, t) } // bin tier change
func easeIn(t float64) float64     { return bez(.5, 0, .8, .4, t) }  // shard dissolve

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
// before, up 10px with a slight overshoot, its opacity ahead of its position so it reads at
// once. A group that changes again mid-way simply restarts from where the eye is.
func (a *app) rise(el ui.Element, t0 time.Time, i int) ui.Element {
	k := a.progress(t0, time.Duration(min(i, 10))*24*time.Millisecond, 340*time.Millisecond)
	if k >= 1 {
		return el
	}
	y := float32(easeRise(k))
	o := float32(easeOut(min(1, k*1.7)))
	return el.Opacity(o).Translate(0, 10*(1-y))
}

// easeRise settles with a small overshoot, like a light spring.
func easeRise(t float64) float64 { return bez(.2, 1.25, .35, 1, t) }

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
	springZeta = .8  // a hint of overshoot, so it feels alive rather than braked
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
