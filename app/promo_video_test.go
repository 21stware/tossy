package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/21stware/tossy/engine/safety"
	"github.com/21stware/tossy/engine/scan"
	"github.com/21stware/tossy/engine/trash"
	"github.com/21stware/tossy/tools/demo/score"
)

// TestPromoVideo renders the promo film (TOSSY_PROMO_VIDEO=dir): some forty seconds, cut like a
// trailer around what Tossy is for, a picture to a point. Its first frame, and no more than
// that, is its cover: the chart, the craft and the name. It opens on its best picture,
// the craft that comes when the trash is emptied, in one take; then a full disk and what filled
// it; then the four things to remember, each on the app itself: see it all (the scan and the
// chart), know what is safe (the verdicts, and what git says), one click (Auto Clean and what
// it cleared), undo anything (the trash); then the trash is emptied and the three vehicles
// that come for it, a bigger one for a bigger bin; then the free space, the ten styles and the
// name.
//
// The film cuts on the beat, 120 a minute, and keeps a cue sheet of where its sections start
// and which cuts land; tools/demo/score writes the music to it, and tools/demo/mux.swift lays
// the track under the picture.
//
// Every picture of the app is a frame the app drew, over the home folder of the README's
// screenshots, on the Tester's clock; there is no pointer and no window. The test is the
// film's editor: it works out each frame's camera, type and light, and tools/demo/film.swift
// draws and encodes them (macOS, needs swiftc).
//
//		TOSSY_PROMO_VIDEO=$PWD/../dist/demo go test -run TestPromoVideo -timeout 30m .
//
//	  - TOSSY_PROMO_LANG picks the language (en by default; zh-Hans is written too, and not
//	    released) and names the file tossy-promo-<lang>.mp4.
//	  - TOSSY_PROMO_FPS sets the frame rate (60), TOSSY_PROMO_SIZE the canvas' width (1920).
//	  - TOSSY_PROMO_DRAFT=1 renders quickly at 30 fps and half the detail, to look a cut over.
//	  - TOSSY_PROMO_SHEETS=dir also writes contact sheets, a frame every half second, and
//	    TOSSY_PROMO_STILLS=12.5,40 the frames at those seconds.
//	  - TOSSY_PROMO_COVERS=dir renders no film: it writes the cover in each of the app's styles
//	    there, tossy-cover-<style>.png.
func TestPromoVideo(t *testing.T) {
	out := os.Getenv("TOSSY_PROMO_VIDEO")
	if out == "" {
		t.Skip("set TOSSY_PROMO_VIDEO")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	code := os.Getenv("TOSSY_PROMO_LANG")
	if code == "" {
		code = "en"
	}
	draft := os.Getenv("TOSSY_PROMO_DRAFT") != ""
	covers := os.Getenv("TOSSY_PROMO_COVERS")
	fps, width := 60, 1920
	if draft {
		fps = 30
	}
	if v, err := strconv.Atoi(os.Getenv("TOSSY_PROMO_FPS")); err == nil && v > 0 {
		fps = v
	}
	if v, err := strconv.Atoi(os.Getenv("TOSSY_PROMO_SIZE")); err == nil && v > 0 {
		width = v
	}
	if covers != "" {
		draft, fps = false, 2 // a frame a cover, and a still of each
	}
	t.Setenv("TOSSY_LANG", code)
	root := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", root) // the root shows as ~
	readmeHome(t, root)

	// the tree, scanned ahead: the scan on screen is played from it
	fresh := func() *scan.Tree {
		tree, err := scan.Scan(context.Background(), root, scan.Options{})
		if err != nil {
			t.Fatal(err)
		}
		inflate(tree, 64<<10) // 1 KB on disk reads as 64 MB
		return tree
	}
	f := &film{t: t, root: root, tree: fresh(), fresh: fresh, zh: strings.HasPrefix(code, "zh"), fps: fps,
		k: float64(width) / 1920, draft: draft, free0: 21e9, hitAt: -1 << 20, rate: 1, covers: covers != ""}
	// an app with a trash and settings of its own
	f.mk = func() *app {
		t.Setenv("TOSSY_STATE", t.TempDir())
		a := newApp()
		a.palIdx, a.pal = f.style, palettes[f.style]
		a.set.InUseMin = 0 // the fixture was all just written
		a.set.SkipApps = false
		return a
	}
	// a disk that is nearly full; what is emptied shows as free once the film says so
	diskUsageOf = func(string) (int64, int64, bool) { return 500e9, int64(f.free0 + f.freed), true }
	defer func() { diskUsageOf = diskUsage }()

	wd, _ := os.Getwd()
	tools := filepath.Join(wd, "../tools/demo")
	tmp := t.TempDir()
	for _, name := range []string{"film", "mux"} {
		if b, err := exec.Command("swiftc", "-O", "-suppress-warnings", "-o", filepath.Join(tmp, name), filepath.Join(tools, name+".swift")).CombinedOutput(); err != nil {
			t.Fatalf("swiftc %s: %v\n%s", name, err, b)
		}
	}
	file := filepath.Join(out, "tossy-promo-"+code+".mp4")
	silent := filepath.Join(tmp, "silent.mp4")
	args := []string{silent, fmt.Sprint(width), fmt.Sprint(width * 9 / 16), fmt.Sprint(fps), filepath.Join(wd, "resources/icon.png")}
	if dir := os.Getenv("TOSSY_PROMO_SHEETS"); dir != "" && covers == "" {
		os.MkdirAll(dir, 0o755)
		args = append(args, dir, os.Getenv("TOSSY_PROMO_STILLS"))
	}
	if covers != "" {
		var at []string
		for i := range coverStyles() {
			at = append(at, fmt.Sprint(float64(i)/float64(fps)))
		}
		args = append(args, tmp, strings.Join(at, ","))
	}
	cmd := exec.Command(filepath.Join(tmp, "film"), args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	pipe, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	f.w = bufio.NewWriterSize(pipe, 8<<20)
	began := time.Now()
	f.play()
	if err := f.w.Flush(); err != nil {
		t.Fatal(err)
	}
	pipe.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if covers != "" {
		if err := os.MkdirAll(covers, 0o755); err != nil {
			t.Fatal(err)
		}
		for i, k := range coverStyles() {
			p := palettes[k]
			b, err := os.ReadFile(filepath.Join(tmp, fmt.Sprintf("still-%05.1f.png", float64(i)/float64(fps))))
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(covers, "tossy-cover-"+p.name+".png")
			if err := os.WriteFile(name, b, 0o644); err != nil {
				t.Fatal(err)
			}
			t.Log(name)
		}
		return
	}
	// the music, written to the film's cue sheet, and laid under the picture
	f.cues.End = f.now()
	wav := filepath.Join(tmp, "track.wav")
	l, r := score.Render(f.cues)
	if err := score.WriteWAV(wav, l, r); err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("TOSSY_PROMO_SHEETS"); dir != "" {
		b, _ := json.MarshalIndent(f.cues, "", " ")
		os.WriteFile(filepath.Join(dir, "cues.json"), b, 0o644)
		score.WriteWAV(filepath.Join(dir, "track.wav"), l, r)
	}
	if b, err := exec.Command(filepath.Join(tmp, "mux"), silent, wav, file).CombinedOutput(); err != nil {
		t.Fatalf("mux: %v\n%s", err, b)
	}
	t.Logf("%d frames (%.1fs, %d drawn again on time) in %v: %s", f.n, f.now(), f.redrawn, time.Since(began).Round(time.Second), file)
}

// The window the app draws in, in points, and the film's colours.
const filmW, filmH = 1280, 800

var (
	filmInk   = []float64{.95, .97, .97, 1}                // type
	filmGrey  = []float64{.95, .97, .97, .6}               // type, second voice
	filmFaint = []float64{.95, .97, .97, .34}              // type, third
	filmCyan  = []float64{.5, .91, .95, 1}                 // the HUD's ink
	filmAmber = []float64{1, .73, .36, 1}                  // the HUD's amber
	filmRed   = []float64{1, .36, .33, 1}                  // a full disk
	filmNight = []float64{.02, .027, .033}                 // the cards' ground
	filmHUD   = []float64{13. / 255, 16. / 255, 19. / 255} // the app's ground: its frames end without a seam
)

// fl is a layer for the compositor (Layer in film.swift): measures are on the 1920×1080 canvas.
type fl struct {
	K      string    `json:"k"`
	X      float64   `json:"x"`
	Y      float64   `json:"y"`
	W      float64   `json:"w,omitempty"`
	H      float64   `json:"h,omitempty"`
	R      float64   `json:"r,omitempty"`
	A      float64   `json:"a"`
	C      []float64 `json:"c,omitempty"`
	Under  bool      `json:"under,omitempty"`
	Z      float64   `json:"z,omitempty"`
	Quad   []float64 `json:"quad,omitempty"`
	Bloom  float64   `json:"bloom,omitempty"`
	S      string    `json:"s,omitempty"`
	Size   float64   `json:"size,omitempty"`
	Wt     float64   `json:"wt,omitempty"`
	Mono   bool      `json:"mono,omitempty"`
	Digits bool      `json:"digits,omitempty"`
	Align  string    `json:"align,omitempty"`
	Track  float64   `json:"track,omitempty"`
	Rev    *float64  `json:"rev,omitempty"`
	Side   string    `json:"side,omitempty"`
}

// txt is a line of type with its baseline at y.
func txt(s string, x, y, size float64) fl {
	return fl{K: "text", S: s, X: x, Y: y, Size: size, A: 1, C: filmInk, Wt: .3}
}
func (l fl) al(a string) fl      { l.Align = a; return l }
func (l fl) col(c []float64) fl  { l.C = c; return l }
func (l fl) wt(w float64) fl     { l.Wt = w; return l }
func (l fl) tr(t float64) fl     { l.Track = t; return l }
func (l fl) alpha(a float64) fl  { l.A *= a; return l }
func (l fl) mono() fl            { l.Mono, l.Wt = true, .1; return l }
func (l fl) digits() fl          { l.Digits = true; return l }
func (l fl) reveal(k float64) fl { k = clamp(k); l.Rev = &k; return l }

func rect(x, y, w, h, r float64, c []float64, a float64) fl {
	return fl{K: "rect", X: x, Y: y, W: w, H: h, R: r, C: c, A: a}
}

// scrim is a wash of the app's ground, opaque at side and clear at the other end.
func scrim(side string, x, y, w, h, a float64) fl {
	return fl{K: "scrim", Side: side, X: x, Y: y, W: w, H: h, C: filmHUD, A: a}
}

// cover gives a side of the canvas to the type: the app's ground from that side to edge, then
// a wash that clears over the next soft pixels.
func cover(side string, edge, soft, a float64) []fl {
	switch side {
	case "l":
		return []fl{rect(0, 0, edge, 1080, 0, filmHUD, a), scrim("l", edge, 0, soft, 1080, a)}
	case "r":
		return []fl{rect(edge, 0, 1920-edge, 1080, 0, filmHUD, a), scrim("r", edge-soft, 0, soft, 1080, a)}
	case "t":
		return []fl{rect(0, 0, 1920, edge, 0, filmHUD, a), scrim("t", 0, edge, 1920, soft, a)}
	}
	return []fl{rect(0, edge, 1920, 1080-edge, 0, filmHUD, a), scrim("b", 0, edge-soft, 1920, soft, a)}
}

func clamp(k float64) float64      { return max(0, min(1, k)) }
func lerp(a, b, k float64) float64 { return a + (b-a)*k }
func outExpo(k float64) float64    { return (1 - math.Pow(2, -10*clamp(k))) / (1 - 1./1024) }

// span is how far t is through the dur seconds from t0, 0 to 1.
func span(t, t0, dur float64) float64 { return clamp((t - t0) / dur) }

// cam is the camera: it puts the window's point (cx, cy) at (ox, oy) on the canvas, z canvas
// pixels to a point, and turns the window about that point by rx and ry degrees. slab rounds
// the window's corners, for the shots that show its edges.
type cam struct {
	cx, cy, z, ox, oy float64
	rx, ry            float64
	slab              float64
}

// at is the camera on (cx, cy), z close, with that point at the middle of the canvas.
func at(cx, cy, z float64) cam { return cam{cx: cx, cy: cy, z: z, ox: 960, oy: 540} }

// to puts the point the camera looks at somewhere else on the canvas.
func (c cam) to(ox, oy float64) cam { c.ox, c.oy = ox, oy; return c }

func (c cam) mix(d cam, k float64) cam {
	return cam{lerp(c.cx, d.cx, k), lerp(c.cy, d.cy, k), math.Exp(lerp(math.Log(c.z), math.Log(d.z), k)),
		lerp(c.ox, d.ox, k), lerp(c.oy, d.oy, k), lerp(c.rx, d.rx, k), lerp(c.ry, d.ry, k), lerp(c.slab, d.slab, k)}
}

// film plays the storyboard and sends each frame on.
type film struct {
	t      *testing.T
	a      *app
	tt     *ui.Tester
	root   string
	tree   *scan.Tree
	fresh  func() *scan.Tree // the tree again, for an app that changes its own
	mk     func() *app
	cues   score.Cues
	zh     bool
	style  int  // the style the film's apps are made in: the first, but for a cover
	covers bool // the covers only, one in each style
	fps    int
	k      float64 // the canvas' width to 1920
	draft  bool
	w      *bufio.Writer

	free0, freed float64 // the disk's free space as the film begins, and what the film has freed

	n       int     // frames sent
	redrawn int     // and those drawn again to be on the film's time (step)
	st, sp  float64 // seconds into the shot, this frame and the last
	rate    float64 // the app's seconds to one of the film's: 1, or more where the film hurries it

	// this frame
	v     cam
	win   bool // the app is on screen
	bloom float64
	vig   float64
	dim   float64
	bg    []float64
	over  []fl // layers over the window
	under []fl // and under it
	hitAt int  // the frame of the last flash
	hitK  float64

	plate  *image.RGBA // a picture the film made itself, shown in the window's place: 1920 points wide
	titles []filmTitle
	crop   []byte
	scales map[*ui.Tester]float64 // what each Tester draws at
}

// stage is an app and the Tester that plays it. The film has one for its story and others
// beside it, each with a trash of its own, for the vehicles.
type stage struct {
	a  *app
	tt *ui.Tester
}

// enter makes s the stage the film shoots.
func (f *film) enter(s *stage) { f.a, f.tt = s.a, s.tt }

// filmTitle is a shot's type: it comes in at t0, over in seconds, and goes with the shot, at the cut.
type filmTitle struct {
	l      fl
	t0, in float64
	reveal bool
}

func (f *film) L(en, zh string) string {
	if f.zh {
		return zh
	}
	return en
}

// N is L for a measure: type sizes differ, as Chinese says in three characters what English
// says in three words.
func (f *film) N(en, zh float64) float64 {
	if f.zh {
		return zh
	}
	return en
}

func (f *film) now() float64           { return float64(f.n) / float64(f.fps) }
func (f *film) frames(sec float64) int { return max(1, int(math.Round(sec*float64(f.fps)))) }
func (f *film) put(l ...fl)            { f.over = append(f.over, l...) }
func (f *film) on(x float64) bool      { return f.sp < x && x <= f.st }

// hit lands a cut: a flash on the picture, and a hit in the music.
func (f *film) hit(k float64) {
	f.hitAt, f.hitK = f.n, k
	f.cues.Hits = append(f.cues.Hits, score.Hit{T: f.now(), K: k})
}

// cue starts a section of the music with the next frame.
func (f *film) cue(name string) {
	if f.n%f.frames(2) != 0 {
		f.t.Fatalf("%s starts at %.2fs, off the bar: the music keeps one grid", name, f.now())
	}
	f.cues.Sections = append(f.cues.Sections, score.Section{Name: name, T: f.now()})
}

// flash is hit for a light cut: the picture only.
func (f *film) flash(k float64) { f.hit(min(k, .14)) }
func (f *film) look(c cam)      { f.v, f.win = c, true }

// title sets type on the shot that comes next, in after delay seconds: a headline's letters rise
// into the line, anything else fades up.
func (f *film) title(delay float64, reveal bool, l fl) {
	in := .4
	if reveal {
		in = .55
	}
	f.titles = append(f.titles, filmTitle{l: l, t0: f.now() + delay, in: in, reveal: reveal})
}

// block is a headline with its number over it and a line under it, from (x, y).
func (f *film) block(delay, x, y, size float64, align, idx, head, sub string) {
	tr := -size * .025
	if f.zh {
		tr = size * .03
	}
	if idx != "" {
		f.title(delay, false, txt(idx, x, y-size*1.22, 26).mono().col(filmCyan).tr(5).al(align))
	}
	f.title(delay+.06, true, txt(head, x, y, size).wt(.56).tr(tr).al(align))
	if sub != "" {
		f.title(delay+.3, false, txt(sub, x, y+size*.3+48, 40).col(filmGrey).wt(.1).al(align))
	}
}

// emit sends the frame.
func (f *film) emit() {
	T := f.now()
	layers := append([]fl(nil), f.under...)
	for i := range layers {
		layers[i].Under = true
	}
	type meta struct {
		W      int       `json:"w,omitempty"`
		H      int       `json:"h,omitempty"`
		SX     int       `json:"sx"`
		SY     int       `json:"sy"`
		FW     int       `json:"fw,omitempty"`
		FH     int       `json:"fh,omitempty"`
		Scale  float64   `json:"scale,omitempty"`
		BG     []float64 `json:"bg"`
		Layers []fl      `json:"layers"`
		Flash  float64   `json:"flash,omitempty"`
		Dim    float64   `json:"dim,omitempty"`
		Vig    float64   `json:"vig,omitempty"`
	}
	m := meta{BG: f.bg, Dim: f.dim, Vig: f.vig}
	var pix []byte
	if f.win && (f.tt != nil || f.plate != nil) {
		img, pts := f.plate, 1920.
		if img == nil {
			img, pts = f.tt.Image(), filmW
		}
		fw, fh := img.Rect.Dx(), img.Rect.Dy()
		s := float64(fw) / pts
		c := f.v
		wl := fl{K: "win", A: 1, Z: c.z, Bloom: f.bloom, R: c.slab}
		x0, y0, x1, y1 := 0, 0, fw, fh
		if c.rx == 0 && c.ry == 0 {
			wl.X, wl.Y = c.ox-c.cx*c.z, c.oy-c.cy*c.z
			// only what the canvas shows of the frame, and a margin for the bloom
			const pad = 28
			x0 = max(0, int(math.Floor(((0-wl.X)/c.z-pad)*s)))
			y0 = max(0, int(math.Floor(((0-wl.Y)/c.z-pad)*s)))
			x1 = min(fw, int(math.Ceil(((1920-wl.X)/c.z+pad)*s)))
			y1 = min(fh, int(math.Ceil(((1080-wl.Y)/c.z+pad)*s)))
		} else {
			// seen at an angle: the window's corners, turned about the point and seen from 2600 away
			const far = 2600.
			sx, cx := math.Sincos(c.rx * math.Pi / 180)
			sy, cy := math.Sincos(c.ry * math.Pi / 180)
			wh := float64(fh) / s // the window's height, which is not always the film's
			for _, p := range [][2]float64{{0, 0}, {filmW, 0}, {filmW, wh}, {0, wh}} {
				x, y := (p[0]-c.cx)*c.z, (p[1]-c.cy)*c.z
				x, z := x*cy, -x*sy
				y, z = y*cx-z*sx, y*sx+z*cx
				k := far / (far + z)
				wl.Quad = append(wl.Quad, c.ox+x*k, c.oy+y*k)
			}
		}
		if x1 > x0 && y1 > y0 {
			w, h := x1-x0, y1-y0
			if cap(f.crop) < w*h*4 {
				f.crop = make([]byte, w*h*4)
			}
			pix = f.crop[:w*h*4]
			for y := 0; y < h; y++ {
				o := img.PixOffset(img.Rect.Min.X+x0, img.Rect.Min.Y+y0+y)
				copy(pix[y*w*4:(y+1)*w*4], img.Pix[o:o+w*4])
			}
			m.W, m.H, m.SX, m.SY, m.FW, m.FH, m.Scale = w, h, x0, y0, fw, fh, s/f.k
			layers = append(layers, wl)
		}
	}
	layers = append(layers, f.over...)
	for _, t := range f.titles {
		if T < t.t0 {
			continue
		}
		l := t.l
		k := span(T, t.t0, t.in)
		if t.reveal {
			l = l.reveal(k)
		} else {
			e := outExpo(k)
			l.A *= e
			l.Y += (1 - e) * 18
		}
		layers = append(layers, l)
	}
	if k := float64(f.n-f.hitAt) / float64(f.frames(.22)); k < 1 {
		m.Flash = f.hitK * (1 - k) * (1 - k)
	}
	m.Layers = layers
	hdr, err := json.Marshal(m)
	if err != nil {
		f.t.Fatal(err)
	}
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], uint32(len(hdr)))
	f.w.Write(n[:])
	f.w.Write(hdr)
	if _, err := f.w.Write(pix); err != nil {
		f.t.Fatal("film: ", err)
	}
	f.n++
}

