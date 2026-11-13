package main

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// Icons, in the prototype's 24×24 viewBox, stroked.
const (
	icoBack    = "M14.5 6L8.5 12L14.5 18"
	icoUp      = "M9 14L4 9l5-5M4 9h10.5C17.5 9 20 11.5 20 14.5C20 17.5 17.5 20 14.5 20H11"
	icoTrash   = "M4 7h16M9.5 7V4.8h5V7M6.5 7l.9 12.2q.1 1.3 1.5 1.3h6.2q1.4 0 1.5-1.3L17.5 7"
	icoSparkle = "M12 3.5l1.6 4.2 4.2 1.6-4.2 1.6L12 15.1l-1.6-4.2-4.2-1.6 4.2-1.6zM18.5 14.5l.8 2 2 .8-2 .8-.8 2-.8-2-2-.8 2-.8zM5.5 15.5l.6 1.4 1.4.6-1.4.6-.6 1.4-.6-1.4-1.4-.6 1.4-.6z"
	icoClose   = "M6 6L18 18M18 6L6 18"
	icoChevron = "M9 6l6 6-6 6"
	icoFolder  = "M3.5 7.5q0-2 2-2h4l2 2h7q2 0 2 2v8q0 2-2 2h-13q-2 0-2-2z"
)

// strokeIcon draws a 24-unit icon scaled into r.
func strokeIcon(p *ui.Painter, r ui.Rect, d string, c ui.Color, w float32) {
	s := min(r.W, r.H) / 24
	x, y := r.X+(r.W-24*s)/2, r.Y+(r.H-24*s)/2
	p.Transform(ui.Scaling(s, s), x, y, func() {
		p.StrokePath(svgPath(d, x, y).Cap(ui.RoundCap), w, c)
	})
}

func icon(c *ui.Context, d string, size float32, col ui.Color, w float32) ui.Element {
	return ui.Box(c).Size(size, size).Shrink(0).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) { strokeIcon(p, r, d, col, w) })
}

// lockIcon is the protected marker: a shackle over a body.
func lockIcon(c *ui.Context, size float32, col ui.Color) ui.Element {
	return ui.Box(c).Size(size, size).Shrink(0).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		s := r.W / 24
		p.StrokePath(rrect(r.X+5*s, r.Y+10.5*s, 14*s, 10*s, 2.2*s), 1.8*s, col)
		var sh ui.Path
		sh.MoveTo(r.X+8*s, r.Y+10.5*s).LineTo(r.X+8*s, r.Y+8*s)
		arcTo(&sh, r.X+12*s, r.Y+8*s, 4*s, math.Pi, 2*math.Pi)
		sh.LineTo(r.X+16*s, r.Y+10.5*s)
		p.StrokePath(sh.Cap(ui.RoundCap), 1.8*s, col)
	})
}

// spinner is a small turning arc.
func (a *app) spinner(c *ui.Context, size float32, col ui.Color) ui.Element {
	a.frame = a.frame || a.motion()
	return ui.Box(c).Size(size, size).Shrink(0).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		t := float64(p.Now().UnixMilli()%800) / 800 * tau
		cx, cy, rr := r.X+r.W/2, r.Y+r.H/2, r.W/2-1
		p.StrokePath(circle(&ui.Path{}, cx, cy, rr, 0, tau), 1.6, col.Alpha(.25))
		var s ui.Path
		arcTo(s.MoveTo(polar(cx, cy, rr, t)), cx, cy, rr, t, t+1.9)
		p.StrokePath(s.Cap(ui.RoundCap), 1.6, col)
		if a.motion() {
			p.AnimationFrame()
		}
	})
}

type btnKind int

const (
	btnPlain btnKind = iota
	btnPrimary
	btnSolid
	btnDanger
	btnGhost // icon-only, no fill until hovered
)

// btn is the prototype's .btn: 30px tall (26 small), 7px corners, filled on hover.
func (a *app) btn(c *ui.Context, label string, kind btnKind, small bool, fn func()) ui.Element {
	pal := a.pal
	h := float32(30)
	if small {
		h = 26
	}
	var bg, fg, hov ui.Color
	switch kind {
	case btnPrimary:
		bg, fg, hov = pal.text, pal.bg, pal.text.Alpha(.88)
	case btnSolid:
		bg, fg, hov = pal.panel2, pal.text, pal.panel2.Mix(pal.text, .08)
	case btnDanger:
		bg, fg, hov = pal.level[2], pal.bg, pal.level[2].Alpha(.88)
	default:
		bg, fg, hov = ui.Transparent, pal.t2, pal.hov
	}
	pad := float32(12)
	if small {
		pad = 10
	}
	if label == "" {
		pad = 0
	}
	e := ui.Row(c).Height(h).Padding(0, pad).Gap(6).Radius(7).AlignItems(ui.Center).Justify(ui.Center).
		Background(bg).TextColor(fg).FontSize(12.5).FontWeight(500).Cursor(ui.CursorPointer).Shrink(0)
	if label == "" {
		e.Width(h)
	}
	e.Children(func() {
		if fn != nil {
			fn()
		}
		if label != "" {
			ui.Text(c, label).SingleLine()
		}
	})
	if e.Hovered() {
		e.Background(hov)
		if kind == btnPlain || kind == btnGhost {
			e.TextColor(pal.text)
		}
	}
	return e
}

// tween eases a value to each new target over dur.
type tween struct {
	from, to, cur float64
	t0            time.Time
	set           bool
}

func (a *app) tweenTo(tw *tween, v float64, dur time.Duration) float64 {
	if !tw.set {
		tw.from, tw.to, tw.cur, tw.set = v, v, v, true
	}
	if v != tw.to {
		tw.from, tw.to, tw.t0 = tw.cur, v, a.now
	}
	k := a.progress(tw.t0, 0, dur)
	tw.cur = tw.from + (tw.to-tw.from)*easeStd(k)
	return tw.cur
}

// diskPct is a share of the disk, never a misleading 0.0%.
func diskPct(n, total int64) string {
	f := 100 * float64(n) / float64(total)
	if n > 0 && f < .1 {
		return "<0.1%"
	}
	return fmt.Sprintf("%.1f%%", f)
}
