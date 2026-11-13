package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/21stware/tossy/engine/gitrepo"
	"github.com/21stware/tossy/engine/safety"
	"github.com/21stware/tossy/engine/scan"
	"github.com/21stware/tossy/engine/trash"
)

// Developer folders: a Git checkout says what deleting it would lose, a repository lists its
// worktrees (agents leave many behind), and a project folder lists the build caches inside it.

// gitState caches what git said about each working tree, per scan.
type gitState struct {
	st                map[string]*gitEntry
	fam               map[string][]gitrepo.Tree
	open              bool // the worktree fold
	rootFor, rootReal string
	devK              int32 // the dev-cache fold's node, and its list
	devV              int
	dev               []devGroup
	devO              bool
}

type gitEntry struct {
	s    gitrepo.Status
	done bool
}

// gitStatus asks git about a working tree in the background; done is false until it answers.
func (a *app) gitStatus(path string) (gitrepo.Status, bool) {
	g := &a.git
	if g.st == nil {
		g.st = map[string]*gitEntry{}
	}
	if e := g.st[path]; e != nil {
		return e.s, e.done
	}
	e := &gitEntry{}
	g.st[path] = e
	go func() {
		s := gitrepo.Stat(context.Background(), path)
		a.onMain(func() { e.s, e.done = s, true })
	}()
	return gitrepo.Status{}, false
}

// gitFamily lists the working trees that share path's repository (read from .git, cached).
func (a *app) gitFamily(path string) []gitrepo.Tree {
	g := &a.git
	if g.fam == nil {
		g.fam = map[string][]gitrepo.Tree{}
	}
	if f, ok := g.fam[path]; ok {
		return f
	}
	f := gitrepo.Family(path)
	g.fam[path] = f
	return f
}

// findReal finds a path git reported, which has its symlinks resolved (/private/var on macOS)
// while the scan's root may not.
func (a *app) findReal(p string) int32 {
	if m := a.t.Find(p); m != scan.None {
		return m
	}
	g := &a.git
	if g.rootFor != a.t.Root {
		g.rootFor, g.rootReal = a.t.Root, a.t.Root
		if r, err := filepath.EvalSymlinks(a.t.Root); err == nil {
			g.rootReal = r
		}
	}
	if rel, err := filepath.Rel(g.rootReal, p); err == nil && !strings.HasPrefix(rel, "..") {
		return a.t.Find(filepath.Join(a.t.Root, rel))
	}
	return scan.None
}

// forgetGit drops what git said: the files may have changed.
func (a *app) forgetGit() { a.git.st, a.git.fam, a.git.dev = nil, nil, nil }

// gitLine says, in a few words, what deleting a working tree would lose.
func gitLine(s gitrepo.Status, done bool) (string, bool) {
	switch {
	case !done:
		return T("正在检查…"), false
	case s.Err != nil:
		return T("无法读取 Git 状态"), true
	}
	var p []string
	if s.Changed > 0 {
		p = append(p, Tn(int64(s.Changed), "%d 个文件未提交", s.Changed))
	}
	if s.Untracked > 0 {
		p = append(p, Tn(int64(s.Untracked), "%d 个新文件未跟踪", s.Untracked))
	}
	if s.LocalOnly > 0 {
		if s.Whole {
			p = append(p, Tn(int64(s.LocalOnly), "%d 个提交只在本机", s.LocalOnly))
		} else {
			p = append(p, Tn(int64(s.LocalOnly), "%d 个提交尚未合并（分支会保留）", s.LocalOnly))
		}
	}
	switch {
	case len(p) > 0:
		return strings.Join(p, T("，")), !s.Lossless()
	case s.Whole:
		return T("改动均已提交并推送"), false
	}
	return T("没有未提交的改动"), false
}

func gitKindLabel(k gitrepo.Kind) string {
	if k == gitrepo.Worktree {
		return "worktree"
	}
	return "git"
}

// focusInfo sits under the list's header: what the folder in focus is to a developer.
func (a *app) focusInfo(c *ui.Context) {
	n := a.focus
	if a.t == nil || n <= 0 || !a.t.Attached(n) || !a.t.Nodes[n].IsDir() {
		return
	}
	pl := float32(18)
	if a.slim {
		pl = 16
	}
	ui.Column(c.Key("finfo")).Gap(6).Padding(0, pl, 6, pl).Shrink(0).Children(func() {
		a.gitPanel(c, n)
		a.devPanel(c, n)
	})
}