// shoot runs a shot for sec seconds: each frame, set places the camera and the type for t
// seconds into the shot and gives the app its cues, the app's clock moves on, and the frame goes.
func (f *film) shoot(sec float64, set func(t float64)) {
	n := f.frames(sec)
	f.run(sec, 1, func() bool { n--; return n <= 0 }, set)
}

// shootUntil is shoot for a shot that lasts until done says so, and sec seconds at most. The
// shot ends on a beat, half a second: one that is done between two runs on to the next.
func (f *film) shootUntil(sec float64, done func() bool, set func(t float64)) {
	f.run(sec, f.frames(.5), done, set)
}

// run is a shot that ends on a frame that is a multiple of grid, once done.
func (f *film) run(sec float64, grid int, done func() bool, set func(t float64)) {
	f.sp = -1
	over := false
	for i := 0; ; i++ {
		if i > f.frames(sec+.5) {
			f.t.Fatalf("%.1fs: the shot never ended", f.now())
		}
		f.st = float64(i) / float64(f.fps)
		f.over, f.under, f.win = f.over[:0], f.under[:0], false
		f.bloom, f.vig, f.dim, f.bg = .32, .5, 0, filmHUD
		set(f.st)
		if f.tt != nil {
			f.step(f.tt, f.a)
		}
		f.emit()
		f.sp = f.st
		if over = over || done(); over && f.n%grid == 0 {
			f.titles = f.titles[:0] // the type goes with its shot
			return
		}
	}
}

