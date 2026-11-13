package main

import (
	"fmt"
	"image"
	"image/png"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/21stware/tossy/engine/scan"
	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"
)

// TestReadmeShots renders the screenshots in docs/screenshots (TOSSY_README_SHOTS=dir): an
// English UI over a home folder that looks lived in. The fixture is written small and its sizes
// are scaled up in memory (scanSize bytes per KB on disk), so the shots show gigabytes without
// writing them; the disk line shows a fixed 500 GB volume.
//
//	TOSSY_README_SHOTS=$PWD/../docs/screenshots go test -run TestReadmeShots .
func TestReadmeShots(t *testing.T) {
	out := os.Getenv("TOSSY_README_SHOTS")
	if out == "" {
		t.Skip("set TOSSY_README_SHOTS")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOSSY_STATE", t.TempDir())
	t.Setenv("TOSSY_LANG", "en")
	root := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", root) // the root shows as ~
	readmeHome(t, root)

	const scale = 64 << 10 // 1 KB on disk reads as 64 MB
	diskUsageOf = func(string) (int64, int64, bool) { return 500e9, 171e9, true }
	defer func() { diskUsageOf = diskUsage }()

	a := newApp()
	a.startScan(root)
	for deadline := time.Now().Add(20 * time.Second); a.scanning; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("scan timeout")
		}
	}
	if a.t == nil {
		t.Fatal(a.scanErr)
	}
	inflate(a.t, scale)
	a.ver++
	a.set.InUseMin = 0 // the fixture was all just written
	a.set.SkipApps = false

	save := func(tt *ui.Tester, name string) {
		f, err := os.Create(filepath.Join(out, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	tt := ui.NewTester(a.view, 1280, 820)
	tt.Advance(2 * time.Second)
	tt.Frame()
	settle := func() {
		for i := 0; i < 300 && (a.anal != nil || a.trashMoving > 0 || a.fly.active() || len(a.timers) > 0); i++ {
			time.Sleep(10 * time.Millisecond)
			tt.Advance(50 * time.Millisecond)
			tt.Frame()
		}
	}
	find := func(rel string) int32 {
		n := a.t.Find(filepath.Join(root, rel))
		if n < 0 {
			t.Fatalf("%s not in the tree", rel)
		}
		return n
	}

	// 1. the disk, with the HUD callout on the largest folder of the second ring
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
	save(tt, "overview")
	tt.Move(5, 5)

	// 2. every style, as one contact sheet
	a.keepMode = true
	const cols, tw, th, gap = 5, 512, 328, 12
	rows := (len(palettes) + cols - 1) / cols
	sheet := image.NewRGBA(image.Rect(0, 0, cols*tw+(cols-1)*gap, rows*th+(rows-1)*gap))
	for i := range palettes {
		a.palIdx = i
		a.mode = modeStructure
		for k := 0; k < 8; k++ { // let power-on and refresh effects finish
			tt.Advance(500 * time.Millisecond)
			tt.Frame()
		}
		c, r := i%cols, i/cols
		dst := image.Rect(c*(tw+gap), r*(th+gap), c*(tw+gap)+tw, r*(th+gap)+th)
		draw.CatmullRom.Scale(sheet, dst, tt.Image(), tt.Image().Bounds(), draw.Src, nil)
	}
	sf, err := os.Create(filepath.Join(out, "styles.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	if err := png.Encode(sf, sheet); err != nil {
		t.Fatal(err)
	}
	a.palIdx, a.keepMode = 0, false
	tt.Advance(2 * time.Second)
	tt.Frame()

	// 3. a folder drilled into, its list in focus with a row selected
	a.zoomTo(find("projects"))
	tt.Advance(time.Second)
	tt.Frame()
	a.pf = pfList
	a.selected = find("projects/api")
	tt.Advance(time.Second)
	tt.Frame()
	save(tt, "browse")
	a.pf = pfNone
	a.zoomTo(0)
	tt.Advance(time.Second)
	tt.Frame()

	// 4. a Git repository with agent worktrees: what deleting one would lose
	w2 := find("projects/shop-worktrees/agent-2")
	a.zoomTo(w2)
	a.git.open, a.git.devO = true, true
	tt.Advance(time.Second)
	for i := 0; i < 100 && !tt.HasText("1 untracked file"); i++ {
		time.Sleep(20 * time.Millisecond)
		tt.Frame()
	}
	tt.Advance(time.Second)
	tt.Frame()
	save(tt, "git")
	a.git.open = false
	a.zoomTo(0)
	tt.Advance(time.Second)
	tt.Frame()

	// 5. Auto Clean: the analysis sweep
	if err := tt.Click(T("自动清理")); err != nil {
		t.Fatal(err, tt.Texts())
	}
	tt.Advance(900 * time.Millisecond)
	tt.Frame()
	save(tt, "analysis")
	settle()

	// 6. a personal file deleted by hand asks first
	a.requestTrash(find("Documents/Taxes"))
	tt.Advance(time.Second)
	tt.Frame()
	save(tt, "ask")
	if err := tt.Click(Tf("仍然移至%s", trashName())); err != nil {
		t.Fatal(err, tt.Texts())
	}
	settle()

	// 7. the trash: the bin, where its contents came from, a ball under the pointer
	for i := 0; i < 30; i++ { // the undo toasts time out
		tt.Advance(500 * time.Millisecond)
		tt.Frame()
	}
	tt.ClickAt(a.binS.right-10, a.binS.bottom-10)
	tt.Advance(time.Second)
	tt.Frame()
	{
		f := &a.binS
		ox, oy := f.origin()
		for _, e := range f.ents {
			if b, ok := f.pile[e.key]; ok {
				x, y := f.toHero(ox+b.x, oy+b.y)
				tt.Move(x, y)
				tt.Advance(50 * time.Millisecond)
				tt.Frame()
				break
			}
		}
	}
	save(tt, "trash")
	tt.Move(5, 5)
	tt.Frame()

	// 8. settings, in their own window
	a.openSettings()
	st := ui.NewTester(a.settingsView, 820, 580)
	st.Frame()
	save(st, "settings")
	st.Key(0, ui.KeyEscape)
	st.Frame()

}

// inflate multiplies the size of every file larger than one block by k (source files, .git
// objects and the like stay small), then re-totals the folders and re-sorts their children.
func inflate(t *scan.Tree, k int64) {
	ns := t.Nodes
	for i := range ns {
		switch {
		case ns[i].IsDir():
			ns[i].Size = 0
		case ns[i].Size > 4096: // more than one block
			ns[i].Size *= k
		}
	}
	for i := len(ns) - 1; i > 0; i-- { // parents precede their children
		ns[ns[i].Parent].Size += ns[i].Size
	}
	var buf []int32
	for i := range ns {
		buf = buf[:0]
		for c := ns[i].First; c != scan.None; c = ns[c].Next {
			buf = append(buf, c)
		}
		if len(buf) < 2 {
			continue
		}
		sort.SliceStable(buf, func(a, b int) bool { return ns[buf[a]].Size > ns[buf[b]].Size })
		ns[i].First = buf[0]
		for j := 0; j < len(buf)-1; j++ {
			ns[buf[j]].Next = buf[j+1]
		}
		ns[buf[len(buf)-1]].Next = scan.None
	}
}

// readmeHome writes the fixture: sizes are in KB (scaled up by the test).
func readmeHome(t *testing.T, root string) {
	rnd := rand.New(rand.NewSource(7))
	buf := make([]byte, 1<<20)
	rnd.Read(buf)
	write := func(rel string, kb int) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, buf[:kb<<10], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	many := func(dir, pattern string, n, lo, hi int) {
		for i := 0; i < n; i++ {
			write(filepath.Join(dir, fmt.Sprintf(pattern, i)), lo+rnd.Intn(hi-lo+1))
		}
	}

	// Xcode and simulators
	for _, p := range []string{"Atlas", "Runner", "Widgets", "Playground", "Notes"} {
		many("Library/Developer/Xcode/DerivedData/"+p+"-bqhxwzfkcmdpsa/Build/Intermediates.noindex", "obj%02d.o", 8, 4, 16)
	}
	many("Library/Developer/CoreSimulator/Devices/7F3A2C1E/data", "img%02d", 4, 10, 16)
	many("Library/Developer/CoreSimulator/Devices/C09B44D2/data", "img%02d", 4, 8, 14)
	many("Library/Caches/go-build", "%02d-d", 24, 5, 8)
	for _, app := range []string{"Slack", "Google/Chrome", "Code"} {
		many("Library/Application Support/"+app, "data%02d", 6, 5, 9)
	}

	// projects: a web app (Git, with agent worktrees), a Rust service, an ML notebook
	write("projects/shop/package.json", 1)
	many("projects/shop/src", "page%02d.tsx", 10, 1, 1)
	for i := 0; i < 40; i++ {
		many(fmt.Sprintf("projects/shop/node_modules/pkg-%02d", i), "index%d.js", 1, 5, 9)
	}
	write("projects/api/Cargo.toml", 1)
	many("projects/api/src", "mod%02d.rs", 8, 1, 1)
	many("projects/api/target/debug/deps", "lib%02d.rlib", 20, 3, 8)
	many("projects/ml/.venv/lib/python3.12/site-packages", "torch%02d.so", 8, 6, 14)
	many("projects/ml/__pycache__", "m%02d.pyc", 6, 5, 6)
	many("projects/ml/data", "batch%02d.parquet", 6, 4, 9)

	// package caches and models
	many(".npm/_cacache/content-v2", "%02d", 40, 5, 8)
	many(".cargo/registry/cache", "crate%02d.crate", 30, 5, 8)
	many(".gradle/caches/modules-2", "jar%02d", 16, 5, 9)
	many(".ollama/models/blobs", "sha256-%02d", 3, 90, 130)
	many(".cache/huggingface/hub/models--llama", "shard%02d", 2, 80, 110)

	// personal data
	many("Documents/Contracts", "contract-%02d.pdf", 12, 5, 10)
	many("Documents/Taxes", "return-%02d.pdf", 6, 5, 9)
	many("Pictures/Photos Library.photoslibrary/originals", "IMG_%04d.heic", 60, 4, 9)
	many("Music/Library", "track%02d.m4a", 20, 5, 9)
	write("Movies/Interview-raw.mov", 70)
	write("Movies/Trip-2025.mp4", 55)
	write("Movies/Screen Recording.mov", 30)
	many("Downloads", "installer-%02d.dmg", 6, 10, 22)
	write("Downloads/dataset.zip", 40)
	old := time.Now().Add(-90 * 24 * time.Hour)
	os.Chtimes(filepath.Join(root, "Downloads/dataset.zip"), old, old)

	// the repository and its worktrees (one holds untracked work)
	shop := filepath.Join(root, "projects/shop")
	g := func(dir string, args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(shop, ".gitignore"), []byte("node_modules\n.next\n"), 0o644)
	g(shop, "init", "-q", "-b", "main")
	g(shop, "add", ".")
	g(shop, "commit", "-qm", "init")
	for _, w := range []string{"agent-1", "agent-2", "agent-3"} {
		g(shop, "worktree", "add", "-q", "-b", w, filepath.Join(root, "projects/shop-worktrees", w))
		for i := 0; i < 12; i++ {
			many(fmt.Sprintf("projects/shop-worktrees/%s/node_modules/pkg-%02d", w, i), "index%d.js", 1, 5, 8)
		}
		many("projects/shop-worktrees/"+w+"/.next/cache", "chunk%02d", 4, 5, 8)
	}
	write("projects/shop-worktrees/agent-2/src/checkout.tsx", 1)
}
