package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
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
