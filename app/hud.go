package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/21stware/tossy/engine/scan"
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
	case instrTE:
		a.instrTE(p, r)
	}
	a.paintAnalysis(p, r)
	if a.scanning && a.prog != nil && a.sceneOn() == sceneBrowse { // the sweep lights the disk chart's arcs, not the trash's
		a.paintRescan(p, r)
	}
	a.paintScreen(p, r)
	a.paintCallout(p, r)
	a.paintCRT(p, r)
}

// paintCallout leads a line from the hovered arc to a label outside the rings. The shelf
// under the label always runs outward (away from the chart); when the chart edge leaves no
// room for it, the leader bends: out to just past the rings, along them to an angle with
// room, then out to the shelf. Text is stacked on the side of the shelf away from the bend.
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
	re := s.rad + 30
	if pal.instr != instrNone {
		re = s.rad + 50
	}
	name := T("较小项目")
	sub := Tf("%s 项聚合", fmtNum(int64(h.count)))
	if h.node != scan.None {
		name = a.name(h.node)
		sub = pct(h.size, a.t.Nodes[a.focus].Size)
		if a.t.Nodes[h.node].IsDir() {
			sub += " · " + nItems(int64(a.t.Nodes[h.node].Count-1))
		}
	}
	lines := []calloutLine{
		{sp: ui.Span{Text: clipText(p, name, 220, 11.5, 600), Size: 11.5, Weight: 600, Color: col}, h: 15},
		{sp: ui.Span{Text: fmtSize(h.size) + " · " + sub, Size: 10, Font: "monospace", Color: col.Alpha(.75)}, h: 13},
	}
	if a.mode == modeSafety && h.node != scan.None {
		lv := a.ev.Verdict(h.node).Level()
		lc := pal.level[lv]
		lines = append(lines, calloutLine{sp: ui.Span{Text: T(levelTitles[lv]), Size: 10.5, Weight: 500, Color: lc}, h: 13, dot: lc, hasDot: true})
	}
	var want float32
	for i := range lines {
		w, _ := p.MeasureText(0, lines[i].sp)
		if lines[i].hasDot {
			w += 10
		}
		want = max(want, w)
	}
	want += 10
	g := placeCallout(s.cx, s.cy, re, am, r, want, lines)
	// clip to the room actually found (only when no angle had enough)
	if g.room < want {
		for i := range lines {
			mw := g.room - 10
			if lines[i].hasDot {
				mw -= 10
			}
			lines[i].sp.Text = clipSpan(p, lines[i].sp, max(mw, 24))
		}
	}
	shelf := min(want, g.room)

	x0, y0 := polar(s.cx, s.cy, h.r1, am)
	ex, ey := polar(s.cx, s.cy, re, g.b)
	var ln ui.Path
	ln.MoveTo(x0, y0)
	if g.b != am {
		rk := re - 14
		ln.LineTo(polar(s.cx, s.cy, rk, am))
		arcTo(&ln, s.cx, s.cy, rk, am, g.b)
	}
	ln.LineTo(ex, ey)
	x2, lx := ex+shelf, ex+5
	if !g.right {
		x2, lx = ex-shelf, ex-shelf+5
	}
	ln.LineTo(x2, ey)
	p.StrokePath(&ln, 1, col.Alpha(.85))
	p.FillPath((&ui.Path{}).Circle(x0, y0, 2.5), col)
	for i, top := range lineTops(ey, g.stack, lines) {
		l := &lines[i]
		x := lx
		if l.hasDot {
			p.FillPath((&ui.Path{}).Circle(x+3, top+7, 3), l.dot)
			x += 10
		}
		p.RichText(x, top, 0, l.sp)
	}
}

// calloutLine is one text row of the hover callout.
type calloutLine struct {
	sp     ui.Span
	h      float32
	dot    ui.Color
	hasDot bool
}

// Callout text arrangements around the shelf.
const (
	stackSplit = iota // first line above the shelf, the rest below
	stackAbove        // all lines above
	stackBelow        // all lines below
)

