package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// The truck draws without coverage masks (truckfx.go): no paths, no path clips, nothing that
// draws through one (paintBall clips a path, bubblePath is one).
func TestTruckDrawsWithoutPaths(t *testing.T) {
	banned := map[string]bool{
		"FillPath": true, "FillPathGradient": true, "FillPathConicGradient": true,
		"StrokePath": true, "StrokePathGradient": true, "StrokePathConicGradient": true,
		"ClipPath": true, "Path": true, "penPath": true, "paintBall": true, "bubblePath": true, "svgPath": true,
	}
	fset := token.NewFileSet()
	for _, name := range []string{"truck.go", "truckfx.go"} {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			var id string
			switch n := n.(type) {
			case *ast.SelectorExpr:
				id = n.Sel.Name
			case *ast.Ident:
				id = n.Name
			}
			if banned[id] {
				t.Errorf("%s: %s draws through a mask", fset.Position(n.Pos()), id)
			}
			return true
		})
	}
}

// TestTruckEndsWithoutInput empties the trash and then leaves the pointer alone: the vehicle
// runs in the paint, whose frames do not build the view, yet the trash view must follow the
// deletion and go back to the disk once the vehicle has gone.
func TestTruckEndsWithoutInput(t *testing.T) {
	a := loaded(t)
	a.set.InUseMin = 0
	tt := ui.NewTester(a.view, 1280, 820)
	tt.Advance(2 * time.Second)
	tt.Frame()

	n := int32(-1)
	for i := range a.sun.arcs {
		if ar := &a.sun.arcs[i]; ar.node > 0 && a.t.Nodes[ar.node].IsDir() && a.t.Name(ar.node) == "node_modules" {
			n = ar.node
		}
	}
	if n < 0 {
		t.Fatal("no node_modules sector")
	}
	a.trashOne(n)
	for deadline := time.Now().Add(5 * time.Second); a.trashMoving > 0; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the mark never landed")
		}
	}
	tt.Frame()
	a.goScene(sceneTrash)
	tt.Frame()
	tt.Advance(2 * time.Second)
	tt.Frame()
	if len(a.bin.List()) != 1 || a.binS.heroK < .99 {
		t.Fatalf("trash view not up: %d items, hero %.2f", len(a.bin.List()), a.binS.heroK)
	}

	a.emptyTrash(nil)
	if a.binS.truck == nil {
		t.Fatal("no vehicle came")
	}
	tt.Frame() // the click's frame; from here on only the clock moves
	for i := 0; i < 400 && a.scene != sceneBrowse; i++ {
		time.Sleep(time.Millisecond) // the deletion runs on its own goroutine
		tt.Advance(50 * time.Millisecond)
		if _, row := tt.Find(T("还原")); row && a.binS.truck == nil && len(a.bin.List()) == 0 { // a row's button
			t.Fatal("the vehicle has gone and the trash view still lists what it took")
		}
	}
	if a.scene != sceneBrowse {
		t.Fatalf("still in the trash view: vehicle gone %v, emptying %v, %d timers waiting", a.binS.truck == nil, a.emptying, len(a.timers))
	}
	if a.emptying || len(a.bin.List()) != 0 {
		t.Errorf("emptying %v, %d items left", a.emptying, len(a.bin.List()))
	}
}
