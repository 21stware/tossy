package main

import (
	"fmt"
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
//   - 查看评估 opens the evaluation of the folder in focus, item by item, with checkboxes.

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
		why := fmt.Sprintf("你曾手动删除 %d 个 %s", p.Match.D, p.Match.Label)
		if p.Match.Seed {
			why += "（示例历史）"
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

// appOf is the app name in an in-use reason ("Chrome 正在运行" → "Chrome").
func appOf(why string) string {
	if i := strings.Index(why, " 正在运行"); i > 0 {
		return why[:i]
	}
	return "文件"
}

func trashName() string {
	if runtime.GOOS == "darwin" {
		return "废纸篓"
	}
	return "回收站"
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
	a.eval, a.scene = nil, sceneBrowse
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
	a.toast(ui.Toast{Title: "已停止分析，未删除任何内容"})
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
		c.After(16 * time.Millisecond)
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
		msg := "没有可安全清理的内容"
		if an.skipped > 0 {
			msg += fmt.Sprintf("（%d 项正在使用，已跳过）", an.skipped)
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
	if a.motion() {
		wait = max(0, a.fly.lastLanding().Sub(a.now)) + 700*time.Millisecond
	}
	a.after(wait, func() {
		title := fmt.Sprintf("已将 %d 项（%s）移至%s", len(live), fmtSize(size), trashName())
		desc := ""
		if habits > 0 {
			desc = fmt.Sprintf("其中 %d 项依据你的习惯", habits)
		}
		a.toast(ui.Toast{ID: "freed", Title: title, Description: desc, Type: "success", Timeout: 7 * time.Second,
			Action: "撤销", OnAction: func() { a.undo(b) }})
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

// ------------------------------------------------------------------------------------------
// 查看评估: the evaluation of the folder in focus

type evalSheet struct {
	scope      int32
	groups     [3][]evalItem // safe, habit, review
	reviewOpen bool
	set        map[int32]bool
	setV, v    int
}

type evalItem struct {
	cleanItem
	on    bool
	inuse string
}

var evalTitles = [3]string{"可安全清理", "依据你的习惯", "建议复核"}

func (e *evalSheet) chosenSet() map[int32]bool {
	if e.set != nil && e.setV == e.v {
		return e.set
	}
	e.set, e.setV = map[int32]bool{}, e.v
	for _, g := range e.groups {
		for _, it := range g {
			if it.on {
				e.set[it.node] = true
			}
		}
	}
	return e.set
}

func (e *evalSheet) chosen() (out []cleanItem, size int64) {
	for _, g := range e.groups {
		for _, it := range g {
			if it.on {
				out = append(out, it.cleanItem)
				size += it.meta.Size
			}
		}
	}
	return
}

func (a *app) evaluate() {
	if a.t == nil || a.scanning || a.anal != nil {
		return
	}
	if a.motion() {
		a.radar = a.now
	}
	a.mode = modeSafety
	a.openEval(a.focus)
}

func (a *app) openEval(scope int32) {
	cands := a.candItems(scope, safety.Review)
	var safe, review []cleanItem
	for _, it := range cands {
		if safety.Level(it.meta.Level) == safety.Safe {
			safe = append(safe, it)
		} else {
			review = append(review, it)
		}
	}
	skip := map[int32]bool{}
	for _, it := range safe {
		skip[it.node] = true
	}
	habits := a.habitItems(scope, skip)
	hn := map[int32]bool{}
	for _, it := range habits {
		hn[it.node] = true
	}
	// review items neither inside a habit pick nor holding one
	var rv []cleanItem
	for _, it := range review {
		if a.underAny(it.node, hn) {
			continue
		}
		holds := false
		for _, h := range habits {
			if h.node != it.node && a.isUnder(h.node, it.node) {
				holds = true
				break
			}
		}
		if !holds {
			rv = append(rv, it)
		}
	}
	busy := a.busyMap()
	e := &evalSheet{scope: scope}
	for _, it := range safe {
		why := busy[it.node]
		e.groups[0] = append(e.groups[0], evalItem{it, why == "", why})
	}
	for _, it := range habits {
		e.groups[1] = append(e.groups[1], evalItem{it, true, ""})
	}
	for _, it := range rv {
		e.groups[2] = append(e.groups[2], evalItem{it, false, ""})
	}
	a.switchScene(func() { a.eval, a.scene = e, sceneEval })
}

func (a *app) closeEval() {
	a.switchScene(func() { a.eval, a.scene = nil, sceneBrowse })
}

func (a *app) evalPanel(c *ui.Context) {
	pal := a.pal
	e := a.eval
	sel, size := e.chosen()
	scope := e.scope
	ui.Column(c.Key("eval")).Grow(1).Children(func() {
		a.stagger(ui.Column(c.Key("eh")).Padding(18, 24, 0, 24).Gap(4), 0).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "安全评估").FontSize(16).Bold().Grow(1)
				if ui.Button(c.Key("x"), "✕").Width(28).Tooltip("关闭（Esc）").Clicked() {
					a.closeEval()
				}
			})
			ui.Text(c, fmt.Sprintf("%s · %s 个项目 · %d 条规则", a.displayPath(scope), fmtNum(int64(a.t.Nodes[scope].Count)), safety.RuleCount()+1)).
				FontSize(11.5).TextColor(pal.t3).SingleLine().TruncateMiddle()
		})
		sums := a.ev.LevelSums(scope)
		tot := float32(max(a.t.Nodes[scope].Size, 1))
		a.stagger(ui.Box(c.Key("ebar")).Height(6).Margin(18, 24, 0, 24), 1).Draw(func(p *ui.Painter, r ui.Rect) {
			x := r.X
			for lv := range sums {
				w := r.W * float32(sums[lv]) / tot
				if w <= 0 {
					continue
				}
				p.Fill(ui.Rect{X: x, Y: r.Y, W: max(0, w-2), H: r.H}, pal.level[lv], 2)
				x += w
			}
		})
		a.stagger(ui.Row(c.Key("elg")).Padding(10, 24, 6, 24).Gap(18).Wrap(), 2).Children(func() {
			for lv, name := range levelNames {
				ui.Row(c.Key(lv)).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(6, 6).Radius(3).Background(pal.level[lv])
					ui.Text(c, name).FontSize(12).TextColor(pal.t3)
					ui.Text(c, fmtSize(sums[lv])).FontSize(12).TextColor(pal.text)
				})
			}
		})
		ui.Scroll(c.Key("evl")).Grow(1).Children(func() {
			ui.Column(c).Padding(0, 14, 10, 14).Children(func() {
				n := 3
				for g := range e.groups {
					if g > 0 && len(e.groups[g]) == 0 {
						continue
					}
					a.evalGroup(c, g, n)
					n++
				}
			})
		})
		ui.Box(c).Height(1).Background(pal.line)
		ui.Row(c.Key("eft")).Padding(12, 24).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "已选 ").FontSize(12).TextColor(pal.t3)
			ui.Text(c, fmt.Sprint(len(sel))).FontSize(12).Bold()
			ui.Text(c, " 项，共 ").FontSize(12).TextColor(pal.t3)
			ui.Text(c, fmtSize(size)).FontSize(12).Bold()
			ui.Spacer(c)
			if ui.Button(c.Key("cancel"), "取消").Clicked() {
				a.closeEval()
			}
			label := "移至" + trashName()
			if len(sel) > 0 {
				label = "释放 " + fmtSize(size)
			}
			if ui.PrimaryButton(c.Key("go"), label).Disabled(len(sel) == 0).Clicked() {
				a.mode = modeSafety
				a.closeEval()
				if a.motion() {
					a.after(360*time.Millisecond, func() { a.freeUp(sel) })
				} else {
					a.freeUp(sel)
				}
			}
		})
	})
}

