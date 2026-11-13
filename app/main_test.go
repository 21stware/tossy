package main

import (
	"fmt"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// fixture builds a small home-like tree with real (allocated) bytes.
func fixture(t testing.TB) string {
	root := t.TempDir()
	rnd := rand.New(rand.NewSource(1))
	buf := make([]byte, 1<<20)
	rnd.Read(buf)
	write := func(rel string, kb int) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		for kb > 0 {
			n := min(kb, 1024)
			f.Write(buf[:n*1024])
			kb -= n
		}
		f.Close()
	}
	for i := 0; i < 40; i++ {
		write(fmt.Sprintf("projects/web/node_modules/pkg%02d/index.js", i), 40+rnd.Intn(400))
	}
	for i := 0; i < 12; i++ {
		write(fmt.Sprintf("projects/api/target/debug/obj%02d.o", i), 200+rnd.Intn(600))
		write(fmt.Sprintf("projects/api/src/m%02d.rs", i), 8+rnd.Intn(30))
	}
	for i := 0; i < 8; i++ {
		write(fmt.Sprintf("Documents/report-%d.pdf", i), 300+rnd.Intn(900))
		write(fmt.Sprintf("Pictures/2025/IMG_%04d.jpg", i), 800+rnd.Intn(2000))
	}
	write("Downloads/Installer.dmg", 6000)
	write("Downloads/notes.zip", 1500)
	write("Movies/clip.mov", 9000)
	for i := 0; i < 30; i++ {
		write(fmt.Sprintf(".npm/_cacache/content/%02d", i), 50+rnd.Intn(200))
		write(fmt.Sprintf("misc/small/f%02d.txt", i), 4)
	}
	old := time.Now().Add(-60 * 24 * time.Hour)
	os.Chtimes(filepath.Join(root, "Downloads/Installer.dmg"), old, old)
	return root
}