// step moves a stage's clock on by a frame of the film, and has the app drawn at that time.
// The Tester draws what moves a refresh after its last frame, whenever that was: a timer of the
// app's that comes due between two frames of the film (every 150 ms while it scans, at each
// step of Auto Clean) draws one there, the next is then due after the film's, and the film
// would show the same frame twice, what moves standing still for one. So a frame that is
// behind the film's time is drawn again, at it.
func (f *film) step(tt *ui.Tester, a *app) {
	tt.Advance(time.Duration(f.rate * float64(time.Second) / float64(f.fps)))
	if tt.FramePending() && tt.Now().Sub(a.now) > 2*time.Millisecond {
		tt.Frame()
		f.redrawn++
	}
}

// detail has the app drawn sharp enough for a camera z close.
func (f *film) detail(z float64) {
	s := max(1.5, min(4, z)) * f.k
	if f.draft {
		s /= 2
	}
	f.scale(s)
}

// scale has the stage on show drawn at s pixels to a point.
func (f *film) scale(s float64) {
	if f.scales == nil {
		f.scales = map[*ui.Tester]float64{}
	}
	if math.Abs(s-f.scales[f.tt]) > .01 {
		f.scales[f.tt] = s
		f.tt.SetScale(float32(s))
	}
}

// skip lets sec seconds of the app pass off camera.
func (f *film) skip(sec float64) {
	for i := f.frames(sec); i > 0; i-- {
		f.tt.Advance(time.Second / time.Duration(f.fps))
	}
	f.tt.Frame()
}