// gitPanel is the Git section for a working tree: its state, and the trees beside it.
func (a *app) gitPanel(c *ui.Context, n int32) {
	pal := a.pal
	kind := a.ev.Git(n)
	if kind != gitrepo.Repo && kind != gitrepo.Worktree {
		return
	}
	path := a.t.Path(n)
	fam := a.gitFamily(path)
	title := T("Git 仓库")
	if kind == gitrepo.Worktree {
		title = T("Git worktree")
	}
	if len(fam) > 1 {
		title += " · " + Tf("共 %d 个工作区", len(fam))
	}
	if line, warn := gitLine(a.gitStatus(path)); warn {
		title += " · " + line
	}
	open := a.git.open
	a.fold(c, "git", title, &open, func() {
		ui.Column(c).Gap(2).Padding(2, 0, 0, 4).Children(func() {
			var famSize int64
			stale, idle := 0, 0
			for i, w := range fam {
				w := w
				m := a.findReal(w.Path)
				here := m == n
				if m != scan.None && !w.Main {
					famSize += a.t.Nodes[m].Size
				}
				if w.Prunable {
					stale++
				} else if m != scan.None && a.ev.AgeDays(m) > 30 {
					idle++
				}
				r := ui.Row(c.Key(fmt.Sprint("w", i))).Gap(8).AlignItems(ui.Start).Padding(3, 4).Radius(5)
				if m != scan.None && !here {
					r.Cursor(ui.CursorPointer)
					if r.Hovered() {
						r.Background(pal.hov)
						a.hover = m
					}
					if r.Clicked() && a.anal == nil {
						a.zoomTo(a.t.Nodes[m].Parent)
						a.selected = m
					}
				}
				r.Children(func() {
					st, done := gitrepo.Status{}, true
					line, warn := "", false
					switch {
					case w.Prunable:
						line, warn = T("文件夹已不存在：可在仓库中运行 git worktree prune"), true
					default:
						st, done = a.gitStatus(w.Path)
						line, warn = gitLine(st, done)
					}
					dot := pal.level[safety.Safe]
					if warn {
						dot = pal.level[safety.Risky]
					} else if !done {
						dot = pal.t4
					}
					ui.Box(c).Size(6, 6).Radius(3).Background(dot).Margin(6, 0, 0, 0).Shrink(0)
					ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
						name := filepath.Base(w.Path)
						if w.Branch != "" && w.Branch != name {
							name += " · " + w.Branch
						}
						var tags []string
						if w.Main {
							tags = append(tags, T("主工作区"))
						}
						if here {
							tags = append(tags, T("当前"))
						}
						if w.Locked {
							tags = append(tags, T("已锁定"))
						}
						if len(tags) > 0 {
							name += "（" + strings.Join(tags, T("，")) + "）"
						}
						ui.Text(c, name).FontSize(12).TextColor(pal.text).SingleLine().Ellipsis("…")
						ui.Text(c, line).FontSize(11.5).TextColor(pal.t3)
					})
					sz := "—"
					if m != scan.None {
						sz = fmtSize(a.t.Nodes[m].Size)
					}
					ui.Text(c, sz).FontSize(12).Font(pal.mono).TextColor(pal.t3).SingleLine().Shrink(0)
				})
			}
			// the reminder: many worktrees pile up quietly
			var notes []string
			if len(fam) > 2 {
				notes = append(notes, Tf("此仓库有 %d 个 worktree，本机可见部分合计 %s。合并后不再需要的，建议在仓库中运行 git worktree remove 移除。", len(fam)-1, fmtSize(famSize)))
			}
			if idle > 0 {
				notes = append(notes, Tn(int64(idle), "%d 个工作区超过 30 天未修改。", idle))
			}
			if stale > 0 {
				notes = append(notes, Tn(int64(stale), "%d 条 worktree 记录指向已删除的文件夹。", stale))
			}
			for i, s := range notes {
				ui.Text(c.Key(fmt.Sprint("n", i)), s).FontSize(11.5).TextColor(pal.level[safety.Review]).Padding(4, 4, 0, 4)
			}
		})
	})
	a.git.open = open
}

// ------------------------------------------------------------------------------------------
// build caches inside a folder

type devGroup struct {
	label string
	tag   string
	size  int64
	items []cleanItem
}