func loaded(t testing.TB) *app {
	t.Setenv("FANFREE_STATE", t.TempDir())
	a := newApp()
	done := make(chan struct{})
	a.startScan(fixture(t))
	go func() {
		for a.scanning {
			time.Sleep(10 * time.Millisecond)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("scan timeout")
	}
	if a.t == nil {
		t.Fatal(a.scanErr)
	}
	return a
}

func shot(t *testing.T, tt *ui.Tester, name string) {
	dir := os.Getenv("FANFREE_SHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, tt.Image())
}

func TestViewFlow(t *testing.T) {
	a := loaded(t)
	tt := ui.NewTester(a.view, 1280, 820)
	tt.Advance(2 * time.Second)
	tt.Frame()
	shot(t, tt, "1-overview")
	if len(a.sun.arcs) == 0 {
		t.Fatal("no arcs")
	}
	a.set.InUseMin = 0 // fixture files were all just written
	a.set.SkipApps = false
	tt.Frame()
	items, _ := a.idlePlan()
	plan := planSize(items)
	if !tt.HasText("自动清理") || !tt.HasText(fmtSize(plan)) {
		t.Errorf("clean button missing: %v", tt.Texts())
	}

	// hover the biggest top-level arc, then drill into it
	var h *arc
	for i := range a.sun.arcs {
		if a.sun.arcs[i].ring == 1 && a.sun.arcs[i].node >= 0 {
			h = &a.sun.arcs[i]
			break
		}
	}
	x, y := a.arcPointOf(h)
	tt.Move(x, y)
	tt.Frame()
	if a.hover != h.node {
		t.Errorf("hover = %d, want %d", a.hover, h.node)
	}
	shot(t, tt, "2-hover")
	want := h.node
	tt.ClickAt(x, y)
	tt.Advance(time.Second)
	tt.Frame()
	if a.focus != want {
		t.Errorf("focus = %d, want %d", a.focus, want)
	}
	shot(t, tt, "3-drilled")

	// centre goes back up
	tt.ClickAt(a.sun.cx, a.sun.cy)
	tt.Advance(time.Second)
	tt.Frame()
	if a.focus != 0 {
		t.Errorf("focus after centre click = %d", a.focus)
	}

	// safety mode
	if err := tt.Click("安全性"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	shot(t, tt, "4-safety")

	// 自动清理: analysis sweep → throw → tree shrinks → undo from the toast
	before := a.t.Nodes[0].Size
	if err := tt.Click("自动清理"); err != nil {
		t.Fatal(err)
	}
	if a.anal == nil {
		t.Fatal("no analysis")
	}
	tt.Advance(900 * time.Millisecond)
	tt.Frame()
	shot(t, tt, "5-analysis")
	if !tt.HasText("停止分析") {
		t.Errorf("busy button: %v", tt.Texts())
	}
	settle := func() {
		for i := 0; i < 200 && (a.anal != nil || a.trashMoving > 0 || a.fly.active() || len(a.timers) > 0); i++ {
			time.Sleep(10 * time.Millisecond)
			tt.Advance(50 * time.Millisecond)
			tt.Frame()
		}
	}
	for i := 0; i < 100 && (a.anal == nil || a.anal.doneAt.IsZero()); i++ {
		if a.anal == nil {
			t.Fatal("analysis ended without a plan")
		}
		time.Sleep(10 * time.Millisecond)
		tt.Advance(50 * time.Millisecond)
		tt.Frame()
	}
	shot(t, tt, "6-plan")
	tt.Advance(450 * time.Millisecond)
	tt.Frame()
	tt.Advance(250 * time.Millisecond)
	tt.Frame()
	shot(t, tt, "7-flying")
	settle()
	if got := a.t.Nodes[0].Size; got != before-plan {
		t.Errorf("root size %d, want %d", got, before-plan)
	}
	tt.Frame()
	shot(t, tt, "8-cleaned")
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err, tt.Texts())
	}
	settle()
	if got := a.t.Nodes[0].Size; got != before {
		t.Errorf("after undo root size %d, want %d", got, before)
	}
	if len(a.bin.List()) != 0 {
		t.Errorf("trash not empty after undo: %d", len(a.bin.List()))
	}

	// 查看评估: the sheet, then 释放 what is checked
	if err := tt.Click("查看评估"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if a.scene != sceneEval || a.eval == nil {
		t.Fatalf("scene %d after 查看评估", a.scene)
	}
	shot(t, tt, "6-eval")
	chosen, size := a.eval.chosen()
	if len(chosen) == 0 {
		t.Fatal("nothing chosen")
	}
	if err := tt.Click("释放 " + fmtSize(size)); err != nil {
		t.Fatal(err, tt.Texts())
	}
	settle()
	if got := a.t.Nodes[0].Size; got != before-size {
		t.Errorf("after eval clean root size %d, want %d", got, before-size)
	}

	// manual delete of a personal file asks first
	var doc int32 = -1
	for i := range a.t.Nodes {
		if a.t.Name(int32(i)) == "report-0.pdf" {
			doc = int32(i)
		}
	}
	a.requestTrash(doc)
	tt.Frame()
	if a.ask == nil || !tt.HasText("确认删除个人数据？") {
		t.Errorf("no risky confirm: %v", tt.Texts())
	}
	tt.Advance(time.Second)
	tt.Frame()
	shot(t, tt, "8b-ask")
	if err := tt.Click("仍然移至" + trashName()); err != nil {
		t.Fatal(err, tt.Texts())
	}
	settle()
	if a.t.Attached(doc) {
		t.Error("document still attached")
	}

	// trash panel
	if err := tt.Click("查看"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.HasText("按清理规则") || !tt.HasText("你手动删除") {
		t.Errorf("trash panel: %v", tt.Texts())
	}
	shot(t, tt, "9-trash")
	if r := a.tsun.root; r == nil || r.cnt != len(a.bin.List()) || len(a.tsun.arcs) == 0 {
		t.Errorf("trash-origin chart not built: %+v", r)
	}
	if !tt.HasText("废纸篓里的内容来自哪里") && !tt.HasText(trashName()+"里的内容来自哪里") {
		t.Error("trash legend missing")
	}

	// empty: confirm, dump, gone
	tt.Click("清空 " + fmtSize(func() (s int64) {
		for _, it := range a.bin.List() {
			s += it.Size
		}
		return
	}()))
	tt.Advance(time.Second)
	tt.Frame()
	shot(t, tt, "9b-empty")
	if err := tt.Click("永久删除"); err != nil {
		t.Fatal(err, tt.Texts())
	}
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	shot(t, tt, "9c-dump")
	settle()
	if n := len(a.bin.List()); n != 0 || a.freedTotal == 0 {
		t.Errorf("after empty: %d items, freed %d", n, a.freedTotal)
	}
	if a.scene != sceneBrowse {
		t.Errorf("scene %d after empty", a.scene)
	}

	// settings
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.HasText("减少动态效果") {
		t.Errorf("settings: %v", tt.Texts())
	}
	shot(t, tt, "10-settings")
	tt.Click("完成")
	tt.Advance(time.Second)

	// classic theme
	if err := tt.Click("经典"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("目录"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	shot(t, tt, "11-classic")
}

// TestInUse: freshly written caches are skipped by the recent-write check.
func TestInUse(t *testing.T) {
	a := loaded(t)
	plan := a.cleanPlan()
	busy := recentWrites(a.t, plan, 10*time.Minute, time.Now())
	for _, it := range plan {
		old := a.t.Name(it.node) == "Installer.dmg" // 60 days old
		if _, b := busy[it.node]; b == old {
			t.Errorf("%s: busy=%v", a.t.Name(it.node), b)
		}
	}
	if got := appsUsing("/Users/x/Library/Caches/Google/Chrome/Default", []string{"google chrome helper"}); got == "" {
		t.Error("chrome cache not flagged")
	}
	if got := appsUsing("/Users/x/Library/Caches/Google/Chrome/Default", []string{"finder"}); got != "" {
		t.Error("flagged without chrome running")
	}
}

// TestStyles renders every style in both modes, with a hover callout and the evaluation sheet.
func TestStyles(t *testing.T) {
	a := loaded(t)
	a.set.InUseMin = 0
	tt := ui.NewTester(a.view, 1280, 820)
	for i, p := range palettes {
		a.palIdx = i
		a.mode = modeStructure
		tt.Advance(2 * time.Second)
		tt.Frame()
		if a.pal != p || len(a.sun.arcs) == 0 {
			t.Fatalf("%s: not applied", p.name)
		}
		var h *arc
		for k := range a.sun.arcs {
			if a.sun.arcs[k].ring == 2 && a.sun.arcs[k].node >= 0 {
				h = &a.sun.arcs[k]
				break
			}
		}
		x, y := a.arcPointOf(h)
		tt.Move(x, y)
		tt.Frame()
		shot(t, tt, "s-"+p.name+"-1")
		a.mode = modeSafety
		tt.Move(5, 5)
		tt.Advance(time.Second)
		tt.Frame()
		shot(t, tt, "s-"+p.name+"-2")
	}
	a.palIdx = 1
	a.evaluate()
	tt.Advance(time.Second)
	tt.Frame()
	shot(t, tt, "s-mech-eval")
}

func (a *app) arcPointOf(h *arc) (float32, float32) {
	x, y, _ := a.arcPoint(h.node)
	return x, y
}

// TestBigTree renders a large real tree (FANFREE_BIG=path) and reports frame times.
func TestBigTree(t *testing.T) {
	root := os.Getenv("FANFREE_BIG")
	if root == "" {
		t.Skip("set FANFREE_BIG")
	}
	t.Setenv("FANFREE_STATE", t.TempDir())
	a := newApp()
	t0 := time.Now()
	a.startScan(root)
	for a.scanning {
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("scan: %d nodes in %v", len(a.t.Nodes), time.Since(t0))
	tt := ui.NewTester(a.view, 1440, 900)
	tt.Advance(2 * time.Second)
	t1 := time.Now()
	tt.Frame()
	t.Logf("first frame %v, paint %.2fms, %d arcs", time.Since(t1), a.drawMs, a.arcCount)
	// hover sweeps: build + hit test only, geometry cached
	t2 := time.Now()
	for i := 0; i < 60; i++ {
		tt.Move(a.sun.cx+float32(i%30)*6, a.sun.cy-a.sun.r0-20)
		tt.Frame()
	}
	t.Logf("hover frame avg %v", time.Since(t2)/60)
	t3 := time.Now()
	tt.Frame()
	_ = tt.Image()
	t.Logf("image %v", time.Since(t3))
	shot(t, tt, "9-big")
}

// TestIcon regenerates resources/icon.png (FANFREE_ICON=1): a HUD sunburst on a rounded plate.
func TestIcon(t *testing.T) {
	if os.Getenv("FANFREE_ICON") == "" {
		t.Skip("set FANFREE_ICON")
	}
	pal := palettes[0]
	img := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Box(c).Fill().Padding(100).Children(func() {
			ui.Box(c).Fill().Radius(185).Background(pal.bg).Draw(func(p *ui.Painter, r ui.Rect) {
				cx, cy := r.X+r.W/2, r.Y+r.H/2
				spans := [][3]float32{{0, .42, 0}, {.42, .7, 1}, {.7, .88, 2}, {.88, 1, 3}}
				for ring := 0; ring < 3; ring++ {
					r0 := 120 + float32(ring)*68
					for i, s := range spans {
						if ring == 2 && i%2 == 1 {
							continue
						}
						h, sat, l := pal.rampAt(s[0])
						var path ui.Path
						sector(&path, cx, cy, r0, r0+60, float32(s[0]*tau)+.03, float32(s[1]*tau)-.03, 0)
						p.FillPath(&path, hsl(h, sat, l*.8, .35))
						p.StrokePath(&path, 5, hsl(h, sat+12, l+14, .95))
					}
				}
				var g ui.Path
				p.StrokePath(circle(&g, cx, cy, 345, 0, tau).Dash(0, 6, 14), 4, pal.ink.Alpha(.35))
			})
		})
	}, 1024, 1024, 1)
	f, err := os.Create("resources/icon.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, img)
}