// wait gives background work (git, the trash journal) the time it takes, off the film's clock.
func (f *film) wait(what string, ok func() bool) {
	for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			f.t.Fatalf("%.1fs: timeout: %s", f.now(), what)
		}
		f.tt.Frame()
	}
	f.tt.Frame()
}

// find is the middle of the label showing s.
func (f *film) find(s string) (float32, float32) {
	r, ok := f.tt.Find(s)
	if !ok {
		f.t.Fatalf("%.1fs: no %q on screen: %q", f.now(), s, f.tt.Texts())
	}
	return r.X + r.W/2, r.Y + r.H/2
}

func (f *film) prefixed(p string) string {
	for _, s := range f.tt.Texts() {
		if strings.HasPrefix(s, p) {
			return s
		}
	}
	f.t.Fatalf("%.1fs: no label starting with %q: %q", f.now(), p, f.tt.Texts())
	return ""
}

func (f *film) node(rel string) int32 {
	n := f.a.t.Find(filepath.Join(f.root, rel))
	if n < 0 {
		f.t.Fatalf("%s not in the tree", rel)
	}
	return n
}

// arc is the middle of rel's sector.
func (f *film) arc(rel string) (float32, float32) {
	x, y, _ := f.a.arcPoint(f.node(rel))
	return x, y
}

// chart is the middle of the chart.
func (f *film) chart() (float64, float64) {
	r := f.a.chartR
	return float64(r.X + r.W/2), float64(r.Y + r.H/2)
}

// park takes the pointer, which the film never shows, off everything.
func (f *film) park() { f.tt.Move(4, 4) }

// ---------------------------------------------------------------------------------------------
// the film

func (f *film) play() {
	if f.covers {
		for _, i := range coverStyles() {
			f.cover(i)
		}
		return
	}
	f.cover(coverStyle())
	f.opener()
	f.a = f.mk()
	f.coldOpen()
	f.seeIt()
	f.knowIt()
	f.cleanIt()
	f.undoIt()
	f.haul()
	f.payoff()
	f.styles()
	f.endCard()
}

// coverStyles are the styles a cover can be made in: those in which a vehicle comes for the
// trash. The E-ink style keeps still, and its trash is emptied without one.
func coverStyles() []int {
	var out []int
	for i, p := range palettes {
		if p.motion {
			out = append(out, i)
		}
	}
	return out
}

// coverStyle is the style of the film's own cover: Classic, the one picked of the nine, for its
// colours. The film itself plays in the first style, as the app first opens.
func coverStyle() int {
	for i, p := range palettes {
		if p.name == "classic" {
			return i
		}
	}
	return 0
}

// cover: the film's first frame, and its poster, in the app's style i: the chart of a whole
// disk, the craft at work over the bin, and the name. Both pictures are the app's own, each
// drawn by an app of its own at the size it has here, and laid on the style's ground: a frame
// of the film shows one window, so the film puts the two on a plate of its own. It is one
// frame: what a player shows before it plays, and gone as it does.
func (f *film) cover(i int) {
	p := palettes[i]
	deco, rule, fps := p.deco, p.rule, f.fps
	p.deco, p.rule = decoNone, 0 // a ground with nothing on it: a pattern, or the panel's rule, would end where each picture does
	f.style = i
	if f.fps < 30 {
		f.fps = 60 // the apps are played to where the pictures are taken, whatever the film's rate
	}
	defer func() { p.deco, p.rule, f.style = deco, rule, 0 }()

	W, H := int(math.Round(1920*f.k)), int(math.Round(1080*f.k))
	plate := image.NewRGBA(image.Rect(0, 0, W, H))
	ground := [3]float64{float64(p.bg.R), float64(p.bg.G), float64(p.bg.B)}
	for i := 0; i < len(plate.Pix); i += 4 {
		plate.Pix[i], plate.Pix[i+1], plate.Pix[i+2], plate.Pix[i+3] = p.bg.R, p.bg.G, p.bg.B, 255
	}
	// lay puts src on the plate, its pixel (sx, sy) at the plate's (dx, dy), as much of it as
	// mask says of each pixel by where it is from there. Of the two, what stands further off
	// the ground stays (the lighter on a dark one, the darker on a light one): on one ground,
	// two pictures meet without an edge.
	lay := func(src *image.RGBA, sx, sy, dx, dy, reach float64, mask func(x, y float64) float64) {
		for y := max(0, int(dy-reach)); y < min(H, int(dy+reach)); y++ {
			for x := max(0, int(dx-reach)); x < min(W, int(dx+reach)); x++ {
				m := clamp(mask(float64(x)-dx, float64(y)-dy))
				u, v := x+int(math.Round(sx-dx)), y+int(math.Round(sy-dy))
				if m <= 0 || u < 0 || v < 0 || u >= src.Rect.Dx() || v >= src.Rect.Dy() {
					continue
				}
				o, q := plate.PixOffset(x, y), src.PixOffset(src.Rect.Min.X+u, src.Rect.Min.Y+v)
				for c := 0; c < 3; c++ {
					w, old := ground[c]+(float64(src.Pix[q+c])-ground[c])*m, float64(plate.Pix[o+c])
					if p.dark == (w > old) {
						plate.Pix[o+c] = uint8(w)
					}
				}
			}
		}
	}
	pass := func(tt *ui.Tester, sec float64) {
		for i := f.frames(sec); i > 0; i-- {
			tt.Advance(time.Second / time.Duration(f.fps))
		}
		tt.Frame()
	}

	// the chart: a disk just scanned, all of it, to its instruments' ring
	a := f.mk()
	a.setTree(f.fresh(), nil)
	a.sun.intro = time.Time{}
	tt := ui.NewTester(a.view, filmW, filmH)
	tt.Move(4, 4)
	pass(tt, 1)
	const chartR = 470.             // on the canvas
	edge := float64(a.sun.rad) + 58 // the chart's own radius and its instruments, in points
	if p.instr == instrNone {
		edge = float64(a.sun.rad) + 26
	}
	z := chartR / edge * f.k // pixels to a point
	tt.SetScale(float32(z))
	pass(tt, .5)
	lay(tt.Image(), float64(a.sun.cx)*z, float64(a.sun.cy)*z, 480*f.k, 560*f.k, chartR*f.k, func(x, y float64) float64 {
		return (chartR*f.k - math.Hypot(x, y)) / (14 * z)
	})

	// the craft: a trash being emptied, the beam on and the items going up it
	s := f.trashStage(func(items []cleanItem) []cleanItem { return items })
	z = 1.5 * f.k
	s.tt.SetScale(float32(z))
	h := f.hauler(s, 3, false)
	for i := f.frames(2.7); i > 0; i-- {
		h.step()
		s.tt.Advance(time.Second / time.Duration(f.fps))
	}
	s.tt.Frame()
	sc := float64(h.g.sc)
	x0, x1 := (h.pf-198)*z, (float64(h.g.binR)+50)*z // its readout ahead of it, to the bin's far side
	y0, y1 := (h.pb-99*sc)*z, (float64(h.g.floor)+20)*z
	hw, hh := (x1-x0)/2, (y1-y0)/2
	lay(s.tt.Image(), (x0+x1)/2, (y0+y1)/2, 1406*f.k, 752*f.k, max(hw, hh), func(x, y float64) float64 {
		return min(hw-math.Abs(x), hh-math.Abs(y)) / (22 * z)
	})
	pendingToasts = nil
	f.fps = fps

	col := func(c ui.Color) []float64 {
		return []float64{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255, 1}
	}
	if !f.covers {
		f.cue("open")
	}
	f.plate = plate
	f.shoot(1/float64(f.fps), func(float64) {
		f.look(at(960, 540, 1))
		f.bg = col(p.bg)[:3]
		f.vig, f.bloom = .15, 0
		if p.dark {
			f.vig, f.bloom = .6, .4
		}
		f.put(txt(f.L("THE DISK CLEANER YOU CAN TRUST", "信 得 过 的 磁 盘 清 理"), 1016, 168, 24).mono().col(col(p.accent)).tr(5))
		f.put(fl{K: "icon", X: 1091, Y: 290, Size: 150, A: 1})
		f.put(txt("Tossy", 1196, 352, 200).wt(.62).tr(-7).col(col(p.text)))
		f.put(txt(f.L("See it. Then clean it.", "看得见，才敢删。"), 1018, 452, 52).wt(.2).col(col(p.t2)))
	})
	f.plate = nil
}

