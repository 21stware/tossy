package main

import (
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/fanfree/fanfree/engine/safety"
	"github.com/fanfree/fanfree/engine/scan"
	"github.com/fanfree/fanfree/engine/trash"
)

// The two ways to clean, as in the prototype:
//
//   - 自动清理 sweeps the whole scan (rules, files in use, learned habits), shows what it finds as
//     it goes, then throws the plan into the trash without asking: everything it takes is
//     regenerated on demand or matches something the user deleted by hand before, and all of it
//     can be restored until the trash is emptied.
//   - a press on the trash bin or a row's delete moves one item by hand.

// ------------------------------------------------------------------------------------------
// timers: callbacks on the main thread after a delay, driven by the view's clock

type timer struct {
	at time.Time
	fn func()
}

func (a *app) after(d time.Duration, fn func()) {
	now := a.now
	if now.IsZero() {
		now = time.Now()
	}
	a.timers = append(a.timers, timer{now.Add(d), fn})
}

func (a *app) runTimers(c *ui.Context) {
	if len(a.timers) == 0 {
		return
	}
	var due []func()
	var keep []timer
	for _, t := range a.timers {
		if !t.at.After(a.now) {
			due = append(due, t.fn)
		} else {
			keep = append(keep, t)
		}
	}
	a.timers = keep
	for _, fn := range due {
		fn()
	}
}

// wakeForTimers asks for a frame when the next timer is due. It runs at the end of the
// view, so timers set later in the frame (by tickAnalysis or a click handler) still wake it;
// without this they would wait for the next mouse move.
func (a *app) wakeForTimers(c *ui.Context) {
	if len(a.timers) > 0 {
		next := a.timers[0].at
		for _, t := range a.timers[1:] {
			if t.at.Before(next) {
				next = t.at
			}
		}
		c.After(max(time.Millisecond, next.Sub(a.now)))
	}
}

// ------------------------------------------------------------------------------------------
// what can go

// candItems lists rule candidates under scope up to level lv, largest first.
func (a *app) candItems(scope int32, lv safety.Level) []cleanItem {
	var out []cleanItem
	for _, c := range a.ev.Candidates(scope, lv) {
		out = append(out, cleanItem{c.Node, trash.Meta{Size: c.Size, Dir: a.t.Nodes[c.Node].IsDir(), Source: trash.ByRule,
			Rule: c.Rule.ID, Level: int(c.Rule.Level)}, c.Rule.Tag + " · " + c.Rule.Reason})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].meta.Size > out[j].meta.Size })
	return out
}

// habitItems lists what the learned habits would take under scope, besides skip.
func (a *app) habitItems(scope int32, skip map[int32]bool) []cleanItem {
	if a.habit == nil || !a.set.Habits {
		return nil
	}
	var out []cleanItem
	for _, p := range a.habit.Candidates(a.t, a.ev, scope, skip) {
		why := Tf("你曾手动删除 %d 个 %s", p.Match.D, habitLabel(p.Match.Label))
		if p.Match.Seed {
			why += T("（示例历史）")
		}
		out = append(out, cleanItem{p.Node, trash.Meta{Size: p.Size, Dir: a.t.Nodes[p.Node].IsDir(), Source: trash.ByHabit,
			Habit: p.Match.Label, Level: int(a.verdict(p.Node).Level())}, why})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].meta.Size > out[j].meta.Size })
	return out
}

// cleanPlan is what 自动清理 would take right now by rule, in use or not (cached per tree version).
func (a *app) cleanPlan() []cleanItem {
	if a.t == nil || a.scanning {
		return nil
	}
	if a.plan != nil && a.planV == a.ver {
		return a.plan
	}
	a.planV = a.ver
	a.plan = a.candItems(0, safety.Safe)
	if a.plan == nil {
		a.plan = []cleanItem{}
	}
	return a.plan
}

// refreshProcs lists running programs in the background, for the in-use check.
func (a *app) refreshProcs(done func()) {
	if !a.set.SkipApps {
		if done != nil {
			done()
		}
		return
	}
	go func() {
		p := runningApps()
		a.onMain(func() {
			a.procs = p
			a.procsV++
			if done != nil {
				done()
			}
		})
	}()
}

