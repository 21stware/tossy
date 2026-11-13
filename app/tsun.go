package main

import (
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/fanfree/fanfree/engine/trash"
)

// The trash scene's chart: where the bin's contents came from. Items are re-rooted into a
// virtual tree by their original path; single-child folder chains collapse ("Users/me/Library").
// Folders keep a calm ramp, items carry their safety colour (same as their row dot and ball).

type tnode struct {
	name   string
	key    string // original path prefix: stable across rebuilds, for the morph
	size   int64
	cnt    int
	kids   []*tnode
	parent *tnode
	it     *trash.Item
	x0, x1 float64
	d      int
}

type tarc struct {
	arc
	n *tnode
}

type trashSun struct {
	sig   string // what the tree was built from
	root  *tnode
	pre   string // the collapsed common folder
	depth int
	arcs  []tarc
	paths []*ui.Path
	box   ui.Rect
	t0    time.Time // ring-by-ring growth on entering
	hl    *tnode    // under the pointer on the chart
	hlID  string    // trash row under the pointer in the list
	hot   bool      // centre hovered: the way back

	prev map[string][2]float32 // morph after a restore
	mT0  time.Time
}

func (a *app) trashSig(items []trash.Item) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString(it.ID)
		b.WriteByte(0)
	}
	return b.String()
}

func buildTrashTree(items []trash.Item) (*tnode, string, int) {
	root := &tnode{}
	for i := range items {
		it := &items[i]
		parts := strings.Split(filepath.ToSlash(it.Orig), "/")
		cur := root
		cur.size += it.Size
		cur.cnt++
		key := ""
		for j, part := range parts {
			if part == "" {
				continue
			}
			key += "/" + part
			leaf := j == len(parts)-1
			var c *tnode
			for _, k := range cur.kids {
				if k.key == key {
					c = k
					break
				}
			}
			if c == nil {
				c = &tnode{name: part, key: key, parent: cur}
				cur.kids = append(cur.kids, c)
			}
			if leaf {
				c.it = it
			}
			c.size += it.Size
			c.cnt++
			cur = c
		}
	}
	pre := ""
	depth := 0
	var fix func(n *tnode, d int, x0, x1 float64)
	fix = func(n *tnode, d int, x0, x1 float64) {
		for n.it == nil && len(n.kids) == 1 && n.kids[0].it == nil && len(n.kids[0].kids) > 0 {
			c := n.kids[0]
			if n == root {
				pre += "/" + c.name
			} else {
				n.name += "/" + c.name
				n.key = c.key
			}
			n.kids = c.kids
			for _, k := range n.kids {
				k.parent = n
			}
		}
		n.d, n.x0, n.x1 = d, x0, x1
		depth = max(depth, d)
		sort.SliceStable(n.kids, func(i, j int) bool { return n.kids[i].size > n.kids[j].size })
		x, k := x0, (x1-x0)/float64(max(n.size, 1))
		for _, c := range n.kids {
			fix(c, d+1, x, x+float64(c.size)*k)
			x += float64(c.size) * k
		}
	}
	fix(root, 0, 0, 1)
	return root, pre, min(5, depth)
}

// syncTrashSun rebuilds the tree when the bin changed; a change while showing eases over.
func (a *app) syncTrashSun() *trashSun {
	ts := &a.tsun
	items := a.bin.List()
	sig := a.trashSig(items)
	if sig == ts.sig && ts.root != nil {
		return ts
	}
	if ts.root != nil && len(ts.arcs) > 0 && a.motion() {
		ts.prev = make(map[string][2]float32, len(ts.arcs))
		for _, h := range ts.arcs {
			ts.prev[h.n.key] = [2]float32{h.a0, h.a1}
		}
		ts.mT0 = a.now
	}
	ts.sig = sig
	ts.root, ts.pre, ts.depth = buildTrashTree(items)
	ts.hl = nil
	return ts
}