// opener: the film's best picture first, in one take. A stage of its own, its trash already
// full, and the craft that comes when the trash is emptied: the bin, close, and the camera
// pulls back as the craft settles over it, its beam comes on and the items go up it.
func (f *film) opener() {
	s := f.trashStage(func(items []cleanItem) []cleanItem { return items })
	f.enter(s)
	f.detail(3)
	h := f.hauler(s, 2.2, false)
	for i := f.frames(.7); i > 0; i-- { // most of its way down, off camera
		h.step()
		f.step(s.tt, s.a)
	}
	g := h.g
	bx, by := float64(g.binCX), float64(g.binTop+g.floor)/2 // the bin's middle
	cx, belly := h.pf+float64(vehLen[h.r.kind]*g.sc)/2, h.pb
	gx, gy := (bx+cx)/2-65, (by+belly)/2-24       // the middle of the group: readout, craft, beam, bin
	f.shoot(2-1/float64(f.fps), func(t float64) { // the bar, less the cover's frame
		h.step()
		k := outExpo(span(t, .05, 1.0))
		f.look(at(lerp(bx, gx, k), lerp(by+6, gy, k), lerp(3.3, 1.95, k)-.03*t))
		f.dim, f.vig, f.bloom = 1-span(t, 0, .3), .75, .45
		f.put(cover("t", 40, 80, 1)...)                  // the trash's rows end above the craft; the camera has the last of them
		f.put(cover("b", lerp(820, 1000, k), 120, 1)...) // and the trash's buttons under the bin, from the first frame
	})
	f.tt, f.a = nil, nil
	pendingToasts = nil
}

// coldOpen: a full disk, then what filled it, faster and faster.
func (f *film) coldOpen() {
	tree := f.tree
	size := func(rel string) string {
		n := tree.Find(filepath.Join(f.root, rel))
		if n < 0 {
			f.t.Fatalf("%s not in the tree", rel)
		}
		return fmtSize(tree.Nodes[n].Size)
	}
	head := f.L("Disk full. Again.", "磁盘，又满了。")
	full := 1 - f.free0/500e9
	f.cue("full")
	f.hit(.22)
	f.shoot(2, func(t float64) {
		f.bg, f.vig = filmNight, 0
		hs := 124 + 8*t
		tr := -3.
		if f.zh {
			tr = 5
		}
		f.put(txt(head, 960, 520, hs).wt(.56).tr(tr).al("c").reveal(span(t, .08, .5)))
		// the disk line, filling
		k := lerp(.52, full, ease(span(t, .35, .9)))
		a := outExpo(span(t, .3, .4))
		c := filmInk
		if k > .9 {
			c = filmRed
		}
		f.put(rect(660, 606, 600, 6, 3, filmInk, .14*a), rect(660, 606, 600*k, 6, 3, c, a))
		free := fmtSize(int64(500e9 * (1 - k)))
		f.put(txt(f.L(free+" free of "+fmtSize(500e9), fmtSize(500e9)+" 中仅剩 "+free), 960, 668, 24).mono().col(filmGrey).al("c").alpha(a))
	})
	// the culprits: each a cut, a bar of them: two a beat long, two in eighths, four in sixteenths
	list := []string{
		"projects/shop/node_modules", "Library/Developer/Xcode/DerivedData", ".npm", ".cargo/registry",
		"Library/Caches/go-build", ".gradle/caches", ".ollama/models", "Library/Developer/CoreSimulator",
	}
	raw, sum := make([]float64, len(list)), 0.
	for i := range raw {
		raw[i] = []float64{.5, .25, .125}[min(2, i/2)]
		sum += raw[i]
	}
	f.cue("flurry")
	n0, total, acc := f.n, f.frames(sum), 0.
	for i, rel := range list {
		name, sz := filepath.Base(rel), size(rel)
		acc += raw[i] / sum
		frames := max(1, n0+int(math.Round(acc*float64(total)))-f.n)
		f.cues.Ticks = append(f.cues.Ticks, float64(n0)/float64(f.fps)+(acc-raw[i]/sum)*sum) // in time, to the sample
		f.shoot(float64(frames)/float64(f.fps), func(t float64) {
			f.bg, f.vig = filmNight, 0
			f.put(txt(name, 960, 548, 96*(1+.07*math.Exp(-t*26))).mono().wt(.4).al("c")) // each lands with a little weight
			f.put(txt(sz, 960, 640, 44).mono().col(filmAmber).al("c"))
			f.put(txt("~/"+rel, 960, 706, 22).mono().col(filmFaint).al("c"))
			f.put(txt(fmt.Sprintf("%02d", i+1), 960, 392, 22).mono().col(filmFaint).tr(4).al("c"))
		})
	}
}

// seeIt: the scan, and the chart coming up.
func (f *film) seeIt() {
	a := f.a
	a.root, a.scanning, a.prog = f.root, true, &scan.Progress{}
	a.scanStart = time.Now()
	f.tt = ui.NewTester(a.view, filmW, filmH)
	a.scanStart = f.tt.Now()
	f.detail(2)

	// The radar, all of it in the picture: its rings, the brackets at its corners and the path
	// that runs under it. The camera comes in a little, to where the chart will take over.
	rad := float64(max(60, min(a.chartR.W, a.chartR.H)/2-70)) // as paintScanning has it
	const scanDrop = 10.                                      // the path under the radar weighs it down
	scanZ := 960 / (2 * (rad + 44))
	// and the counters running up: the scan is played from the tree
	var dirs []string
	var files, folders int64
	for i := range f.tree.Nodes {
		if f.tree.Nodes[i].IsDir() {
			folders++
			if p := f.tree.Path(int32(i)); !strings.Contains(p, "/.git") {
				dirs = append(dirs, p)
			}
		} else {
			files++
		}
	}
	total, last := f.tree.Nodes[0].Size, -1
	const scanFor = 2
	f.cue("scan")
	f.hit(.22)
	f.shoot(scanFor, func(t float64) {
		k := ease(clamp(t / scanFor))
		if j := min(len(dirs)-1, int(k*float64(len(dirs)))); j != last {
			last = j
			// a one-folder scan sets the progress' current path, as the scanner does
			scan.Scan(context.Background(), dirs[j], scan.Options{Progress: a.prog, Workers: 1})
		}
		a.prog.Files.Store(int64(k * float64(files)))
		a.prog.Dirs.Store(int64(k * float64(folders)))
		a.prog.Bytes.Store(int64(k * float64(total)))
		cx, cy := f.chart()
		f.look(at(cx, cy+scanDrop, lerp(scanZ*.9, scanZ, ease(clamp(t/scanFor)))))
		f.dim = 1 - span(t, 0, .2)
		f.put(scanFrame(1)...)
	})
	a.setTree(f.tree, nil)
	if !a.sun.intro.IsZero() {
		a.sun.intro = f.tt.Now()
	}
	f.tt.Frame()

	// the chart comes up as the camera pulls back and gives the left of the picture to the type
	f.cue("see")
	f.flash(.14)
	f.block(.3, 112, 560, f.N(108, 136), "l", "01", f.L("See it all.", "看得见"), f.L("Your whole disk, in one picture.", "整块磁盘，一眼看清。"))
	hover := []string{"Library", "projects"}
	f.shoot(3, func(t float64) {
		cx, cy := f.chart()
		k := outExpo(t / 1.7)
		f.look(at(cx, lerp(cy+scanDrop, cy, k), lerp(scanZ, 1.66, k)+.012*t).to(lerp(960, 1290, k), 540))
		f.put(scanFrame(1 - span(t, 0, .4))...)
		f.put(cover("l", 380, 520, span(t, .25, .6))...)
		for i, rel := range hover {
			if f.on(1.7 + float64(i)*.6) {
				f.tt.Move(f.arc(rel))
			}
		}
	})
}

