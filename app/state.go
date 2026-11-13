package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/fanfree/fanfree/engine/habit"
	"github.com/fanfree/fanfree/engine/safety"
	"github.com/fanfree/fanfree/engine/scan"
	"github.com/fanfree/fanfree/engine/trash"
)

const (
	modeStructure = iota
	modeSafety
)

const (
	sortSize = iota
	sortName
	sortAge
)

// app is the window's state. The view reads it on the main thread; background work changes it
// through win.Update.
type app struct {
	win    *mygo.Window
	setWin *mygo.Window // the settings window while open
	pal    *palette

	// scan
	root     string
	scanning bool
	prog     *scan.Progress
	cancel   context.CancelFunc
	scanErr  error

	t                *scan.Tree
	ev               *safety.Evaluator
	tsun             trashSun // the trash scene's chart
	rightSp          spring   // the right column's width: none before the first scan, wider when a panel has focus
	rightW           float32
	rightTo          float32
	rightEnter       bool // pushing in for the first time: the content keeps its final width
	shown            bool
	pf               int   // which panel has focus: pfNone, pfList or pfDetail (the trash is the scene)
	selOn            bool  // something below the focus is selected (last frame)
	selLast          int32 // the selection last frame
	rightHL          float32
	wL, wD, wT       float32 // each panel's width this frame
	wTto             float32
	wLsp, wDsp, wTsp spring
	sz               int       // window size class
	slim             bool      // the list in its short form (it is narrow: something else has focus)
	slimT0           time.Time // the panels changed form
	fileOn           bool      // the panel shows a file's detail, not the list
	fileT0           time.Time // when it last switched between the two
	detNode          int32     // the file the detail shows (kept while it slides out)
	askSp            spring
	tvSp             spring
	winW             float32
	chartR           ui.Rect // the chart, last frame (the bin sits in its corner)
	hTsp             spring  // the trash panel height; the list takes the rest
	tvTw             tween   // the left side's trash-scene state
	tvK              float32
	rightW0          float32 // the right column's width when the trash view began to open
	safeR            float32 // the trash view's safety block, from the window's right edge
	safeH            float32 // and its height, last frame
	tvOn             bool
	keepMode         bool      // tests: hold a.mode as set
	burstT0          time.Time // the clean button's press
	sheenOn          bool      // the big button: hovered last frame, and when its sheen started
	sheenT0          time.Time
	dustHot          bool // the big button is hovered: its glitter dust drifts
	dustK            tween
	dustT0           time.Time
	upOn             bool
	upT0             time.Time

	ver int // bumped on every tree change, invalidates cached geometry and rows

	bin   *trash.Bin
	habit *habit.Store

	// navigation
	focus, selected int32
	hover           int32 // node under the pointer (chart or list), or scan.None
	hoverAgg        *arc  // aggregated tail under the pointer
	centerHot       bool
	mode            int
	sortBy          int

	sun   sunburst
	rows  []int32 // children of focus, sorted
	rowsV int     // ver + focus + sort the rows were built for
	rowsK [3]int
	list  ui.ListState

	// clean
	plan        []cleanItem
	planV       int
	freedTotal  int64
	trashMoving int              // moves in flight (background)
	emptying    bool             // Empty is deleting files (background)
	trashed     map[string]int32 // trash item id → node of this scan, for restores

	fly       flight
	binS      binState
	throwRow  ui.Rect // the list row a manual delete starts from
	throwNode int32

	now     time.Time // this frame's time, set by the view
	lastPal *palette
	uiTheme *ui.Theme
	palIdx  int
	disk    struct {
		ver         int
		total, free int64
		ok          bool
	}
	scanStart time.Time
	scanTxt   scanText // the path readout and blips while scanning
	rescan    rescanFX // a rescan's play on the sunburst on screen

	set, saved   settings
	showSettings bool
	setTab       int // settings category on show
	groupN       int // entries drawn so far in the settings group being built
	scene        int
	anal         *analysis

	timers  []timer
	procs   []string // running programs, lower case
	procsV  int
	busy    map[int32]string
	busyK   [4]int
	pulse   time.Time // the analysis finished
	radar   time.Time // 查看评估 sweep
	panelT0 time.Time // the right panel changed scene
	frame   bool      // something animates: ask for the next frame

	rowsT0       time.Time // the list changed folder
	rowsDx       float32
	flip         map[int32]float32 // rows' old index minus new, NaN for new rows (FLIP)
	flipT0       time.Time
	diskU, diskT tween
	drag         dragState
	drop         dropSrc
	shakeN       int32
	shakeT0      time.Time
	ask          *askState
	askOut       *askState // closing: shrinks back into its corner
	askOutT0     time.Time
	askTw        tween // the right panel quiets while a confirm is open
	askK         float32
	git          gitState
	compOpen     bool // 构成与最大的文件
	prevShut     bool // 预览 (open by default)
	whyOpen      bool
	comp         compCache
	prev         previewCache
	sceneT0      time.Time // the scene switch began (outgoing stagger)
	sceneNext    int

	// stats for the footer
	drawMs   float64
	fx       themeFX
	arcCount int
}

