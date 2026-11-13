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

// stagger fades panel part i in from 10px below, one after another (the prototype's staggerIn).
func (a *app) stagger(el ui.Element, i int) ui.Element {
	if !a.sceneT0.IsZero() { // leaving: fade up and out, quicker
		k := a.progress(a.sceneT0, time.Duration(min(i, 6))*12*time.Millisecond, 100*time.Millisecond)
		e := float32(easeStd(k))
		return el.Opacity(1-e).Translate(0, -6*e)
	}
	k := a.progress(a.panelT0, 40*time.Millisecond+time.Duration(i)*26*time.Millisecond, 300*time.Millisecond)
	if k >= 1 {
		return el
	}
	e := float32(easeStd(k))
	return el.Opacity(e).Translate(0, 10*(1-e))
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}
