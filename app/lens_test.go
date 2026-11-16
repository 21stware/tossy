package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/21stware/tossy/engine/safety"
	"github.com/21stware/tossy/engine/scan"
)

// devFixture is the fixture with more of what development leaves behind.
func devFixture(t testing.TB) string {
	root := fixture(t)
	write := func(rel string, kb int) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, make([]byte, kb<<10), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("projects/api/Cargo.toml", 1)
	write("projects/web/package.json", 1)
	write("projects/web/.next/cache/a.pack", 900)
	write("projects/web/dist/app.js", 300)
	write("projects/ml/.venv/lib/torch.so", 2500)
	write(".android/avd/Pixel_8.avd/userdata.img", 5000)
	write(".android/avd/Tablet.avd/userdata.img", 2000)
	write(".cache/huggingface/hub/model.safetensors", 4000)
	write("Library/Developer/Xcode/DerivedData/App-abc/Build/a.o", 1200)
	write("Library/Developer/Xcode/DerivedData/Lib-def/Build/b.o", 700)
	return root
}

func waitMoves(t *testing.T, a *app, tt *ui.Tester) {
	t.Helper()
	for i := 0; i < 300 && (a.anal != nil || a.trashMoving > 0 || a.fly.active() || len(a.timers) > 0); i++ {
		tt.Advance(100 * time.Millisecond)
		tt.Frame()
		time.Sleep(5 * time.Millisecond)
	}
	if a.trashMoving > 0 {
		t.Fatal("the moves to the trash did not finish")
	}
}

// kindNode is the lens's node for kind k (scan.None if the lens has none of it).
func kindNode(a *app, k safety.DevKind) int32 {
	return a.find(place{kind: k})
}