// cleanItem is one thing auto-clean will take.
type cleanItem struct {
	node int32
	meta trash.Meta
	why  string
}

func newApp() *app {
	a := &app{pal: palettes[0], hover: scan.None, focus: 0, selected: 0}
	state := stateDir()
	var err error
	if a.bin, err = trash.Open(state); err != nil {
		fmt.Fprintln(os.Stderr, "trash journal:", err)
	}
	if a.habit, err = habit.Load(filepath.Join(state, "habits.json")); err != nil {
		a.habit, _ = habit.Load(filepath.Join(os.TempDir(), "fanfree-habits.json"))
	}
	a.loadSettings()
	if t := os.Getenv("FANFREE_THEME"); t != "" {
		if i, _ := paletteByName(t); i >= 0 {
			a.palIdx = i
		}
	}
	a.pal = palettes[a.palIdx]
	return a
}

func stateDir() string {
	if d := os.Getenv("FANFREE_STATE"); d != "" {
		return d
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "FanFree")
}

// startScan scans root in the background. done runs on the main thread (via win.Update, or
// directly when there is no window, as in tests).
func (a *app) startScan(root string) {
	if a.cancel != nil {
		a.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.root, a.scanning, a.scanErr = root, true, nil
	a.prog = &scan.Progress{}
	a.scanStart = time.Now()
	a.scanTxt, a.rescan = scanText{}, rescanFX{}
	prog := a.prog
	opt := scan.Options{Progress: prog, Exclude: a.excludes(), CrossDevice: a.set.CrossDevice}
	a.anal, a.scene = nil, sceneBrowse
	a.sceneNext, a.sceneT0 = a.sceneNext+1, time.Time{}
	go func() {
		t, err := scan.Scan(ctx, root, opt)
		if ctx.Err() != nil {
			return
		}
		a.onMain(func() { a.setTree(t, err) })
	}()
}

func (a *app) onMain(fn func()) {
	if a.win != nil {
		a.win.Update(fn)
	} else {
		fn()
	}
}

func (a *app) setTree(t *scan.Tree, err error) {
	a.scanning = false
	a.scanErr = err
	if t == nil {
		return
	}
	// a rescan switches straight to the new data, at the folder that was in focus: the intro
	// plays only for the first tree
	focus := int32(0)
	first := a.t == nil
	if !first && a.focus != 0 {
		if n := t.Find(a.t.Path(a.focus)); n != scan.None && t.Nodes[n].IsDir() {
			focus = n
		}
	}
	a.hideMarked(t)
	a.t, a.ev = t, safety.New(t)
	a.forgetGit()
	a.focus, a.selected, a.hover = focus, focus, scan.None
	a.ver++
	a.sun.reset()
	if first && a.motion() {
		a.sun.intro = time.Now()
	}
	a.crtOn()
	a.refreshProcs(nil)
}

// --------------------------------------------------------------------------------------------
// tree queries

func (a *app) name(i int32) string {
	if i == 0 {
		return displayRoot(a.t.Root)
	}
	return a.t.Name(i)
}

func displayRoot(p string) string {
	if home, err := os.UserHomeDir(); err == nil && p == home {
		return "~"
	}
	if b := filepath.Base(p); b != "" && b != "/" && b != `\` && b != "." {
		return b
	}
	return p
}

func (a *app) depth(i int32) int {
	d := 0
	for i > 0 {
		i = a.t.Nodes[i].Parent
		d++
	}
	return d
}

// chain from the root to i.
func (a *app) chain(i int32) []int32 {
	var out []int32
	for {
		out = append(out, i)
		if i == 0 {
			break
		}
		i = a.t.Nodes[i].Parent
	}
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out
}

func (a *app) children() []int32 {
	k := [3]int{a.ver, int(a.focus), a.sortBy}
	if a.rows != nil && a.rowsK == k {
		return a.rows
	}
	// same folder, different rows (delete / restore / re-sort): remember where each row was
	var old map[int32]int
	if a.rows != nil && a.rowsK[1] == k[1] && a.motion() {
		old = make(map[int32]int, len(a.rows))
		for i, n := range a.rows {
			old[n] = i
		}
	}
	a.rowsK = k
	a.rows = a.t.Children(a.focus, a.rows[:0])
	defer func() {
		if old == nil {
			return
		}
		a.flip = make(map[int32]float32, len(a.rows))
		for i, n := range a.rows {
			if j, ok := old[n]; ok {
				a.flip[n] = float32(j - i)
			} else {
				a.flip[n] = float32(math.NaN()) // new: fades in
			}
		}
		a.flipT0 = a.now
	}()
	switch a.sortBy {
	case sortName:
		sort.SliceStable(a.rows, func(i, j int) bool {
			return strings.ToLower(a.t.Name(a.rows[i])) < strings.ToLower(a.t.Name(a.rows[j]))
		})
	case sortAge:
		sort.SliceStable(a.rows, func(i, j int) bool { return a.t.Nodes[a.rows[i]].MTime > a.t.Nodes[a.rows[j]].MTime })
	}
	return a.rows
}

func (a *app) verdict(i int32) safety.Verdict { return a.ev.Verdict(i) }

// --------------------------------------------------------------------------------------------
// navigation

func (a *app) zoomTo(i int32) {
	if a.t == nil {
		return
	}
	if !a.t.Nodes[i].IsDir() || a.t.Nodes[i].First == scan.None {
		if i != 0 {
			a.selected = i
			i = a.t.Nodes[i].Parent
		}
	}
	if i == a.focus {
		return
	}
	a.sun.zoomFrom(a)
	a.inkRefresh(false)
	a.rowsT0, a.rowsDx = a.now, 1
	if a.isUnder(a.focus, i) {
		a.rowsDx = -1 // going up: rows come in from the left
	}
	a.focus = i
	if !a.isUnder(a.selected, i) || a.selected == i {
		a.selected = i
	}
	a.hover, a.hoverAgg = scan.None, nil
	a.list.ScrollTo(0, ui.Start)
}

func (a *app) isUnder(i, anc int32) bool {
	for {
		if i == anc {
			return true
		}
		if i <= 0 {
			return false
		}
		i = a.t.Nodes[i].Parent
	}
}

// --------------------------------------------------------------------------------------------
// removing and restoring

// canTrash says whether the user may move i to the trash, and why not.
func (a *app) canTrash(i int32) (bool, string) {
	if i <= 0 {
		return false, T("扫描根目录不能删除")
	}
	n := &a.t.Nodes[i]
	if n.Flags&scan.FlagCloud != 0 {
		return false, T("云端占位文件，删除可能同步删除云端数据")
	}
	if a.verdict(i).Level() == safety.Protected {
		return false, Tf("系统保护：%s", T(a.verdict(i).Reason()))
	}
	return true, ""
}

// hideMarked takes what is in FanFree's trash out of a fresh scan: a trashed item is only a mark
// and its file is still on disk, so the scanner found it again. Deepest first, so a mark inside a
// marked folder is still reachable when its turn comes; restoring one reattaches it.
func (a *app) hideMarked(t *scan.Tree) {
	a.trashed = nil
	if a.bin == nil {
		return
	}
	items := a.bin.List()
	sort.Slice(items, func(i, j int) bool { return len(items[i].Orig) > len(items[j].Orig) })
	for _, it := range items {
		if n := t.Find(it.Orig); n > 0 {
			t.Remove(n)
			if a.trashed == nil {
				a.trashed = map[string]int32{}
			}
			a.trashed[it.ID] = n
		}
	}
}

// trashNodes removes nodes from the tree at once (the chart and list update in this frame),
// then marks them in FanFree's trash in the background (files stay put; nothing goes to the
// system trash). A failed mark puts the node back.
func (a *app) trashNodes(items []cleanItem, many bool) *batch {
	b := &batch{}
	if a.bin == nil || len(items) == 0 {
		return b
	}
	if a.trashFX() {
		a.throw(items, many)
	}
	a.chartMorph(400 * time.Millisecond)
	type job struct {
		node int32
		path string
		meta trash.Meta
	}
	jobs := make([]job, 0, len(items))
	for _, it := range items {
		jobs = append(jobs, job{it.node, a.t.Path(it.node), it.meta})
	}
	for _, it := range items {
		a.t.Remove(it.node)
		if it.meta.Source == trash.ByManual && a.set.Habits {
			a.learn(it.node, 1, 0)
		}
	}
	a.ver++
	if !a.t.Attached(a.focus) {
		a.focus = a.t.Nodes[items[0].node].Parent
		for !a.t.Attached(a.focus) {
			a.focus = a.t.Nodes[a.focus].Parent
		}
	}
	if !a.t.Attached(a.selected) {
		a.selected = a.focus
	}
	a.hover, a.hoverAgg = scan.None, nil
	a.trashMoving += len(jobs)
	bin := a.bin
	go func() {
		var failed []int32
		var errs []string
		moved := map[string]int32{}
		for _, j := range jobs {
			if it, err := bin.Move(j.path, j.meta); err != nil {
				failed = append(failed, j.node)
				errs = append(errs, err.Error())
			} else {
				moved[it.ID] = j.node
			}
		}
		a.onMain(func() {
			a.trashMoving -= len(jobs)
			done := map[string]bool{}
			for _, j := range jobs {
				done[j.path] = true
			}
			keep := a.binS.pend[:0]
			for _, p := range a.binS.pend {
				if !done[p.key] {
					keep = append(keep, p)
				}
			}
			a.binS.pend = keep
			if a.trashed == nil {
				a.trashed = map[string]int32{}
			}
			for id, n := range moved {
				a.trashed[id] = n
				b.ids = append(b.ids, id)
			}
			b.done = true
			if b.undo {
				a.restore(b.ids)
			}
			if len(failed) > 0 {
				a.chartMorph(420 * time.Millisecond)
				for _, n := range failed {
					a.t.Reattach(n)
				}
				a.ver++
				a.toast(ui.Toast{Title: Tf("%d 项未能移到废纸篓", len(failed)), Description: firstLine(errs[0]), Type: "error"})
			}
			if a.habit != nil {
				a.habit.Save()
			}
		})
	}()
	return b
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func (a *app) learn(i int32, dd, rr int) {
	if a.habit == nil {
		return
	}
	n := &a.t.Nodes[i]
	sub := habit.Subject{Path: a.t.Path(i), Dir: n.IsDir(), Size: n.Size, AgeDays: a.ev.AgeDays(i)}
	if l := a.habit.Learn(sub, dd, rr); l != "" && dd > 0 {
		a.after(380*time.Millisecond, func() {
			a.toast(ui.Toast{Title: Tf("已学会：%s · 自动清理会一并处理同类项", habitLabel(l)), Timeout: 5200 * time.Millisecond})
		})
	}
}

// restore puts trash items back; nodes still in this scan reappear in the chart.
func (a *app) restore(ids []string) {
	a.chartMorph(420 * time.Millisecond)
	var ok, failed, unlearned int
	for _, id := range ids {
		it, err := a.bin.Restore(id)
		if err != nil {
			failed++
			continue
		}
		ok++
		if n, found := a.trashed[id]; found && a.t != nil && !a.t.Attached(n) {
			delete(a.trashed, id)
			a.t.Reattach(n)
			a.ver++
			sub := habit.Subject{Path: it.Orig, Dir: it.Dir, Size: it.Size, AgeDays: a.ev.AgeDays(n)}
			switch it.Source {
			case trash.ByHabit:
				a.habit.Learn(sub, 0, 1)
				unlearned++
			case trash.ByManual:
				a.habit.Learn(sub, -1, 0)
			}
		}
	}
	if a.habit != nil {
		a.habit.Save()
	}
	if failed > 0 {
		a.toast(ui.Toast{Title: Tf("%d 项未能恢复", failed), Description: T("原位置已有同名文件，或文件已不在废纸篓"), Type: "error"})
	} else if unlearned > 0 {
		a.toast(ui.Toast{Title: T("已记录：以后不再按此习惯自动清理同类项")})
	}
	_ = ok
}

func planSize(p []cleanItem) (s int64) {
	for _, it := range p {
		s += it.meta.Size
	}
	return
}

// --------------------------------------------------------------------------------------------

var pendingToasts []ui.Toast

func (a *app) toast(t ui.Toast) { pendingToasts = append(pendingToasts, t) }

func fmtSize(b int64) string {
	f := float64(b)
	for _, u := range []string{"B", "KB", "MB", "GB", "TB"} {
		if f < 1000 || u == "TB" {
			if u == "B" {
				return fmt.Sprintf("%d B", b)
			}
			if f < 10 {
				return fmt.Sprintf("%.2f %s", f, u)
			}
			return fmt.Sprintf("%.1f %s", f, u)
		}
		f /= 1024
	}
	return ""
}

func fmtNum(n int64) string {
	s := fmt.Sprint(n)
	if n < 10000 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func ageStr(days int) string {
	switch {
	case days < 1:
		return T("今天")
	case days < 30:
		return Tn(int64(days), "%d 天前", days)
	case days < 365:
		return Tn(int64((days+15)/30), "%d 个月前", (days+15)/30)
	}
	return Tf("%.1f 年前", float64(days)/365)
}

func fmtFloat1(f float64) string {
	if f >= 10 {
		return fmt.Sprintf("%.0f", f)
	}
	return fmt.Sprintf("%.1f", f)
}

// kindOf names a file's kind by its extension.
func kindOf(name string) string {
	ext := strings.TrimPrefix(filepath.Ext(name), ".")
	if ext == "" || len(ext) > 8 {
		return T("文件")
	}
	return Tf("%s 文件", strings.ToUpper(ext))
}
