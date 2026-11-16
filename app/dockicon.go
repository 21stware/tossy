package main

import (
	"bytes"
	"image/png"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The Dock icon follows the style: the waste basket (the app icon's bin) in the style's ink, on a
// tile of the style's ground, drawn by the same code as the dock bin. The bundle keeps the mint
// icon of resources/icon.png for Finder and the Dock before launch.

// iconPx is the icon's size in pixels, as macOS asks of an app icon.
const iconPx = 1024

// iconTile is the macOS icon grid: an 824-pixel tile inset 100 in the 1024 canvas, its corner
// ~185 pixels.
const iconInset, iconRadius = 100, 185

var dockIcons struct {
	sync.Mutex
	png  map[string][]byte // by style name
	want string            // the style last asked for: a slow render of an older one does not win
}

// syncDockIcon runs every frame: on a change of style it sets the Dock icon to pal's bin, drawn
// off the frame.
func (a *app) syncDockIcon(pal *palette) {
	if a.win == nil || underTest() {
		return
	}
	dockIcons.Lock()
	if dockIcons.want == pal.name {
		dockIcons.Unlock()
		return
	}
	dockIcons.want = pal.name
	b := dockIcons.png[pal.name]
	dockIcons.Unlock()
	go func() {
		if b == nil {
			b = renderDockIcon(pal)
			dockIcons.Lock()
			if dockIcons.png == nil {
				dockIcons.png = map[string][]byte{}
			}
			dockIcons.png[pal.name] = b
			stale := dockIcons.want != pal.name
			dockIcons.Unlock()
			if stale {
				return
			}
		}
		if b != nil {
			mygo.App.Dock.SetIcon(b)
		}
	}()
}

// renderDockIcon draws pal's icon as a PNG.
func renderDockIcon(pal *palette) []byte {
	const s = 2 // pixels per DIP
	const W = iconPx / s
	img := ui.Render(func(c *ui.Context) {
		th := *pal.theme()
		th.Background = ui.Transparent // the window's fill: around the tile the icon is clear
		c.SetTheme(&th)
		ui.Box(c).Size(W, W).Draw(func(p *ui.Painter, r ui.Rect) { paintIcon(p, pal, W) })
	}, W, W, s)
	var buf bytes.Buffer
	if png.Encode(&buf, img) != nil {
		return nil
	}
	return buf.Bytes()
}

// paintIcon draws the tile and the bin into a W×W square.
func paintIcon(p *ui.Painter, pal *palette, W float32) {
	k := W / iconPx
	tile := ui.Rect{X: iconInset * k, Y: iconInset * k, W: (iconPx - 2*iconInset) * k, H: (iconPx - 2*iconInset) * k}
	rad := iconRadius * k
	white, black := ui.RGB(255, 255, 255), ui.RGB(0, 0, 0)

	// the tile: the style's ground, lit from above, on a soft drop shadow
	p.Shadow(tile, rad, 0, 12*k, 28*k, 0, ui.RGBA(0, 0, 0, .3))
	top, bot := pal.panel2.Mix(white, .04), pal.bg.Mix(black, .12)
	if !pal.dark {
		top, bot = pal.bg.Mix(white, .5), pal.bg.Mix(black, .04)
	}
	p.FillGradient(tile, ui.LinearGradient{Angle: 180, From: top, To: bot}, rad)
	p.FillRadialGradient(tile, ui.RadialGradient{
		Radius: tile.W * .58,
		Stops:  []ui.GradientStop{{Color: white.Alpha(.1), At: 0}, {Color: white.Alpha(0), At: 1}},
	}, rad)
	p.Stroke(tile, white.Alpha(.12), rad, 2*k)

	// the bin: the dock's waste basket, zoomed as the hero bin is, its foot below the middle
	ic := &app{pal: pal, now: time.Now()}
	f := &ic.binS
	f.tier, f.prevTier = 1, 1
	T := &binTiers[1]
	f.right, f.bottom = T.w, T.h
	f.heroK, f.heroX = 1, 1
	f.heroS = 560 * k / T.w
	f.heroTX, f.heroTY = W/2, W/2+T.h*f.heroS/2+18*k
	ic.paintBinBack(p, nil)
	ic.paintBinFront(p)
}