// scanFrame keeps the scan's picture to the radar: the window's bars, which the camera has
// half of at the top and at the foot, go under the app's ground.
func scanFrame(a float64) []fl {
	return append(cover("t", 40, 50, a), cover("b", 1040, 34, a)...)
}

// knowIt: the verdicts, and what git says of a worktree.
func (f *film) knowIt() {
	a := f.a
	pal := a.pal
	rgb := func(c ui.Color) []float64 {
		return []float64{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255, 1}
	}
	a.selected = 0
	f.park()
	f.skip(.4)
	f.detail(1.8)

	// the chart turns to its verdicts, and the four of them are read out beside it
	sums := a.ev.LevelSums(0)
	names := []string{T("可安全清理"), T("建议复核后清理"), T("个人数据"), T("系统保护")}
	var top int64 = 1
	for _, s := range sums {
		top = max(top, s)
	}
	const lx = 1120.
	f.flash(.14)
	f.block(.15, lx, 318, f.N(92, 128), "l", "02", f.L("Know what's safe.", "分得清"), f.L("Every item gets a verdict, and the reason.", "每一项都有结论，也有理由。"))
	f.shoot(3, func(t float64) {
		if f.on(.2) {
			a.keepMode, a.mode = true, modeSafety
			f.tt.Move(4, 4)
		}
		cx, cy := f.chart()
		f.look(at(cx, cy, 1.6+.012*t).to(580, 540))
		f.put(cover("r", 1400, 440, 1)...)
		for i, name := range names {
			k := outExpo(span(t, .75+float64(i)*.18, .5))
			y := 548 + float64(i)*108
			c := rgb(pal.level[i])
			f.put(rect(lx, y-26, 16, 16, 8, c, k))
			f.put(txt(name, lx+38, y-6+(1-k)*14, 40).wt(.2).alpha(k))
			sz := fmtSize(sums[i])
			if sums[i] == 0 {
				sz = f.L("never touched", "永不触碰")
			}
			f.put(txt(sz, 1810, y-6+(1-k)*14, 30).mono().col(filmGrey).al("r").alpha(k))
			f.put(rect(lx, y+18, 690, 3, 1.5, filmInk, .1*k))
			f.put(rect(lx, y+18, 690*float64(sums[i])/float64(top)*ease(span(t, .9+float64(i)*.18, .8)), 3, 1.5, c, k))
		}
	})
	a.keepMode, a.mode = false, modeStructure

	// a worktree: what git says deleting it would lose
	a.zoomTo(f.node("projects/shop-worktrees/agent-2"))
	a.pf, a.git.open = pfList, true
	f.tt.Move(4, 4)
	f.wait("git status", func() bool {
		e := a.git.st[a.t.Path(a.focus)]
		return e != nil && e.done
	})
	f.wait("git family", func() bool {
		for _, s := range f.tt.Texts() {
			if strings.HasPrefix(s, "agent-3") {
				return true
			}
		}
		return false
	})
	f.skip(1.4)
	f.detail(3.1)
	f.flash(.14)
	f.block(.1, 112, 540, f.N(76, 92), "l", "", f.L("It asks git first.", "删之前，先问 git"), f.L("No unpushed work slips by.", "没提交的改动、只在本机的提交，一条不漏。"))
	px := float64(a.winW - a.wL/2)
	f.shoot(2, func(t float64) {
		f.look(at(px, lerp(150, 190, ease(clamp(t/2))), 3.1).to(1440, 540))
		f.put(cover("l", 560, 300, 1)...)
	})
	a.git.open, a.pf = false, pfNone
	a.zoomTo(0)
	a.selected = 0
	f.park()
	f.skip(1.2)
}

// cleanIt: the button, the sweep, the throw, and the number.
func (f *film) cleanIt() {
	a := f.a
	f.detail(3)

	// the button, closer and closer; the pointer comes over it, and it is pressed
	bx, by := f.find(T("自动清理"))
	pill := [2]float64{float64(a.chartR.X + a.chartR.W/2), float64(by)}
	f.cue("click")
	f.flash(.14)
	f.block(.05, 960, 360, f.N(120, 150), "c", "03", f.L("One click.", "一键清"), f.L("It takes only what rebuilds itself.", "只带走能重新生成的东西。"))
	f.shoot(2, func(t float64) {
		if f.on(1.3) { // late, so that its tooltip does not come up before the press
			f.tt.Move(bx, by)
		}
		if f.on(1.78) {
			f.tt.Press(bx, by)
		}
		if f.on(1.9) { // before the cut, at any frame rate
			f.tt.Release(bx, by)
			f.tt.Move(4, 4)
		}
		f.look(at(pill[0], pill[1]-52, lerp(2.3, 3.0, ease(clamp(t/2)))).to(960, 640))
		f.put(cover("t", 440, 190, 1)...)
	})
	an := a.anal
	if an == nil {
		f.t.Fatal("the press did not start Auto Clean")
	}
	f.wait("the process check", func() bool { return an.ready })

	// the sweep: the chart in its verdicts, the beam, what it finds
	f.detail(2)
	f.flash(.14)
	f.shoot(2, func(t float64) {
		cx, cy := f.chart()
		f.look(at(cx, cy, 1.8+.05*t))
		if an := a.anal; an != nil {
			f.put(cover("b", 1040, 200, 1)...)
			f.put(txt(T(an.phase()), 940, 1006, 26).mono().col(filmGrey).al("r"))
			f.put(txt(nItems(int64(an.found))+"  ·  "+fmtSize(an.foundSz), 980, 1006, 26).mono().col(filmCyan))
		}
	})
	for i := 0; a.anal != nil; i++ { // a big tree's sweep takes longer than the film gives it
		if i > f.frames(8) {
			f.t.Fatal("the sweep never ended")
		}
		f.step(f.tt, a)
	}
	f.wait("the marks", func() bool { return a.trashMoving == 0 })

	// the throw: everything it took flies to the bin; then the number
	f.detail(1.7)
	f.cue("throw")
	f.hit(.22)
	total := float64(a.bin.Size())
	label := f.L("CLEARED IN ONE CLICK", "一 键 清 出")
	f.shoot(3, func(t float64) {
		f.look(at(lerp(760, 800, ease(clamp(t/3))), 468, 1.5+.012*t))
		// the number comes in as the last of it lands
		const n0 = 1.2
		if f.on(n0) {
			f.flash(.14)
		}
		if t >= n0 {
			k := span(t, n0, .25)
			f.dim = .86 * k
			v := total * outExpo(span(t, n0, .9))
			num, unit, _ := strings.Cut(fmtSize(int64(v)), " ")
			f.put(txt(label, 960, 392, 26).mono().col(filmCyan).tr(6).al("c").alpha(k))
			f.put(txt(num, 1090, 610, 250).wt(.56).digits().tr(-8).al("r").alpha(k))
			f.put(txt(unit, 1118, 610, 110).wt(.4).col(filmGrey).alpha(k))
			f.put(txt(nItems(int64(len(a.bin.List())))+f.L("  ·  all of it rebuilds on demand", "  ·  全部可以重新生成"), 960, 706, 30).col(filmGrey).wt(.1).al("c").alpha(span(t, n0+.6, .4)))
		}
	})
}

