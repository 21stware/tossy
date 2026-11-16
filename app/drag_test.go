package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// dragTo presses at (x0, y0) and moves the pointer to (x1, y1) in steps, then lets go.
func dragTo(tt *ui.Tester, x0, y0, x1, y1 float32) {
	tt.Move(x0, y0)
	tt.Press(x0, y0)
	const steps = 12
	for i := 1; i <= steps; i++ {
		tt.Advance(10 * time.Millisecond)
		k := float32(i) / steps
		tt.Move(x0+(x1-x0)*k, y0+(y1-y0)*k)
	}
	tt.Release(x1, y1)
	tt.Frame()
}

// TestDragSectorToBin drags a sector of the chart onto the bin, and one that is let go short
// of it, which neither trashes nor drills into it.
func TestDragSectorToBin(t *testing.T) {
	a := loaded(t)
	a.set.InUseMin = 0
	tt := ui.NewTester(a.view, 1280, 820)
	tt.Advance(2 * time.Second)
	tt.Frame()

	var h *arc
	for i := range a.sun.arcs {
		ar := &a.sun.arcs[i]
		if ar.node > 0 && a.t.Nodes[ar.node].IsDir() && a.t.Name(ar.node) == "node_modules" {
			h = ar
		}
	}
	if h == nil {
		t.Fatal("no node_modules sector")
	}
	n := h.node
	if ok, why := a.canTrash(n); !ok || a.asksFirst(n) {
		t.Fatalf("node_modules not trashable at once: %v", why)
	}
	x, y := a.arcPointOf(h)

	// let go on the chart: the ball goes back, nothing is trashed, no drill-down
	focus := a.focus
	dragTo(tt, x, y, x+40, y+30)
	tt.Advance(time.Second)
	tt.Frame()
	if !a.t.Attached(n) || a.focus != focus || a.drag.press {
		t.Fatalf("drag let go on the chart: attached %v, focus %d (was %d), press %v",
			a.t.Attached(n), a.focus, focus, a.drag.press)
	}

	// onto the bin
	f := &a.binS
	T := &binTiers[f.tier]
	ox, oy := f.origin()
	bx, by := f.toHero(ox+T.w/2, oy+T.h/2)
	x, y = a.arcPointOf(h)
	dragTo(tt, x, y, bx, by)
	tt.Advance(2 * time.Second)
	tt.Frame()
	if a.t.Attached(n) {
		t.Fatal("node_modules still attached after dropping its sector on the bin")
	}
	if len(a.bin.List()) != 1 {
		t.Errorf("trash has %d entries, want 1", len(a.bin.List()))
	}
	if a.drag.on || a.binS.hot {
		t.Errorf("drag still on after the drop: on %v, bin hot %v", a.drag.on, a.binS.hot)
	}
}