func (a *app) enterTrashSun() {
	a.tsun.sig = "" // rebuild
	a.tsun.prev = nil
	a.tsun.t0 = a.now
	a.tsun.hl, a.tsun.hlID = nil, ""
}

func (ts *trashSun) under(n, anc *tnode) bool {
	for ; n != nil; n = n.parent {
		if n == anc {
			return true
		}
	}
	return false
}

// hasItem reports whether id sits under n.
func (n *tnode) hasItem(id string) bool {
	if n.it != nil && n.it.ID == id {
		return true
	}
	for _, k := range n.kids {
		if k.hasItem(id) {
			return true
		}
	}
	return false
}

// paintTrashSun lays out and paints the trash-origin chart in the disk chart's geometry.
func (a *app) paintTrashSun(p *ui.Painter, r ui.Rect) {
	pal, s := a.pal, &a.sun
	ts := a.syncTrashSun()
	now := p.Now()
	// the disk chart's frame: same centre, radius and hole
	s.cx, s.cy = r.X+r.W/2, r.Y+r.H/2
	pad := float32(30)
	if pal.instr != instrNone {
		pad = 62
	}
	s.rad = max(60, min(r.W, r.H)/2-pad)
	s.r0 = s.rad * .27
	ts.box = r

	k := 1.0
	if !ts.t0.IsZero() && a.motion() {
		k = float64(now.Sub(ts.t0)) / float64(700*time.Millisecond)
		if k >= 1 {
			ts.t0, k = time.Time{}, 1
		}
	}
	mk := 1.0
	if ts.prev != nil {
		mk = float64(now.Sub(ts.mT0)) / float64(480*time.Millisecond)
		if mk >= 1 {
			ts.prev, mk = nil, 1
		}
	}
	if k < 1 || mk < 1 {
		p.AnimationFrame()
	}
	grow := func(ring int) float32 {
		t := (k - float64(ring-1)*.1) / .45
		if t <= 0 {
			return 0
		}
		return float32(easeOut(min(1, t)))
	}

	D := max(3, ts.depth)
	rw := (s.rad - s.r0) / float32(D)
	gap, cr := pal.gap, pal.corner
	ts.arcs = ts.arcs[:0]
	np := 0
	newPath := func() *ui.Path {
		if np < len(ts.paths) {
			np++
			return ts.paths[np-1].Reset()
		}
		q := &ui.Path{}
		ts.paths = append(ts.paths, q)
		np++
		return q
	}
	ih, is, _ := toHSL(pal.ink)
	var rec func(n *tnode, idx int)
	rec = func(n *tnode, idx int) {
		if n.d > D {
			return
		}
		if n.d > 0 {
			g := grow(n.d)
			if g <= 0 {
				return
			}
			a0, a1 := float32(n.x0*tau), float32(n.x1*tau)
			if o, ok := ts.prev[n.key]; ok && mk < 1 {
				e := float32(ease(mk))
				a0, a1 = o[0]+(a0-o[0])*e, o[1]+(a1-o[1])*e
			} else if mk < 1 { // a newcomer opens from its middle
				m := (a0 + a1) / 2
				e := float32(ease(mk))
				a0, a1 = m+(a0-m)*e, m+(a1-m)*e
			}
			if a1-a0 < .004 {
				return
			}
			r0 := s.r0 + float32(n.d-1)*rw + gap
			r1 := r0 + (rw-gap)*g
			var H, S, L float32
			if n.it != nil {
				H, S, L = toHSL(pal.level[lvOf(n.it)])
				L += float32(idx&1)*2 - float32(n.d)*1.5
			} else {
				H, S = ih+float32(n.x0+n.x1)*14, is*.35
				if pal.dark {
					L = 24 + float32(n.d)*3 + float32(idx&1)*1.5
				} else {
					L = 84 - float32(n.d)*3 - float32(idx&1)*1.5
				}
			}
			padA := min(.8/r0, (a1-a0)*.2)
			h := tarc{arc{a0: a0, a1: a1, r0: r0, r1: r1, ring: n.d, h: H, s: S, l: L, leaf: n.it != nil, size: n.size,
				path: sector(newPath(), s.cx, s.cy, r0, r1, a0+padA, a1-padA, cr)}, n}
			h.label = g >= 1 && mk >= 1 && r1-r0 > 15 && (a1-a0)*(r0+r1)/2 > 54
			ts.arcs = append(ts.arcs, h)
		}
		for i, c := range n.kids {
			rec(c, i)
		}
	}
	if ts.root != nil {
		rec(ts.root, 0)
	}
	a.arcCount = len(ts.arcs)

	// the row hovered in the list lights its arc
	hl := ts.hl
	if hl == nil && ts.hlID != "" {
		for i := range ts.arcs {
			if n := ts.arcs[i].n; n.it != nil && n.it.ID == ts.hlID {
				hl = n
			}
		}
	}
	for i := range ts.arcs {
		h := &ts.arcs[i]
		al := float32(1)
		if hl != nil && !ts.under(h.n, hl) && !ts.under(hl, h.n) {
			al = .22
		}
		a.paintArc(p, &h.arc, h.h, h.s, h.l, al)
		if h.n == hl {
			p.StrokePath(h.path, 1.5, pal.text)
		}
	}
	for i := range ts.arcs {
		h := &ts.arcs[i]
		if !h.label {
			continue
		}
		am := float64(a0Top + (h.a0+h.a1)/2)
		rm := (h.r0 + h.r1) / 2
		flip := math.Sin(am) > .05
		two := h.r1-h.r0 >= 30
		up := float32(1)
		if flip {
			up = -1
		}
		rN, rS := rm, rm-up*7
		if two {
			rN = rm + up*6
		}
		maxW := (h.a1-h.a0)*min(rN, rm) - 14
		if maxW < 34 {
			continue
		}
		parts := strings.Split(h.n.name, "/")
		if len(parts) > 2 {
			parts = parts[len(parts)-2:]
		}
		name := clipText(p, strings.Join(parts, "/"), maxW, 11, 500)
		if len([]rune(name)) < 3 {
			continue
		}
		al := float32(1)
		if hl != nil && !ts.under(h.n, hl) && !ts.under(hl, h.n) {
			al = .35
		}
		col, sc := pal.labelText.Alpha(al), pal.labelText.Alpha(.6*al)
		if !pal.holo() {
			if luma(hsl(h.h, h.s, h.l, 1)) > .5 {
				col, sc = ui.RGBA(15, 15, 18, .85*al), ui.RGBA(15, 15, 18, .55*al)
			} else {
				col, sc = ui.RGBA(255, 255, 255, .95*al), ui.RGBA(255, 255, 255, .7*al)
			}
		}
		arcText(p, name, s.cx, s.cy, rN, am, flip, 11, 500, col, pal.font)
		if two {
			arcText(p, fmtSize(h.size), s.cx, s.cy, rS, am, flip, 9.5, 400, sc, "monospace")
		}
	}

	// centre: the bin's total, or what is hovered; hovering the centre offers the way back
	ca := float32(min(1, k*3))
	if ts.hot {
		var c ui.Path
		p.FillPath(circle(&c, s.cx, s.cy, s.r0-2, 0, tau), pal.ink.Alpha(.035))
	}
	info := ts.root
	if hl != nil {
		info = hl
	}
	name, size, sub := trashName(), "空", "0 项"
	if info != nil && info.cnt > 0 {
		size, sub = fmtSize(info.size), fmtNum(int64(info.cnt))+" 项"
		if info != ts.root {
			parts := strings.Split(info.name, "/")
			name = parts[len(parts)-1]
		}
		if info.it != nil {
			sub = levelTitles[lvOf(info.it)]
		}
	}
	if ts.hot {
		sub = "← 返回磁盘"
	}
	fs, big := max(11, s.r0*.11), max(18, s.r0*.25)
	centerText(p, s.cx, s.cy-s.r0*.28, clipText(p, name, s.r0*1.5, fs, 400), fs, 400, pal.t3.Alpha(ca), "")
	centerText(p, s.cx, s.cy+s.r0*.02, size, big, 600, pal.text.Alpha(ca), "")
	sc := pal.t3
	if ts.hot {
		sc = pal.text
	}
	centerText(p, s.cx, s.cy+s.r0*.28, sub, max(10, s.r0*.1), 400, sc.Alpha(ca), "")
}

