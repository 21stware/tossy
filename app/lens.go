package main

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/21stware/tossy/engine/safety"
	"github.com/21stware/tossy/engine/scan"
)

// The developer's view. The disk view shows everything the scan found, folder by folder. A
// developer mostly wants one slice of that: what building software leaves behind, wherever it
// sits. This view is a lens over the same scan (scan.Tree.Lens): its first ring is the kinds
// below, the second the items of each (a project's node_modules, a tool's cache, an emulator),
// and from there on the real folders. The chart, the list, the clean button and the trash work
// on it as they do on the disk, since the lens is a tree like the scan; a.t and a.ev are
// whichever of the two is on screen, a.base and a.baseEv always the scan.

// devKind is how a kind of developer content reads: its name and what it is, in the source
// language (translated on the way to the screen, like the rules' reasons).
type devKind struct{ name, what string }

var devKindText = map[safety.DevKind]devKind{
	safety.DevDeps:     {"依赖", "装进项目里的依赖（node_modules、Pods、虚拟环境等），可用安装命令重新获取"},
	safety.DevBuild:    {"构建产物", "项目里的构建输出与打包结果（target、dist、.next 等），重新构建即可生成"},
	safety.DevCache:    {"构建缓存", "编译器与工具的缓存（DerivedData、go-build、ccache 等），只影响下一次构建的速度"},
	safety.DevPackages: {"包缓存", "包管理器的下载缓存（npm、Cargo、Go 模块、Gradle、Homebrew 等），需要时会重新下载"},
	safety.DevDevices:  {"模拟器与设备", "Android 模拟器（AVD）、iOS 模拟器与设备支持文件，删除后需重新创建或下载"},
	safety.DevVMs:      {"容器与虚拟机", "Docker、Colima 等的虚拟磁盘，含镜像与数据卷，删除前请确认不再需要"},
	safety.DevModels:   {"本地模型", "本地大模型与预训练权重（Ollama、Hugging Face 等），删除后需重新下载"},
	safety.DevAppCache: {"应用缓存", "应用留在缓存目录里的内容，删除后应用会按需重建"},
}

// devRoot names the lens's root.
const devRoot = "开发者"

// devLens is the developer's view of one scan.
type devLens struct {
	t       *scan.Tree
	ev      *safety.Evaluator
	orig    []int32                  // each node's node in the scan (scan.None for the root and the kinds)
	kind    map[int32]safety.DevKind // the kind each group node stands for
	trashed map[string]int32         // trash item id → node of the lens, for restores
}

// buildLens groups the scan's developer content by kind.
func (a *app) buildLens() *devLens {
	t, ev := a.base, a.baseEv
	items := map[safety.DevKind][]scan.Item{}
	rules := map[int32]*safety.Rule{}
	level := map[safety.DevKind]safety.Level{}
	for _, it := range ev.DevItems(0) {
		if t.Nodes[it.Node].Size == 0 {
			continue
		}
		items[it.Kind] = append(items[it.Kind], scan.Item{Node: it.Node, Name: devItemName(t, it)})
		rules[it.Node] = it.Rule
		level[it.Kind] = max(level[it.Kind], it.Rule.Level)
	}
	var groups []scan.Group
	byName := map[string]safety.DevKind{}
	for _, k := range safety.DevKinds() {
		groups = append(groups, scan.Group{Name: devKindText[k].name, Items: items[k]})
		byName[devKindText[k].name] = k
	}
	l, orig := t.Lens(devRoot, groups)
	d := &devLens{t: l, orig: orig, kind: map[int32]safety.DevKind{}, trashed: map[string]int32{}}
	own := map[int32]*safety.Rule{}
	for g := l.Nodes[0].First; g != scan.None; g = l.Nodes[g].Next {
		k := byName[l.Name(g)]
		d.kind[g] = k
		// a kind is as removable as the least removable thing in it
		own[g] = &safety.Rule{ID: "dev-" + k.String(), Level: level[k], Reason: devKindText[k].what, Tag: devKindText[k].name}
		for c := l.Nodes[g].First; c != scan.None; c = l.Nodes[c].Next {
			own[c] = rules[orig[c]]
		}
	}
	d.ev = ev.Lens(l, orig, own)
	return d
}

// devItemName is an item's name in its kind's list. A folder that a rule knows by its place is
// named with the folder it sits in, "web/node_modules" or ".gradle/caches": there may be forty
// node_modules. A cache folder's own name already says whose it is.
func devItemName(t *scan.Tree, it safety.DevItem) string {
	name := t.Name(it.Node)
	p := t.Nodes[it.Node].Parent
	if p <= 0 || it.Rule.ID == "user-caches" || it.Rule.ID == "xdg-cache" {
		return name
	}
	if home := homeDir(); home != "" && t.Path(p) == home {
		return name
	}
	return t.Name(p) + "/" + name
}

// lensOn is the lens while it is the tree on screen.
func (a *app) lensOn() *devLens {
	if a.dev != nil && a.t == a.dev.t {
		return a.dev
	}
	return nil
}

