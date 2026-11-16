package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/draw"
)

// TestDockIcons renders every style's Dock icon. TOSSY_ICONS=dir writes dock-icons.png, all of
// them in a row, which the README shows:
//
//	TOSSY_ICONS=$PWD/../docs go test -run TestDockIcons .
func TestDockIcons(t *testing.T) {
	out := os.Getenv("TOSSY_ICONS")
	const cell = 160
	strip := image.NewNRGBA(image.Rect(0, 0, cell*len(palettes), cell))
	for i, pal := range palettes {
		b := renderDockIcon(pal)
		if len(b) == 0 {
			t.Fatalf("%s: no icon", pal.name)
		}
		m, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, a := m.At(2, 2).RGBA(); a != 0 {
			t.Errorf("%s: the corner outside the tile is not clear", pal.name)
		}
		draw.CatmullRom.Scale(strip, image.Rect(i*cell, 0, (i+1)*cell, cell), m, m.Bounds(), draw.Over, nil)
	}
	if out != "" {
		var buf bytes.Buffer
		if err := png.Encode(&buf, strip); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "dock-icons.png"), buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
