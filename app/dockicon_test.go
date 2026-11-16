package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestDockIcons renders every style's Dock icon; TOSSY_ICONS=dir writes them out to look at.
func TestDockIcons(t *testing.T) {
	out := os.Getenv("TOSSY_ICONS")
	for _, pal := range palettes {
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
		if out != "" {
			os.WriteFile(filepath.Join(out, pal.name+".png"), b, 0o644)
		}
	}
}