// busyMap says which planned items are in use and why (cached).
func (a *app) busyMap() map[int32]string {
	k := [4]int{a.ver, a.procsV, int(a.set.InUseMin * 10), b2i(a.set.SkipApps)}
	if a.busy != nil && a.busyK == k {
		return a.busy
	}
	a.busyK = k
	items := a.cleanPlan()
	a.busy = recentWrites(a.t, items, time.Duration(a.set.InUseMin*float64(time.Minute)), time.Now())
	if a.set.SkipApps && len(a.procs) > 0 {
		for _, it := range items {
			if _, ok := a.busy[it.node]; ok {
				continue
			}
			if why := appsUsing(a.t.Path(it.node), a.procs); why != "" {
				a.busy[it.node] = why
			}
		}
	}
	return a.busy
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// idlePlan is the button's promise: rule items not in use.
func (a *app) idlePlan() (items []cleanItem, busy int) {
	bm := a.busyMap()
	for _, it := range a.cleanPlan() {
		if bm[it.node] != "" {
			busy++
		} else {
			items = append(items, it)
		}
	}
	return
}

// appOf names what holds an item in use: the running app, or just "files" for a recent write.
func appOf(why string) string {
	if why == recentWrite {
		return T("文件")
	}
	return why
}

func trashName() string {
	if runtime.GOOS == "darwin" {
		return T("废纸篓")
	}
	return T("回收站")
}

// ------------------------------------------------------------------------------------------
// 自动清理: analyse, then throw

type analysis struct {
	t0      time.Time
	dur     time.Duration
	k       float64
	rules   []cleanItem
	habits  []cleanItem
	order   []sweepItem // rules by angle, for the live count
	locks   []int32     // the biggest rule items, bracketed as the beam passes
	inuse   map[int32]string
	ready   bool // the process check came back
	found   int
	foundSz int64
	// after the sweep
	doneAt  time.Time
	plan    []cleanItem
	skipped int
	apps    []string
}

type sweepItem struct {
	mid  float64
	size int64
}

var analysisPhases = []string{"匹配清理规则", "检查文件占用", "学习你的删除习惯", "生成清理方案"}

func (an *analysis) phase() string {
	return analysisPhases[min(3, int(an.k*4))]
}

// autoClean starts the sweep, or stops it when one is running.
func (a *app) autoClean() {
	if an := a.anal; an != nil {
		if an.doneAt.IsZero() {
			a.cancelAnalysis()
		} else {
			a.anal = nil
			a.freeUp(an.plan)
		}
		return
	}
	if a.t == nil || a.scanning {
		return
	}
	a.scene = sceneBrowse
	a.sceneNext, a.sceneT0 = a.sceneNext+1, time.Time{}
	if a.focus != 0 {
		a.zoomTo(0)
	}
	a.mode = modeSafety
	rules := a.candItems(0, safety.Safe)
	skip := make(map[int32]bool, len(rules))
	for _, it := range rules {
		skip[it.node] = true
	}
	an := &analysis{t0: a.now, rules: rules, habits: a.habitItems(0, skip)}
	an.dur = 1500*time.Millisecond + time.Duration(min(1200, a.t.Nodes[0].Count/400))*time.Millisecond
	if !a.set.Analysis || !a.motion() {
		an.dur = 0
	}
	for _, it := range rules {
		x0, x1 := a.xOf(it.node)
		an.order = append(an.order, sweepItem{(x0 + x1) / 2, it.meta.Size})
		if len(an.locks) < 36 {
			an.locks = append(an.locks, it.node)
		}
	}
	sort.Slice(an.order, func(i, j int) bool { return an.order[i].mid < an.order[j].mid })
	an.inuse = recentWrites(a.t, rules, time.Duration(a.set.InUseMin*float64(time.Minute)), time.Now())
	a.anal = an
	paths := make([]string, len(rules))
	for i, it := range rules {
		paths[i] = a.t.Path(it.node)
	}
	a.refreshProcs(func() {
		if a.anal != an {
			return
		}
		if a.set.SkipApps {
			for i, p := range paths {
				if _, ok := an.inuse[rules[i].node]; ok {
					continue
				}
				if why := appsUsing(p, a.procs); why != "" {
					an.inuse[rules[i].node] = why
				}
			}
		}
		an.ready = true
	})
}

func (a *app) cancelAnalysis() {
	if a.anal == nil {
		return
	}
	a.anal = nil
	a.toast(ui.Toast{Title: T("已停止分析，未删除任何内容")})
}

// tickAnalysis advances the sweep; at the end it shows the plan for a beat, then throws it.
func (a *app) tickAnalysis(c *ui.Context) {
	an := a.anal
	if an == nil || !an.doneAt.IsZero() {
		return
	}
	tf := 1.0
	if an.dur > 0 {
		tf = float64(a.now.Sub(an.t0)) / float64(an.dur)
	}
	lim := 1.0
	if !an.ready {
		lim = .9
	}
	an.k = max(0, min(1, tf, lim))
	an.found, an.foundSz = 0, 0
	for _, o := range an.order {
		if o.mid > an.k {
			break
		}
		an.found++
		an.foundSz += o.size
	}
	if an.k >= .75 {
		an.found += len(an.habits)
	}
	if an.k < 1 || !an.ready {
		c.AnimationFrame()
		return
	}
	// the plan: rules not in use, then habits
	seen := map[string]bool{}
	for _, it := range an.rules {
		if a.t.Attached(it.node) {
			if why := an.inuse[it.node]; why != "" {
				an.skipped++
				if app := appOf(why); !seen[app] {
					seen[app] = true
					an.apps = append(an.apps, app)
				}
				continue
			}
			an.plan = append(an.plan, it)
		}
	}
	for _, it := range an.habits {
		if a.t.Attached(it.node) {
			an.plan = append(an.plan, it)
		}
	}
	if len(an.plan) == 0 {
		a.anal = nil
		msg := T("没有可安全清理的内容")
		if an.skipped > 0 {
			msg += Tf("（%d 项正在使用，已跳过）", an.skipped)
		}
		a.toast(ui.Toast{Title: msg})
		return
	}
	an.doneAt = a.now
	if a.motion() {
		a.pulse = a.now
	}
	a.after(420*time.Millisecond, func() {
		if a.anal == an {
			a.anal = nil
			a.freeUp(an.plan)
		}
	})
}

// freeUp throws items into the trash, largest first, and sums it up once they have landed.
func (a *app) freeUp(items []cleanItem) {
	items = append([]cleanItem(nil), items...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].meta.Size > items[j].meta.Size })
	var live []cleanItem
	for _, it := range items {
		if a.t.Attached(it.node) {
			live = append(live, it)
		}
	}
	if len(live) == 0 {
		return
	}
	habits := 0
	for _, it := range live {
		if it.meta.Source == trash.ByHabit {
			habits++
		}
	}
	size := planSize(live)
	b := a.trashNodes(live, true)
	wait := time.Duration(0)
	if a.trashFX() {
		wait = max(0, a.fly.lastLanding().Sub(a.now)) + 700*time.Millisecond
	}
	a.after(wait, func() {
		title := Tn(int64(len(live)), "已将 %d 项（%s）移至%s", len(live), fmtSize(size), trashName())
		desc := ""
		if habits > 0 {
			desc = Tf("其中 %d 项依据你的习惯", habits)
		}
		a.toast(ui.Toast{ID: "freed", Title: title, Description: desc, Type: "success", Timeout: 7 * time.Second,
			Action: T("撤销"), OnAction: func() { a.undo(b) }})
	})
}

// batch is one trashNodes call; ids fill in as the background moves finish.
type batch struct {
	ids  []string
	done bool
	undo bool // undo was asked before the moves finished
}

func (a *app) undo(b *batch) {
	if b == nil {
		return
	}
	if !b.done {
		b.undo = true
		return
	}
	a.restore(b.ids)
}

func (a *app) displayPath(i int32) string { return displayHome(a.t.Path(i)) }

func displayHome(p string) string {
	if home := homeDir(); home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

var _ = scan.None