// trashChartInput: hover lights arcs and their rows; the centre leads back to the disk.
func (a *app) trashChartInput(chart ui.Element) {
	ts := &a.tsun
	ts.hl, ts.hot = nil, false
	x, y, over := chart.PointerPosition()
	if !over || ts.root == nil {
		return
	}
	b := chart.Bounds()
	s := &a.sun
	dx, dy := float64(b.X+x-s.cx), float64(b.Y+y-s.cy)
	r := float32(math.Hypot(dx, dy))
	out := r > s.rad+24
	if r < s.r0-3 {
		ts.hot = true
	} else {
		ang := math.Atan2(dy, dx) - a0Top
		for ang < 0 {
			ang += tau
		}
		for ang >= tau {
			ang -= tau
		}
		for i := len(ts.arcs) - 1; i >= 0; i-- {
			h := &ts.arcs[i]
			if r >= h.r0-1 && r < h.r1 && float32(ang) >= h.a0 && float32(ang) < h.a1 {
				ts.hl = h.n
				break
			}
		}
	}
	if ts.hot || ts.hl != nil {
		chart.Cursor(ui.CursorPointer)
	}
	if ts.hl != nil {
		chart.Tooltip(ts.hl.name + "\n" + fmtSize(ts.hl.size))
	}
	if out {
		chart.Tooltip("返回磁盘")
	}
	if chart.Clicked() && (ts.hot || (out && ts.hl == nil)) {
		a.goScene(sceneBrowse)
	}
}