// virtual says whether n is one of the lens's own folders (its root or a kind), not something
// on disk.
func (a *app) virtual(n int32) bool { return a.t != nil && a.t.Virtual(n) }

// baseOf is node n of the tree on screen as the scan knows it (scan.None for a virtual one).
func (a *app) baseOf(n int32) int32 {
	if d := a.lensOn(); d != nil {
		return d.orig[n]
	}
	return n
}

// pathOrName is n's path, or the name of a node that has none.
func (a *app) pathOrName(n int32) string {
	if p := a.t.Path(n); p != "" {
		return p
	}
	return a.name(n)
}

// setDev switches between the disk view and the developer's.
func (a *app) setDev(on bool) {
	if on == a.devOn || a.anal != nil {
		return
	}
	back := int32(0)
	if !on {
		back = a.diskFocus
	} else if a.diskFocus = 0; a.base != nil && a.t == a.base {
		a.diskFocus = a.focus // the folder to come back to
	}
	a.devOn = on
	if on {
		a.set.View = "dev"
	} else {
		a.set.View = ""
	}
	if a.base == nil {
		return // the first scan is still running: its tree comes up in this view
	}
	a.showView()
	a.pf = pfNone
	a.list.ScrollTo(0, ui.Start)
	if !on && back > 0 && a.t.Attached(back) && a.t.Nodes[back].IsDir() {
		a.focus, a.selected = back, back
	}
	if a.scene == sceneTrash {
		a.goScene(sceneBrowse)
	}
	if a.motion() { // the rings grow out again, the rows slide in
		a.sun.intro = a.now
		a.rowsT0, a.rowsDx = a.now, 1
		if !on {
			a.rowsDx = -1
		}
	}
	a.frame = true
}

// showView puts the chosen view's tree on screen, at its root.
func (a *app) showView() {
	if a.devOn {
		if a.dev == nil {
			a.dev = a.buildLens()
		}
		a.t, a.ev = a.dev.t, a.dev.ev
	} else {
		a.t, a.ev = a.base, a.baseEv
	}
	a.focus, a.selected, a.hover, a.hoverAgg = 0, 0, scan.None, nil
	a.ver++
	a.sun.reset()
	a.sun.prev, a.sun.cur = nil, view{}
	a.rows, a.flip = nil, nil
	a.drag = dragState{}
}

// place is where the view is, in terms that outlive the tree: a path on disk, or a kind.
type place struct {
	path string
	kind safety.DevKind
}

func (a *app) placeOf(n int32) place {
	if a.t == nil || n <= 0 {
		return place{}
	}
	if d := a.lensOn(); d != nil && a.t.Virtual(n) {
		return place{kind: d.kind[n]}
	}
	return place{path: a.t.Path(n)}
}

// find is the node at p in the tree on screen, scan.None if it is not there.
func (a *app) find(p place) int32 {
	switch {
	case p.path != "":
		return a.t.Find(p.path)
	case p.kind != safety.DevNone:
		if d := a.lensOn(); d != nil {
			for g, k := range d.kind {
				if k == p.kind && a.t.Attached(g) {
					return g
				}
			}
		}
	}
	return scan.None
}

// relens rebuilds the lens after the scan changed in a way the lens did not follow, keeping
// the folder in focus where it can.
func (a *app) relens() {
	if !a.devOn || a.base == nil {
		a.dev = nil // rebuilt when the developer's view next comes up
		return
	}
	focus, sel := a.placeOf(a.focus), a.placeOf(a.selected)
	a.dev = nil
	a.showView()
	if n := a.find(focus); n != scan.None && a.t.Nodes[n].IsDir() {
		a.focus, a.selected = n, n
		if m := a.find(sel); m != scan.None && a.isUnder(m, n) {
			a.selected = m
		}
	}
}

// detach takes node n of the tree on screen out of it: out of the scan as well when the lens
// shows it, with a kind it leaves empty. It returns the scan's node.
func (a *app) detach(n int32) int32 {
	d := a.lensOn()
	if d == nil {
		a.t.Remove(n)
		a.dev = nil // the lens no longer matches the scan
		return n
	}
	b := d.orig[n]
	d.t.Remove(n)
	if g := d.t.Nodes[n].Parent; g > 0 && d.t.Virtual(g) && d.t.Nodes[g].First == scan.None {
		d.t.Remove(g)
	}
	if b != scan.None {
		a.base.Remove(b)
	}
	return b
}

// reattach puts a node of the lens back, and its kind first if that went with it.
func (d *devLens) reattach(n int32) {
	if d.t.Attached(n) {
		return
	}
	if g := d.t.Nodes[n].Parent; g > 0 && d.t.Virtual(g) && !d.t.Attached(g) {
		d.t.Reattach(g)
	}
	d.t.Reattach(n)
}

// putBack shows trash item id in the trees again, once it is out of the trash. It returns the
// scan's node, or false when this scan never held the item.
func (a *app) putBack(id string) (int32, bool) {
	n, found := a.trashed[id]
	if !found || a.base == nil || a.base.Attached(n) {
		return scan.None, false
	}
	delete(a.trashed, id)
	a.base.Reattach(n)
	a.ver++
	if d := a.dev; d != nil {
		if l, ok := d.trashed[id]; ok {
			delete(d.trashed, id)
			d.reattach(l)
		} else {
			a.lensStale = true // it went before this lens was built: only a rebuild can place it
		}
	}
	return n, true
}