// devCaches groups the rule candidates under n by what they are (node_modules ×3, target ×2…).
func (a *app) devCaches(n int32) []devGroup {
	g := &a.git
	if g.dev != nil && g.devK == n && g.devV == a.ver {
		return g.dev
	}
	g.devK, g.devV = n, a.ver
	by := map[string]*devGroup{}
	for _, it := range a.candItems(n, safety.Safe) {
		if it.node == n {
			continue
		}
		r := a.ev.Own(it.node)
		if r == nil || !devTags[r.Tag] {
			continue
		}
		key := r.ID + "/" + strings.ToLower(a.t.Name(it.node))
		d := by[key]
		if d == nil {
			d = &devGroup{label: a.t.Name(it.node), tag: r.Tag}
			by[key] = d
		}
		d.size += it.meta.Size
		d.items = append(d.items, it)
	}
	out := []devGroup{}
	for _, d := range by {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].size > out[j].size })
	g.dev = out
	return out
}

// devTags are the rule tags that mean "a developer's rebuildable stuff".
var devTags = map[string]bool{"构建产物": true, "构建缓存": true, "依赖": true, "测试缓存": true, "包缓存": true, "开发缓存": true}

func (a *app) devPanel(c *ui.Context, n int32) {
	pal := a.pal
	groups := a.devCaches(n)
	if len(groups) == 0 {
		return
	}
	var total int64
	var all []cleanItem
	for _, d := range groups {
		total += d.size
		all = append(all, d.items...)
	}
	open := a.git.devO
	a.fold(c, "dev", Tf("可重建的开发缓存 · %s", fmtSize(total)), &open, func() {
		ui.Column(c).Gap(0).Padding(2, 0, 0, 4).Children(func() {
			for i, d := range groups[:min(8, len(groups))] {
				label := d.label
				if len(d.items) > 1 {
					label += fmt.Sprintf(" ×%d", len(d.items))
				}
				ui.Row(c.Key(fmt.Sprint("d", i))).Gap(8).AlignItems(ui.Center).Height(22).Padding(0, 4).Children(func() {
					ui.Box(c).Size(7, 7).Radius(2).Background(pal.level[safety.Safe]).Shrink(0)
					ui.Text(c, label).FontSize(12).Font(pal.mono).TextColor(pal.t2).SingleLine().Shrink(0)
					ui.Text(c, T(d.tag)).FontSize(11.5).TextColor(pal.t4).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
					ui.Text(c, fmtSize(d.size)).FontSize(12).Font(pal.mono).TextColor(pal.t3).SingleLine().Shrink(0)
				})
			}
			ui.Row(c).Padding(6, 0, 0, 0).Children(func() {
				b := a.btn(c.Key("devclean"), Tf("清理这些缓存（%s）", fmtSize(total)), btnPlain, false, nil).Disabled(a.anal != nil)
				b.Tooltip(T("只清理可由构建或安装命令重新生成的内容，源码与 Git 记录不受影响"))
				if b.Clicked() {
					a.freeUp(all)
				}
			})
		})
	})
	a.git.devO = open
}

// ------------------------------------------------------------------------------------------
// deleting a folder that holds checkouts

// checkoutsIn lists the Git working trees a delete of n would take.
func (a *app) checkoutsIn(n int32) []int32 {
	if !a.t.Nodes[n].IsDir() {
		return nil
	}
	return a.ev.Checkouts(n, 40)
}