// trashLegend: where the bin's contents came from, in the chart's bottom-left corner.
func (a *app) trashLegend(c *ui.Context) {
	pal := a.pal
	ts := a.syncTrashSun()
	items := a.bin.List()
	var total int64
	for _, it := range items {
		total += it.Size
	}
	ui.Column(c.Key("tlegend")).Absolute().Left(18).Bottom(10).Gap(5).MaxWidth(320).PassThrough().Children(func() {
		ui.Text(c, trashName()+"里的内容来自哪里").FontSize(12).FontWeight(600).TextColor(pal.t2)
		sub := "空"
		if len(items) > 0 {
			sub = fmtNum(int64(len(items))) + " 项 · " + fmtSize(total)
			if ts.pre != "" {
				sub += " · 都在 " + displayHome(filepath.FromSlash(ts.pre)) + " 下"
			}
		}
		ui.Text(c, sub).FontSize(11).TextColor(pal.t4).SingleLine().TruncateMiddle()
		if ts.root == nil {
			return
		}
		ih, is, _ := toHSL(pal.ink)
		for i, k := range ts.root.kids {
			if i == 5 {
				ui.Text(c, fmtNum(int64(len(ts.root.kids)-5))+" 个其他位置").FontSize(11).TextColor(pal.t4)
				break
			}
			col := hsl(ih+float32(k.x0+k.x1)*14, is*.35, 40, 1)
			if k.it != nil {
				col = pal.level[lvOf(k.it)]
			}
			parts := strings.Split(k.name, "/")
			ui.Row(c.Key(k.key)).Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(8, 8).Radius(2).Background(col)
				ui.Text(c, parts[len(parts)-1]).FontSize(11.5).TextColor(pal.t2).SingleLine().Ellipsis("…").Shrink(1)
				ui.Text(c, fmtSize(k.size)).FontSize(11).Font(pal.mono).TextColor(pal.t4)
			})
		}
	})
}

func lvOf(it *trash.Item) int { return max(0, min(3, it.Level)) }
