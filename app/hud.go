package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/fanfree/fanfree/engine/scan"
)

// paintHUD draws the instrument layer over the chart: bezel, counter-rotating dashed rings,
// reticle, the amber callout for the hovered arc and corner readouts. While scanning it is a
// radar sweep with live counters.
func (a *app) paintHUD(p *ui.Painter, r ui.Rect) {
	pal := a.pal
	if a.t == nil {
		a.paintScanning(p, r)
		return
	}
	s := &a.sun
	if s.rad <= 0 {
		return
	}
	switch pal.instr {
	case instrHUD:
		a.instrHUD(p, r)
	case instrMech:
		a.instrHUD(p, r)
		a.instrMech(p, r)
	case instrBlueprint:
		a.instrBlueprint(p, r)
	case instrTerminal:
		a.instrTerminal(p, r)
	case instrSwiss:
		a.instrSwiss(p, r)
	case instrTE:
		a.instrTE(p, r)
	}
	a.paintAnalysis(p, r)
	a.paintScreen(p, r)
	a.paintCallout(p, r)
}

// paintCallout leads a line from the hovered arc to a label outside the rings.
func (a *app) paintCallout(p *ui.Painter, r ui.Rect) {
	s, pal := &a.sun, a.pal
	var h *arc
	if a.hoverAgg != nil {
		h = a.hoverAgg
	} else if a.hover != scan.None {
		for i := range s.arcs {
			if s.arcs[i].node == a.hover {
				h = &s.arcs[i]
				break
			}
		}
	}
	if h == nil {
		return
	}
	col := pal.amber
	if pal.instr == instrNone {
		col = pal.text.Alpha(.8)
	}
	am := float64(a0Top + (h.a0+h.a1)/2)
	c, sn := float32(math.Cos(am)), float32(math.Sin(am))
	x0, y0 := s.cx+c*h.r1, s.cy+sn*h.r1
	re := s.rad + 30
	if pal.instr != instrNone {
		re = s.rad + 50
	}
	x1, y1 := s.cx+c*re, s.cy+sn*re
	name := "较小项目"
	sub := fmtNum(int64(h.count)) + " 项聚合"
	if h.node != scan.None {
		name = a.name(h.node)
		sub = pct(h.size, a.t.Nodes[a.focus].Size)
		if a.t.Nodes[h.node].IsDir() {
			sub += " · " + fmtNum(int64(a.t.Nodes[h.node].Count-1)) + " 项"
		}
	}
	name = clipText(p, name, 220, 11.5, 600)
	info := fmtSize(h.size) + " · " + sub
	w1, _ := p.MeasureText(0, ui.Span{Text: name, Size: 11.5, Weight: 600})
	w2, _ := p.MeasureText(0, ui.Span{Text: info, Size: 10, Font: "monospace"})
	shelf := max(w1, w2) + 10
	// the shelf runs outward, or back inward when it would leave the chart
	right := c >= 0
	if right && x1+shelf > r.X+r.W-6 {
		right = false
	} else if !right && x1-shelf < r.X+6 {
		right = true
	}
	x2 := x1 + shelf
	lx := x1 + 5
	if !right {
		x2 = x1 - shelf
		lx = x2 + 5
	}
	var ln ui.Path
	ln.MoveTo(x0, y0).LineTo(x1, y1).LineTo(x2, y1)
	p.StrokePath(&ln, 1, col.Alpha(.85))
	p.FillPath((&ui.Path{}).Circle(x0, y0, 2.5), col)
	p.RichText(lx, y1-17, 0, ui.Span{Text: name, Size: 11.5, Weight: 600, Color: col})
	p.RichText(lx, y1+4, 0, ui.Span{Text: info, Size: 10, Font: "monospace", Color: col.Alpha(.75)})
	if a.mode == modeSafety && h.node != scan.None {
		lv := a.ev.Verdict(h.node).Level()
		lc := pal.level[lv]
		p.FillPath((&ui.Path{}).Circle(lx+3, y1+24, 3), lc)
		p.RichText(lx+10, y1+17, 0, ui.Span{Text: levelTitles[lv], Size: 10.5, Weight: 500, Color: lc})
	}
}

func (a *app) paintScanning(p *ui.Painter, r ui.Rect) {
	pal := a.pal
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	rad := max(60, min(r.W, r.H)/2-70)
	ink := pal.ink
	if pal.instr == instrNone {
		ink = pal.text
	}
	var g ui.Path
	for i := 1; i <= 4; i++ {
		g.Circle(cx, cy, rad*float32(i)/4)
	}
	p.StrokePath(&g, 1, ink.Alpha(.08))
	if a.scanErr != nil {
		centerText(p, cx, cy, "扫描失败："+firstLine(a.scanErr.Error()), 13, 500, pal.level[2], "")
		return
	}
	if !a.scanning || a.prog == nil {
		return
	}
	now := p.Now()
	ang := float64(now.Sub(a.scanStart)) / float64(time.Second) * tau / 2.4
	// sweep: a fan of thin wedges fading behind the beam
	for i := 0; i < 24; i++ {
		b := ang - float64(i)*.035
		var w ui.Path
		w.MoveTo(cx, cy)
		w.LineTo(cx+float32(math.Cos(b))*rad, cy+float32(math.Sin(b))*rad)
		arcTo(&w, cx, cy, rad, b, b-.036)
		w.Close()
		p.FillPath(&w, ink.Alpha(.16*(1-float32(i)/24)))
	}
	pr := a.prog
	centerText(p, cx, cy-14, fmtSize(pr.Bytes.Load()), 26, 600, pal.text, "")
	centerText(p, cx, cy+14, fmtNum(pr.Files.Load())+" 个文件 · "+fmtNum(pr.Dirs.Load())+" 个文件夹", 11, 400, pal.t3, "")
	cur := pr.Current()
	if cur != "" {
		centerText(p, cx, cy+rad+24, clipText(p, cur, r.W-60, 10.5, 400), 10.5, 400, pal.t4, "monospace")
	}
	if a.pal.motion {
		p.AnimationFrameRate(30)
	}
	a.paintScreen(p, r)
}
