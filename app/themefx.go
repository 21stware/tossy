package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Effects that belong to one style: the terminal's CRT power-on and
// typed command line, the blueprint's linework drawn before its washes.

type themeFX struct {
	crtT0 time.Time // terminal: the tube switching on

	cmd   string    // terminal: the command line being typed
	cmdT0 time.Time // when it started
}

const (
	crtLine = 130 * time.Millisecond // a dot stretches into a line across the tube
	crtOpen = 300 * time.Millisecond // the line opens into the picture
	crtLife = 760 * time.Millisecond // and the overexposure settles
	cmdRate = 22 * time.Millisecond  // a character typed
)

// ------------------------------------------------------------------------------------------
// terminal: CRT power-on

// crtOn switches the tube on: after a scan, the chart comes up as an old monitor's picture.
func (a *app) crtOn() {
	if a.pal.art != artPhosphor || !a.motion() {
		return
	}
	a.fx.crtT0 = time.Now()
	a.frame = true
}

// paintCRT draws the power-on over the chart: black, a bright dot stretching into a line, the
// line opening into the picture, the picture overexposed and settling.
func (a *app) paintCRT(p *ui.Painter, r ui.Rect) {
	f := &a.fx
	if f.crtT0.IsZero() {
		return
	}
	d := p.Now().Sub(f.crtT0)
	if d >= crtLife || a.pal.art != artPhosphor || !a.motion() {
		f.crtT0 = time.Time{}
		return
	}
	black := a.pal.bg
	glow := a.pal.ink
	cy := r.Y + r.H/2
	switch {
	case d < crtLine:
		k := float32(easeOut(float64(d) / float64(crtLine)))
		p.Fill(r, black, 0)
		w := r.W * k
		p.Fill(ui.Rect{X: r.X + r.W/2 - w/2, Y: cy - 6, W: w, H: 12}, glow.Alpha(.16), 6)
		p.Fill(ui.Rect{X: r.X + r.W/2 - w/2, Y: cy - 1, W: w, H: 2}, glow.Mix(ui.RGB(255, 255, 255), .6), 1)
	case d < crtLine+crtOpen:
		k := float32(easeOut(float64(d-crtLine) / float64(crtOpen)))
		h := 2 + (r.H-2)*k
		top, bot := cy-h/2, cy+h/2
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: top - r.Y}, black, 0)
		p.Fill(ui.Rect{X: r.X, Y: bot, W: r.W, H: r.Y + r.H - bot}, black, 0)
		p.Fill(ui.Rect{X: r.X, Y: top, W: r.W, H: h}, glow.Alpha(.3*(1-k)), 0)
		edge := glow.Mix(ui.RGB(255, 255, 255), .5).Alpha(1 - k*.6)
		p.Fill(ui.Rect{X: r.X, Y: top - 1, W: r.W, H: 2}, edge, 0)
		p.Fill(ui.Rect{X: r.X, Y: bot - 1, W: r.W, H: 2}, edge, 0)
	default:
		k := float32(d-crtLine-crtOpen) / float32(crtLife-crtLine-crtOpen)
		p.Fill(r, glow.Alpha(.14*(1-k)*(1-k)), 0)
	}
	p.AnimationFrame()
}

// typed is the part of the terminal's command line typed so far: a new line types itself
// out, a character every cmdRate; done says whether it is all there.
func (a *app) typed(p *ui.Painter, line string) (string, bool) {
	f := &a.fx
	if !a.motion() {
		return line, true
	}
	if line != f.cmd {
		f.cmd, f.cmdT0 = line, p.Now()
		if !f.crtT0.IsZero() { // after the tube is on
			f.cmdT0 = f.crtT0.Add(crtLine + crtOpen)
		}
	}
	rs := []rune(line)
	n := max(0, int(p.Now().Sub(f.cmdT0)/cmdRate))
	if n >= len(rs) {
		return line, true
	}
	p.AnimationFrame()
	return string(rs[:n]), false
}

// ------------------------------------------------------------------------------------------
// blueprint: linework first

// washK is how much of the blueprint's washes (the sector fills) show: on the intro and on a
// zoom the lines are drawn first and the washes laid in after them.
func (a *app) washK(now time.Time, intro float64) float32 {
	if a.pal.art != artBlueprint || !a.motion() {
		return 1
	}
	k := float32(1)
	if intro < 1 {
		k = float32(easeStd(max(0, min(1, (intro-.55)/.45))))
	}
	if s := &a.sun; s.zooming {
		z := float64(now.Sub(s.zoomT0)) / float64(zoomDur)
		k = min(k, float32(easeStd(max(0, min(1, (z-.5)/.5)))))
	}
	return k
}