// lineTops returns the top y of each line for a shelf at y.
func lineTops(y float32, stack int, lines []calloutLine) []float32 {
	tops := make([]float32, len(lines))
	var total float32
	for _, l := range lines {
		total += l.h
	}
	switch stack {
	case stackAbove:
		t := y - 3 - total
		for i, l := range lines {
			tops[i], t = t, t+l.h
		}
	case stackBelow:
		t := y + 4
		for i, l := range lines {
			tops[i], t = t, t+l.h
		}
	default:
		tops[0] = y - 17
		t := y + 4
		for i := 1; i < len(lines); i++ {
			tops[i], t = t, t+lines[i].h
		}
	}
	return tops
}

// calloutGeom is where the callout label sits: anchor angle b on the label circle, the
// side its shelf runs to, how the text stacks and the horizontal room there.
type calloutGeom struct {
	b     float64
	right bool
	stack int
	room  float32
}

// placeCallout picks the anchor angle nearest am whose outward shelf of width want, and
// text block, fit inside box. Failing that it takes the angle with the most room.
func placeCallout(cx, cy, re float32, am float64, box ui.Rect, want float32, lines []calloutLine) calloutGeom {
	const margin = 6
	try := func(b float64) (calloutGeom, bool) {
		ex, ey := polar(cx, cy, re, b)
		g := calloutGeom{b: b, right: math.Cos(b) >= 0}
		if math.Abs(math.Cos(b)) < 1e-6 { // exactly vertical: follow the hovered side
			g.right = math.Cos(am) >= 0
		}
		if g.right {
			g.room = box.X + box.W - margin - ex
		} else {
			g.room = ex - (box.X + margin)
		}
		// a straight radial leader stays on the inner side of the text, so any stacking
		// is clear of it; a bent one keeps the text off the side the bend arrives from
		stacks := []int{stackSplit, stackBelow, stackAbove}
		if b != am {
			stacks = []int{stackBelow}
			if (b > am) != (math.Cos(b) > 0) {
				stacks[0] = stackAbove
			}
		}
		last := len(lines) - 1
		for _, st := range stacks {
			g.stack = st
			tops := lineTops(ey, st, lines)
			if tops[0] >= box.Y+2 && tops[last]+lines[last].h <= box.Y+box.H-2 {
				return g, true
			}
		}
		g.stack = stacks[0]
		return g, false
	}
	g, vert := try(am)
	if vert && g.room >= want {
		return g // fits as is: straight radial leader
	}
	best, bestOK := g, vert
	const step = math.Pi / 90 // 2°
	for k := 1; k <= 90; k++ {
		for _, sg := range [2]float64{1, -1} {
			c, ok := try(am + sg*float64(k)*step)
			if ok && c.room >= want {
				return c
			}
			if ok && (!bestOK || c.room > best.room) {
				best, bestOK = c, true
			}
		}
	}
	return best
}

// clipSpan shortens sp's text with an ellipsis until it measures at most maxW.
func clipSpan(p *ui.Painter, sp ui.Span, maxW float32) string {
	if w, _ := p.MeasureText(0, sp); w <= maxW {
		return sp.Text
	}
	r := []rune(sp.Text)
	for len(r) > 2 {
		r = r[:len(r)-1]
		sp2 := sp
		sp2.Text = string(r) + "…"
		if w, _ := p.MeasureText(0, sp2); w <= maxW {
			break
		}
	}
	return string(r) + "…"
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
		centerText(p, cx, cy, Tf("扫描失败：%s", firstLine(a.scanErr.Error())), 13, 500, pal.level[2], "")
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
	a.paintScanWire(p, cx, cy, rad, ang, ink)
	pr := a.prog
	centerText(p, cx, cy-14, fmtSize(pr.Bytes.Load()), 26, 600, pal.text, "")
	centerText(p, cx, cy+14, Tf("%s 个文件 · %s 个文件夹", fmtNum(pr.Files.Load()), fmtNum(pr.Dirs.Load())), 11, 400, pal.t3, "")
	a.paintScanPath(p, cx, cy+rad+44, min(r.W-60, 640), pr.Current(), ang)
	if a.pal.motion {
		p.AnimationFrameRate(30)
	}
	a.paintScreen(p, r)
}