func (a *app) evalGroup(c *ui.Context, g, st int) {
	pal := a.pal
	e := a.eval
	items := e.groups[g]
	ui.Column(c.Key(g)).Children(func() {
		head := a.stagger(ui.Row(c.Key("h")).Padding(14, 10, 6, 10).Gap(8).AlignItems(ui.Center), st).Children(func() {
			if g == 2 {
				ch := "▸"
				if e.reviewOpen {
					ch = "▾"
				}
				ui.Text(c, ch).FontSize(10).TextColor(pal.t3)
			}
			ui.Text(c, evalTitles[g]).FontSize(12).TextColor(pal.t2).Bold()
			sub := fmt.Sprintf("%d 项", len(items))
			switch g {
			case 1:
				sub += " · 学自你的手动删除"
			case 2:
				sub += " · 默认不选"
			}
			ui.Text(c, sub).FontSize(12).TextColor(pal.t4)
			ui.Spacer(c)
			if g < 2 && len(items) > 0 {
				all := ui.Text(c.Key("all"), "全选").FontSize(12).TextColor(pal.t3).Cursor(ui.CursorPointer)
				if all.Hovered() {
					all.TextColor(pal.text)
				}
				if all.Clicked() {
					a.toggleGroup(g)
				}
			}
		})
		if g == 2 {
			head.Cursor(ui.CursorPointer)
			if head.Clicked() {
				e.reviewOpen = !e.reviewOpen
			}
			if !e.reviewOpen {
				return
			}
		}
		if len(items) == 0 {
			ui.Text(c, "没有发现可安全清理的项目").FontSize(12).TextColor(pal.t4).Padding(8, 10)
			return
		}
		for i := range items {
			it := &items[i]
			row := ui.Row(c.Key(it.node)).Padding(6, 10).Gap(12).Radius(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).Children(func() {
				if ui.Checkbox(c.Key("cb"), &it.on, "").Changed() {
					e.v++
				}
				ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
					ui.Text(c, a.name(it.node)).SingleLine().Ellipsis("…")
					ui.Row(c).Children(func() {
						switch {
						case it.inuse != "":
							ui.Text(c, it.inuse+" · 自动清理会跳过").FontSize(11.5).TextColor(pal.level[safety.Review]).SingleLine().Ellipsis("…").Shrink(1)
						case it.meta.Source == trash.ByHabit:
							ui.Text(c, "依据你的习惯").FontSize(11.5).TextColor(pal.accent)
							ui.Text(c, " · "+it.why).FontSize(11.5).TextColor(pal.t4).SingleLine().Ellipsis("…").Shrink(1)
						default:
							ui.Text(c, it.why).FontSize(11.5).TextColor(pal.t4).SingleLine().Ellipsis("…").Shrink(1)
						}
					})
				})
				ui.Text(c, fmtSize(it.meta.Size)).TextColor(pal.t2)
			})
			if row.Hovered() {
				a.hover = it.node
				row.Background(pal.hov)
			}
			if row.Clicked() {
				it.on = !it.on
				e.v++
			}
		}
	})
}

// toggleGroup is 全选: everything off when all are on, else everything on.
func (a *app) toggleGroup(g int) {
	e := a.eval
	all := true
	for _, it := range e.groups[g] {
		all = all && it.on
	}
	for i := range e.groups[g] {
		e.groups[g][i].on = !all
	}
	e.v++
}

// displayPath shows a node's path with the home folder as ~.
func (a *app) displayPath(i int32) string { return displayHome(a.t.Path(i)) }

func displayHome(p string) string {
	if home := homeDir(); home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

var _ = scan.None