// syncLens rebuilds a lens that putBack left behind the scan.
func (a *app) syncLens() {
	if a.lensStale {
		a.lensStale = false
		a.relens()
	}
}

// ------------------------------------------------------------------------------------------
// on screen

const (
	icoDisk = "M12 3.5C16.7 3.5 20.5 7.3 20.5 12C20.5 16.7 16.7 20.5 12 20.5C7.3 20.5 3.5 16.7 3.5 12C3.5 7.3 7.3 3.5 12 3.5ZM12 3.5V12H20.5"
	icoCode = "M8.5 7L3.5 12l5 5M15.5 7l5 5-5 5M13.5 5l-3 14"
)

// viewSwitch is the pair of tabs beside the clean button: the whole disk, or the developer's
// slice of it. A slim window shows the icons alone.
func (a *app) viewSwitch(c *ui.Context) ui.Element {
	pal := a.pal
	slim := a.sz == szCompact
	rad := max(pal.radius, 4) + 3
	track := ui.Row(c.Key("vsw")).Padding(2).Gap(2).Radius(rad).Background(pal.panel2).Shrink(0)
	if pal.radius == 0 { // technical styles: a ruled strip
		rad = 2
		track.Radius(0).Background(ui.Transparent).Border(1, pal.ink.Alpha(.35))
	}
	track.Children(func() {
		for _, it := range []struct {
			key, ico, label, tip string
			dev                  bool
		}{
			{"disk", icoDisk, T("磁盘"), Tf("磁盘视角：扫描范围里的全部内容（%s 切换）", devShortcut()), false},
			{"dev", icoCode, T(devRoot), Tf("开发者视角：只看依赖、构建产物、缓存、模拟器（%s 切换）", devShortcut()), true},
		} {
			sel := it.dev == a.devOn
			col := pal.t3
			if sel {
				col = pal.text
			}
			s := ui.Row(c.Key(it.key)).Height(26).Padding(0, 9).Gap(5).Radius(rad - 2).AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(it.tip)
			if slim {
				s.Padding(0, 7)
			}
			switch {
			case sel && pal.radius == 0:
				s.Background(pal.ink.Alpha(.12))
			case sel:
				s.Background(pal.bg).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, .12))
			case s.Hovered():
				col = pal.t2
				s.Background(pal.hov)
			}
			s.Children(func() {
				icon(c, it.ico, 15, col, 1.7)
				if !slim {
					t := ui.Text(c, it.label).FontSize(12.5).TextColor(col).SingleLine()
					if sel {
						t.FontWeight(600)
					}
				}
			})
			if s.Clicked() {
				a.setDev(it.dev)
			}
		}
	})
	return track
}

// kindPanel sits under the list's header in the developer's view: what the view, or the kind
// in focus, is, and a button that cleans what is safe in the kind.
func (a *app) kindPanel(c *ui.Context, n int32) {
	pal := a.pal
	pl := float32(18)
	if a.slim {
		pl = 16
	}
	ui.Column(c.Key("kinfo")).Gap(8).Padding(0, pl, 8, pl).Shrink(0).Children(func() {
		what := T("开发留下的可重建内容，按类别归拢：依赖、构建产物、各类缓存、模拟器与虚拟机。")
		empty := a.t.Nodes[0].First == scan.None
		if n != 0 {
			what = T(a.verdict(n).Reason())
		} else if empty {
			what = T("这次扫描里没有发现开发相关的内容。")
		}
		if !a.slim || empty { // the slim column keeps its room for the names
			ui.Text(c, what).FontSize(12).TextColor(pal.t3)
		}
		if n == 0 {
			return
		}
		items, size := a.kindPlan(n)
		if len(items) == 0 {
			return
		}
		label := Tf("清理可安全清理的 %s", fmtSize(size))
		if a.slim {
			label = Tf("清理 %s", fmtSize(size))
		}
		ui.Row(c).Children(func() {
			b := a.btn(c.Key("kclean"), label, btnSolid, false, nil).Disabled(a.anal != nil)
			b.Tooltip(Tf("把这一类里可重建的 %s 移到%s，清空前都可以还原", nItems(int64(len(items))), trashName()))
			if b.Clicked() {
				a.freeUp(items)
			}
		})
	})
}

// kindPlan is what the clean button of the kind in focus takes: its safe items not in use.
func (a *app) kindPlan(n int32) (items []cleanItem, size int64) {
	bm := a.busyMap()
	for _, it := range a.cleanPlan() {
		if bm[it.node] == "" && a.isUnder(it.node, n) {
			items = append(items, it)
			size += it.meta.Size
		}
	}
	return
}

// devShortcut is the key that flips the view, as the platform writes it.
func devShortcut() string { return strings.Replace(settingsKey(), ",", "D", 1) }