// The developer's view: the scan regrouped by kind, cleaned and restored in step with the disk
// view.
func TestDevView(t *testing.T) {
	root := devFixture(t)
	a := loadedAt(t, root)
	a.set.InUseMin, a.set.SkipApps = 0, false // the fixture's files were all just written
	tt := ui.NewTester(a.view, 1280, 820)
	tt.Advance(2 * time.Second)
	tt.Frame()
	if a.devOn || a.t != a.base || !tt.HasText("开发者") || !tt.HasText("磁盘") {
		t.Fatalf("the disk view with its switch: devOn=%v %v", a.devOn, tt.Texts())
	}
	diskPlan, _ := a.idlePlan()

	// the switch: the kinds are the first ring and the list
	if err := tt.Click("开发者"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(2 * time.Second)
	tt.Frame()
	if !a.devOn || a.lensOn() == nil || !a.t.IsLens() || a.focus != 0 {
		t.Fatalf("after the switch: devOn=%v lens=%v focus=%d", a.devOn, a.lensOn() != nil, a.focus)
	}
	if a.set.View != "dev" {
		t.Errorf("the view is not remembered: %q", a.set.View)
	}
	shot(t, tt, "dev-1-kinds")
	for _, s := range []string{"依赖", "构建产物", "构建缓存", "包缓存", "模拟器与设备", "本地模型"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in the developer's view: %v", s, tt.Texts())
		}
	}
	var sum int64
	for _, it := range a.baseEv.DevItems(0) {
		sum += it.Size
	}
	if got := a.t.Nodes[0].Size; got != sum || got >= a.base.Nodes[0].Size {
		t.Errorf("the lens holds %d, the developer items %d, the scan %d", got, sum, a.base.Nodes[0].Size)
	}
	if len(a.sun.arcs) == 0 {
		t.Error("no arcs in the developer's view")
	}
	// what the big button takes here is the developer's share of what it takes on the disk
	devPlan, _ := a.idlePlan()
	want := map[string]bool{}
	for _, it := range diskPlan {
		if p := a.base.Path(it.node); a.t.Find(p) != scan.None {
			want[p] = true
		}
	}
	for _, it := range devPlan {
		p := a.t.Path(it.node)
		if !want[p] {
			t.Errorf("the developer's plan takes %s, which the disk's does not", p)
		}
		delete(want, p)
	}
	if len(want) > 0 || len(devPlan) == 0 || len(devPlan) >= len(diskPlan) {
		t.Errorf("plans: %d on the disk, %d in the developer's view, missing %v", len(diskPlan), len(devPlan), want)
	}

	// a kind is not a folder: it opens, but cannot go to the trash
	devices := kindNode(a, safety.DevDevices)
	if devices == scan.None {
		t.Fatal("no emulators kind")
	}
	if ok, why := a.canTrash(devices); ok || why == "" {
		t.Error("a kind can be trashed")
	}
	if a.t.Path(devices) != "" || a.name(devices) != "模拟器与设备" {
		t.Errorf("kind: path %q name %q", a.t.Path(devices), a.name(devices))
	}
	if lv := a.verdict(devices).Level(); lv != safety.Review {
		t.Errorf("emulators are %v", lv)
	}

	// into a kind with safe items: its button cleans them, here and in the scan
	deps := kindNode(a, safety.DevDeps)
	a.zoomTo(deps)
	tt.Advance(time.Second)
	tt.Frame()
	shot(t, tt, "dev-2-kind")
	if !tt.HasText("web/node_modules") || !tt.HasText("ml/.venv") {
		t.Errorf("the dependencies: %v", tt.Texts())
	}
	items, size := a.kindPlan(deps)
	tt.ClickAt(a.rightX()+40, 200) // the list takes the focus and widens: the kind says what it is
	tt.Advance(time.Second)
	tt.Frame()
	if len(items) != 1 || !tt.HasText("清理可安全清理的 "+fmtSize(size)) || !tt.HasText(devKindText[safety.DevDeps].what) {
		t.Fatalf("the kind's clean button: %d items, %v", len(items), tt.Texts())
	}
	shot(t, tt, "dev-2b-kind-wide")
	nm := filepath.Join(root, "projects/web/node_modules")
	baseNM := a.base.Find(nm)
	before, baseBefore := a.t.Nodes[0].Size, a.base.Nodes[0].Size
	if err := tt.Click("清理可安全清理的 " + fmtSize(size)); err != nil {
		t.Fatal(err)
	}
	waitMoves(t, a, tt)
	if a.t.Find(nm) != scan.None && a.t.Attached(a.t.Find(nm)) {
		t.Error("node_modules is still in the lens")
	}
	if a.base.Attached(baseNM) {
		t.Error("node_modules is still in the scan")
	}
	if a.t.Nodes[0].Size != before-size || a.base.Nodes[0].Size != baseBefore-size {
		t.Errorf("sizes after the clean: lens %d (was %d), scan %d (was %d), cleaned %d", a.t.Nodes[0].Size, before, a.base.Nodes[0].Size, baseBefore, size)
	}
	if a.focus != deps || !a.t.Attached(deps) {
		t.Errorf("the kind still holds the virtualenv: focus %d", a.focus)
	}
	shot(t, tt, "dev-3-cleaned")

	// the virtualenv by hand: the kind is empty and goes with it
	venv := a.t.Find(filepath.Join(root, "projects/ml/.venv"))
	baseVenv := a.base.Find(filepath.Join(root, "projects/ml/.venv"))
	a.requestTrash(venv)
	waitMoves(t, a, tt)
	if a.t.Attached(deps) || a.focus != 0 {
		t.Errorf("the emptied kind is still there: attached %v, focus %d", a.t.Attached(deps), a.focus)
	}
	if baseVenv == scan.None || a.base.Attached(baseVenv) {
		t.Error("the virtualenv is still in the scan")
	}

	// restoring puts both back, in both trees, and the kind with them
	var ids []string
	for _, it := range a.bin.List() {
		ids = append(ids, it.ID)
	}
	if len(ids) != 2 {
		t.Fatalf("%d items in the trash", len(ids))
	}
	a.restore(ids)
	tt.Advance(time.Second)
	tt.Frame()
	if !a.t.Attached(deps) || !a.t.Attached(venv) || a.t.Nodes[0].Size != before || a.base.Nodes[0].Size != baseBefore || !a.base.Attached(baseNM) || !a.base.Attached(baseVenv) {
		t.Errorf("after the restore: kind %v, lens %d (want %d), scan %d (want %d)", a.t.Attached(deps), a.t.Nodes[0].Size, before, a.base.Nodes[0].Size, baseBefore)
	}

	// back on the disk: what is deleted there is gone from the developer's view too
	if err := tt.Click("磁盘"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(2 * time.Second)
	tt.Frame()
	if a.devOn || a.t != a.base || a.set.View != "" {
		t.Fatal("not back in the disk view")
	}
	a.requestTrash(baseNM)
	waitMoves(t, a, tt)
	tt.Key(ui.Cmd, ui.KeyD)
	tt.Advance(2 * time.Second)
	tt.Frame()
	if !a.devOn || a.t.Find(nm) != scan.None {
		t.Errorf("the developer's view after a delete on the disk: devOn=%v, node_modules %d", a.devOn, a.t.Find(nm))
	}
	// and a restore of it, with the lens on screen, brings it back there
	a.restore([]string{a.bin.List()[0].ID})
	tt.Frame()
	if n := a.t.Find(nm); n == scan.None || !a.t.Attached(n) || a.t.Nodes[0].Size != before {
		t.Errorf("the lens after restoring what the disk view deleted: node %d, size %d (want %d)", n, a.t.Nodes[0].Size, before)
	}

	// a rescan comes up in the same view, at the kind in focus
	a.zoomTo(kindNode(a, safety.DevDevices))
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.HasText(".android/avd") {
		t.Errorf("the emulators: %v", tt.Texts())
	}
	shot(t, tt, "dev-4-devices")
	nt, err := scan.Scan(t.Context(), root, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a.setTree(nt, nil)
	tt.Advance(time.Second)
	tt.Frame()
	if !a.devOn || !a.t.IsLens() || a.base != nt || a.lensOn().kind[a.focus] != safety.DevDevices {
		t.Errorf("after a rescan: devOn=%v lens=%v focus=%d", a.devOn, a.t.IsLens(), a.focus)
	}
}

// The developer's view at every size and in every style.
func TestDevViewSizes(t *testing.T) {
	a := loadedAt(t, devFixture(t))
	for _, sz := range [][2]int{{720, 620}, {1000, 700}, {1600, 900}} {
		tt := ui.NewTester(a.view, sz[0], sz[1])
		a.setDev(false)
		tt.Advance(2 * time.Second)
		tt.Frame()
		a.setDev(true)
		tt.Advance(2 * time.Second)
		tt.Frame()
		tt.Frame()
		if !a.t.IsLens() || len(a.sun.arcs) == 0 {
			t.Errorf("%v: no developer's view", sz)
		}
		shot(t, tt, fmt.Sprint("dev-size-", sz[0]))
	}
	tt := ui.NewTester(a.view, 1280, 820)
	for i, p := range palettes {
		a.palIdx = i
		tt.Advance(time.Second)
		tt.Frame()
		tt.Frame()
		shot(t, tt, "dev-style-"+p.name)
	}
}