// undoIt: the trash, and an item put back.
func (f *film) undoIt() {
	a := f.a
	a.goScene(sceneTrash)
	f.tt.Move(filmW-110, 150) // off the bin as it grows: a ball under the pointer scrolls the list to its row
	f.skip(2)
	f.detail(2)
	f.flash(.14)
	f.block(.1, 112, 560, f.N(100, 136), "l", "04", f.L("Undo anything.", "能反悔"), f.L("Nothing is gone until you empty the trash.", "清空之前，文件原地不动，随时放回。"))
	var rx, ry, bx, by float32
	f.shoot(3, func(t float64) {
		if f.on(.7) {
			rx, ry = f.find("node_modules") // the first row, the largest
			f.tt.Move(rx+60, ry)
		}
		if f.on(1) {
			f.cue("undo")            // on the bar: the roll into the emptying starts under the shot
			bx, by = f.find(T("还原")) // its button, which shows under the pointer
			if math.Abs(float64(by-ry)) > 14 {
				f.t.Fatalf("the first %s button is not the row's: at %.0f, the row at %.0f", T("还原"), by, ry)
			}
			f.tt.Move(bx, by)
		}
		if f.on(1.38) {
			f.tt.Press(bx, by)
		}
		if f.on(1.5) {
			f.tt.Release(bx, by)
			f.tt.Move(filmW-110, 150)
			f.flash(.12)
		}
		c := filmTrash(0)
		c.z *= 1 + .01*t
		f.look(c)
		f.put(cover("l", 760, 340, 1)...)
		f.put(cover("b", 990, 90, 1)...) // the list runs on under the picture
	})
}

// filmTrash is the camera on the trash panel: its header and rows at 0, the bin on its stage at 1.
func filmTrash(k float64) cam {
	return at(filmW-192, lerp(262, 566, k), 2).to(1500, 540)
}

// trashStage is an app beside the film's own, in the trash view, with what pick takes of what
// Auto Clean would in its trash. Its window is taller than the film's: the trash's rows then
// end well clear of the vehicle, and the chart stands clear above the floor.
func (f *film) trashStage(pick func([]cleanItem) []cleanItem) *stage {
	a := f.mk()
	a.setTree(f.fresh(), nil)
	a.sun.intro = time.Time{}
	s := &stage{a, ui.NewTester(a.view, filmW, 1500)}
	pass := func(sec float64) {
		for i := f.frames(sec); i > 0; i-- {
			s.tt.Advance(time.Second / time.Duration(f.fps))
		}
		s.tt.Frame()
	}
	pass(1)
	a.trashNodes(pick(a.candItems(0, safety.Safe)), true)
	for deadline := time.Now().Add(10 * time.Second); a.trashMoving > 0; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			f.t.Fatal("timeout: a stage's marks")
		}
	}
	pass(4)
	a.goScene(sceneTrash)
	s.tt.Move(filmW-110, 150)
	pass(3)
	pendingToasts = nil
	return s
}

// upTo picks the smallest items that come to no more than limit bytes, past the first skip.
func upTo(limit int64, skip int) func([]cleanItem) []cleanItem {
	return func(items []cleanItem) []cleanItem {
		sort.SliceStable(items, func(i, j int) bool { return items[i].meta.Size < items[j].meta.Size })
		var out []cleanItem
		var sum int64
		for _, it := range items[min(skip, len(items)):] {
			if sum+it.meta.Size > limit {
				break
			}
			out, sum = append(out, it), sum+it.meta.Size
		}
		return out
	}
}

// hauler plays an emptying on the film's clock, as a disk that takes a few seconds over it:
// the vehicle takes the items one by one, each when it would be deleted, by its share of the
// bytes. A real one then deletes them; the others are cut away from before they would.
type hauler struct {
	f        *film
	s        *stage
	r        *truckRun
	g        truckGeo
	pf, pb   float64 // where the vehicle stands: its front, and its base
	items    []trash.Item
	size     int64
	due      []int // the frame of its own each item is gone by
	start    float64
	n        int // frames of its own
	i        int
	started  bool
	finished bool
	real     bool
}

func (f *film) hauler(s *stage, budget float64, real bool) *hauler {
	a := s.a
	h := &hauler{f: f, s: s, real: real, items: a.bin.List()}
	sort.SliceStable(h.items, func(i, j int) bool { return h.items[i].Size < h.items[j].Size })
	keys := map[string]bool{}
	for _, it := range h.items {
		keys[it.Orig] = true
		h.size += it.Size
	}
	a.askClose()
	a.emptying = true
	if h.r = a.startTruck(keys, h.size); h.r == nil {
		f.t.Fatal("no vehicle: the bin is not on its stage")
	}
	h.g = a.truckGeo()
	pf, pb := a.truckParked(h.r, h.g)
	h.pf, h.pb = float64(pf), float64(pb)
	fps := float64(f.fps)
	h.start = (truckArrive.Seconds() + .3) * fps
	at := h.start
	for _, it := range h.items {
		at += budget * fps * (.3/float64(len(h.items)) + .7*float64(it.Size)/float64(max(1, h.size)))
		h.due = append(h.due, int(at))
	}
	s.tt.Frame() // nothing else may be moving: the view has to see the vehicle to start drawing it
	return h
}

// step tells the vehicle what has been deleted by this frame of its own.
func (h *hauler) step() {
	h.n++
	for h.i < len(h.items) && h.n >= h.due[h.i] {
		h.r.step(trash.Step{Item: h.items[h.i], Done: true})
		h.i, h.started = h.i+1, false
	}
	if h.i < len(h.items) && !h.started && float64(h.n) >= h.start {
		h.r.step(trash.Step{Item: h.items[h.i]})
		h.started = true
	}
	if h.real && h.i == len(h.items) && !h.finished {
		if _, err := h.s.a.bin.Empty(nil); err != nil {
			h.f.t.Fatal(err)
		}
		h.r.finish(h.size, h.s.a.bin.List(), nil) // the sizes as shown, not the fixture's on disk
		h.finished = true
	}
}

// frame is the camera on the vehicle at work: all of it, its readout and the bin, z times as
// close as shows them all.
func (h *hauler) frame(z float64) cam {
	g, sc := h.g, float64(h.g.sc)
	x0, x1 := h.pf-26, float64(g.binR)+34
	y0, y1 := float64(g.floor)-205*sc-40, float64(g.floor)+30 // room for the arm, and a lid thrown open
	if h.r.kind == vehSkiff {                                 // over the bin, its readout ahead of it
		x0, y0 = h.pf-200, h.pb-96*sc
	}
	fit := min(1720/(x1-x0), 880/(y1-y0))
	return at((x0+x1)/2, (y0+y1)/2, fit*z).to(960, 500)
}