// gitAskBody explains, line by line, what the checkouts in a delete hold. It returns whether any
// of them may lose work (or is still being checked).
func (a *app) gitAskBody(c *ui.Context, q *askState) bool {
	pal := a.pal
	risky := false
	n := q.node
	if len(q.list) == 1 && q.list[0] == n {
		what := T("Git 仓库")
		if a.ev.Git(n) == gitrepo.Worktree {
			what = T("Git worktree")
		}
		ui.Text(c, Tf("「%s」是一个 %s，大小 %s。", a.name(n), what, fmtSize(a.t.Nodes[n].Size))).TextColor(pal.t2)
	} else {
		ui.Text(c, Tf("「%s」（%s）里有 %d 个 Git 工作区：", a.name(n), fmtSize(a.t.Nodes[n].Size), len(q.list))).TextColor(pal.t2)
	}
	worktrees, outside := 0, 0
	ui.Column(c.Key("gl")).Gap(3).Children(func() {
		for i, m := range q.list {
			if !a.t.Attached(m) {
				continue
			}
			st, done := a.gitStatus(a.t.Path(m))
			line, warn := gitLine(st, done)
			if warn || !done {
				risky = true
			}
			if a.ev.Git(m) == gitrepo.Worktree {
				worktrees++
			}
			if i >= 6 {
				continue
			}
			ui.Row(c.Key(i)).Gap(8).AlignItems(ui.Start).Children(func() {
				dot := pal.level[safety.Safe]
				if warn {
					dot = pal.level[safety.Risky]
				} else if !done {
					dot = pal.t4
				}
				ui.Box(c).Size(6, 6).Radius(3).Background(dot).Margin(6, 0, 0, 0).Shrink(0)
				name := a.name(m)
				if st.Branch != "" && st.Branch != name {
					name += " · " + st.Branch
				}
				ui.RichText(c).FontSize(12).Grow(1).Shrink(1).Children(func() {
					ui.Text(c, name+"  ").TextColor(pal.text)
					ui.Text(c, line).TextColor(pal.t3)
				})
			})
		}
		if len(q.list) > 6 {
			ui.Text(c, Tf("另有 %d 个", len(q.list)-6)).FontSize(12).TextColor(pal.t3).Padding(0, 0, 0, 14)
		}
	})
	// worktrees elsewhere that live off a repository being deleted
	for _, m := range q.list {
		if a.ev.Git(m) != gitrepo.Repo {
			continue
		}
		for _, w := range a.gitFamily(a.t.Path(m)) {
			if w.Main || w.Prunable {
				continue
			}
			if k := a.findReal(w.Path); k == scan.None || !a.isUnder(k, n) {
				outside++
			}
		}
	}
	if outside > 0 {
		risky = true
		ui.Text(c, Tn(int64(outside), "另有 %d 个 worktree 依赖这里的仓库，删除后它们将无法使用。", outside)).TextColor(pal.level[safety.Risky])
	}
	if worktrees > 0 {
		ui.Text(c, T("主仓库会保留 worktree 的记录；确认不再需要后，可在仓库中运行 git worktree prune。")).TextColor(pal.t3)
	}
	if !risky {
		ui.Text(c, Tf(markNote, trashName())).TextColor(pal.t3)
	}
	return risky
}

// ------------------------------------------------------------------------------------------
// emptying the trash: a last look at the checkouts in it

// markNote says what putting something in Tossy's trash really does.
const markNote = "文件会留在原处，只在 Tossy 的%[1]s里做个标记；清空前随时可以还原，清空时直接永久删除，不经过系统%[1]s。"

// emptyCheck is what git says, at the moment of emptying, about the checkouts in the trash.
// Marks can sit for days while work goes on in a marked folder, so the state seen when it was
// trashed is not enough: this is the last chance before the files are deleted for good.
type emptyCheck struct {
	n     int // trash items when the check started; a change restarts it
	done  bool
	ids   []string    // every item checked
	risky []emptyRisk // items that hold Git work found nowhere else (or that git could not read)
}

type emptyRisk struct {
	id   string
	name string // the checkout, relative to the item when inside it
	line string // what deleting it would lose
}

// checkEmpty starts (or restarts) the check for the empty confirm.
func (a *app) checkEmpty(q *askState, items []trash.Item) *emptyCheck {
	if q.chk != nil && q.chk.n == len(items) {
		return q.chk
	}
	chk := &emptyCheck{n: len(items)}
	q.chk = chk
	for _, it := range items {
		chk.ids = append(chk.ids, it.ID)
	}
	go func() {
		risky := emptyRisks(context.Background(), items)
		a.onMain(func() { chk.risky, chk.done = risky, true })
	}()
	return chk
}

// emptyRisks lists the items whose deletion would lose Git work. It runs git: off the main thread.
func emptyRisks(ctx context.Context, items []trash.Item) []emptyRisk {
	var out []emptyRisk
	for _, it := range items {
		if !it.Dir {
			continue
		}
		found, _ := gitrepo.Find(ctx, it.Orig, 20, 20000)
		for _, p := range found {
			s := gitrepo.Stat(ctx, p)
			if s.Lossless() {
				continue
			}
			name := filepath.Base(it.Orig)
			if rel, err := filepath.Rel(it.Orig, p); err == nil && rel != "." {
				name += "/" + filepath.ToSlash(rel)
			}
			line, _ := gitLine(s, true)
			out = append(out, emptyRisk{id: it.ID, name: name, line: line})
		}
	}
	return out
}

// safeIDs are the checked items without Git work at risk.
func (chk *emptyCheck) safeIDs() []string {
	bad := map[string]bool{}
	for _, r := range chk.risky {
		bad[r.id] = true
	}
	out := []string{}
	for _, id := range chk.ids {
		if !bad[id] {
			out = append(out, id)
		}
	}
	return out
}