// haul: the trash is emptied, and a vehicle comes for it: a bigger one for a bigger bin. Two
// more trashes are emptied beside the film's own, a small one and a middling one, and the film
// looks at each in turn: the van that picks a small bin's items out with its arm, the hauler
// that takes a mid-sized bin's in at the back, and the craft over the big one, as the last of
// it goes and the craft leaves.
func (f *film) haul() {
	a := f.a
	home := &stage{f.a, f.tt}
	// the other two, set up while the film is not looking. A bin's size goes by its share of
	// the disk, and its vehicle by its size: under 3% a van, under 15% a hauler.
	small := f.trashStage(upTo(13e9, 0))
	mid := f.trashStage(upTo(70e9, 5))
	hv := f.hauler(small, 3.3, false)
	for i := f.frames(2.2); i > 0; i-- { // the van is there and its arm is in the bin as the film turns to it
		hv.step()
		f.step(small.tt, small.a)
	}
	// the film's own is done as its shot, the third, begins: the last of it goes up, and the
	// craft leaves; the hauler is at work by the second
	hm, he := f.hauler(home, 2.5, true), f.hauler(mid, 4.6, false)
	if hv.r.kind != vehVan || he.r.kind != vehEV || hm.r.kind != vehSkiff {
		f.t.Fatalf("the vehicles are %d, %d and %d: want a van, a hauler and a craft", hv.r.kind, he.r.kind, hm.r.kind)
	}

	// every frame moves all three on; the one on show by the shot's own clock
	all := []*hauler{hm, hv, he}
	tick := func() {
		for _, h := range all {
			h.step()
			if h.s.tt != f.tt {
				f.step(h.s.tt, h.s.a)
			}
		}
	}
	cut := func(h *hauler, z float64) {
		if f.tt != h.s.tt {
			f.scale(1) // the one left runs on small, out of sight
		}
		f.enter(h.s)
		f.detail(z)
	}
	began := f.n
	line := txt(f.L("When it goes, a ride comes for it. Bigger bin, bolder ride.", "真要清空，有车来接：桶越大，来的车越大。"), 112, 1012, 44).wt(.4)
	caption := func() { // one line under all three
		u := float64(f.n-began) / float64(f.fps)
		e := outExpo(span(u, .4, .5))
		l := line.alpha(e)
		l.Y += (1 - e) * 18
		f.put(l)
	}
	side := func(h *hauler) {
		cut(h, 2.4)
		f.shoot(2, func(t float64) {
			tick()
			c := h.frame(1)
			c.z *= 1 + .03*t
			f.look(c)
			f.vig, f.bloom = .7, .42
			if h == hm { // the film's own window is no taller than the film: the trash's rows end by the craft
				f.put(cover("t", 70, 90, 1)...)
			}
			f.put(cover("b", 950, 150, 1)...)
			caption()
		})
	}
	f.cue("haul")
	f.hit(.22)
	side(hv) // the van, its arm in the small one
	f.flash(.12)
	side(he) // the hauler, the middling one going in at its back
	f.flash(.12)
	side(hm) // the craft: the last of the big one, and away
	all = all[:1]
	for i := 0; a.scene != sceneBrowse; i++ { // the app goes back to the disk by itself
		if i > f.frames(15) {
			f.t.Fatal("the trash never emptied")
		}
		tick()
		f.step(f.tt, a)
	}
	f.skip(1.2) // the trash's panel goes, and the disk's chart grows back
	pendingToasts = nil
}

// payoff: the disk line, with what was freed.
func (f *film) payoff() {
	a := f.a
	a.selected = a.focus
	f.park()
	f.detail(3.3)
	f.cue("payoff")
	f.hit(.22)
	before := f.free0
	gain := float64(a.freedTotal)
	label := f.L("FREE SPACE", "可 用 空 间")
	f.shoot(3, func(t float64) {
		if f.on(.3) { // the disk line reads the volume again, and runs to its new place
			f.freed = gain
			a.disk.ok = false
			f.tt.Move(4, 4)
		}
		cx, _ := f.chart()
		f.look(at(cx, 33, 3.3+.03*t).to(960, 250))
		f.put(cover("b", 610, 250, 1)...)
		k := outExpo(span(t, .3, 1.2))
		a0 := span(t, .1, .3)
		f.put(txt(label, 960, 560, 26).mono().col(filmCyan).tr(6).al("c").alpha(a0))
		was, wu, _ := strings.Cut(fmtSize(int64(before)), " ")
		num, unit, _ := strings.Cut(fmtSize(int64(before+gain*k)), " ")
		f.put(txt(was+" "+wu, 560, 800, 84).wt(.3).digits().col(filmFaint).al("r").alpha(a0))
		f.put(rect(600, 768, 90*span(t, .3, .4), 4, 2, filmFaint, a0))
		f.put(txt(num, 1420, 820, 230).wt(.56).digits().tr(-7).al("r").alpha(a0))
		f.put(txt(unit, 1444, 820, 100).wt(.4).col(filmGrey).alpha(a0))
	})
}

// styles: the whole window, turning a little, in each of the ten styles: eight in eighths, the
// ninth a beat, and home. The music's part is the payoff's, and plays on.
func (f *film) styles() {
	a := f.a
	f.detail(1.5)
	n := len(palettes)
	head := f.L("TEN STYLES", "十 种 风 格")
	const total = 3.
	t0 := 0.
	for i := 1; i <= n; i++ {
		a.palIdx = i % n
		a.pal = palettes[a.palIdx]
		f.tt.Frame()
		p := a.pal
		dur := .25
		switch i {
		case n - 1:
			dur = .5
		case n:
			dur = total - t0
		}
		from := t0
		f.flash(.1)
		// by the frame its end falls on: an eighth is not a whole number of frames at every rate
		frames := f.frames(t0+dur) - f.frames(t0)
		if t0 == 0 {
			frames = f.frames(dur)
		}
		f.shoot(float64(frames)/float64(f.fps), func(t float64) {
			g := (from + t) / total
			f.bg, f.vig = filmNight, .3
			f.bloom = 0
			if p.dark {
				f.bloom = .25
			}
			c := at(filmW/2, filmH/2, (1.02+.05*g)*(1+.02*math.Exp(-t*14))).to(960, 520)
			c.rx, c.ry, c.slab = lerp(7, 3, g), lerp(-17, -7, g), 12
			f.look(c)
			f.under = append(f.under, fl{K: "glow", X: 960, Y: 560, W: 1150, H: 620, C: filmCyan, A: .2})
			f.put(txt(head, 960, 80, 26).mono().col(filmCyan).tr(6).al("c"))
			f.put(txt(fmt.Sprintf("%02d  ·  %s", (i-1)%n+1, T(p.label)), 960, 1036, 28).mono().col(filmGrey).al("c"))
		})
		t0 += dur
	}
}

// endCard: the mark, the name, the line.
func (f *film) endCard() {
	f.cue("end")
	f.hit(.22)
	f.shoot(3, func(t float64) {
		f.bg, f.vig = filmNight, 0
		k := outExpo(span(t, .05, .7))
		f.under = append(f.under, fl{K: "glow", X: 960, Y: 400, W: 520, H: 420, C: []float64{.45, .9, .68, 1}, A: .22 * k})
		f.put(fl{K: "icon", X: 960, Y: 392, Size: lerp(150, 196, k) + 4*t, A: span(t, .05, .25)})
		f.put(txt("Tossy", 960, 668, 132).wt(.6).tr(-4).al("c").reveal(span(t, .2, .55)))
		a := outExpo(span(t, .55, .45))
		tr := 0.
		if f.zh {
			tr = 6
		}
		f.put(txt(f.L("See it. Then clean it.", "看得见，才敢删。"), 960, 754+(1-a)*16, 44).wt(.2).col(filmGrey).tr(tr).al("c").alpha(a))
		b := outExpo(span(t, .8, .45))
		f.put(txt("macOS  ·  Linux      github.com/21stware/tossy", 960, 990, 22).mono().col(filmFaint).al("c").alpha(b))
		f.dim = span(t, 2.55, .45)
	})
}
