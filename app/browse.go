package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/21stware/tossy/engine/gitrepo"
	"github.com/21stware/tossy/engine/habit"
	"github.com/21stware/tossy/engine/safety"
	"github.com/21stware/tossy/engine/scan"
)

// The browse scene: one panel that shows the folder in focus as a list, or, with a file
// selected, that file in detail. The two are drawers: the detail slides in from the right
// over the list's way out to the left, and back.

func (a *app) browsePanel(c *ui.Context) {
	pal := a.pal
	a.throwNode = scan.None
	on := a.fileSel()
	if on {
		a.detNode = a.selected
	}
	if on != a.fileOn {
		a.fileOn, a.fileT0 = on, a.now
	}
	k := float32(easeStd(a.progress(a.fileT0, 0, 300*time.Millisecond)))
	w := max(a.wL, 160)
	lx, dx := -w*(1-k), w*k // back to the list
	if a.fileOn {
		lx, dx = -w*k, w*(1-k)
	}
	ui.Box(c.Key("browse")).Grow(1).Shrink(1).Clip().Children(func() {
		if !a.fileOn || k < 1 {
			ui.Column(c.Key("blist")).Absolute().Top(0).Left(0).Fill().Translate(lx, 0).Children(func() {
				a.listHeader(c)
				a.focusInfo(c)
				if a.t == nil {
					ui.Box(c).Grow(1)
					return
				}
				rows := a.children()
				a.list.Key = func(i int) any { return rows[i] }
				ui.List(c.Key("list"), &a.list, len(rows), func(i int) { a.row(c, rows[i], i) }).Grow(1).Shrink(1).Padding(2, 0, 6, 0).Children(func() {
					if len(rows) == 0 {
						ui.Text(c, T("此文件夹为空")).TextColor(pal.t3).FontSize(12.5).Padding(24).AlignSelf(ui.Center)
					}
				})
			})
		}
		if (a.fileOn || k < 1) && a.t != nil && a.t.Attached(a.detNode) {
			ui.Column(c.Key("bdet")).Absolute().Top(0).Left(0).Fill().Translate(dx, 0).Children(func() {
				ui.Row(c.Key("dback")).Padding(14, 16, 0, 10).AlignItems(ui.Center).DragWindow().Children(func() {
					b := a.btn(c.Key("back"), a.name(a.focus), btnGhost, true, func() { icon(c, icoBack, 15, pal.t2, 2) }).
						Tooltip(T("返回列表（Esc）")).Shrink(1)
					if b.Clicked() && a.fileOn {
						a.selected = a.focus
					}
				})
				a.detail(c)
			})
		}
	})
}

func (a *app) listHeader(c *ui.Context) ui.Element {
	pal := a.pal
	pl := float32(18)
	if a.slim {
		pl = 16
	}
	return ui.Row(c.Key("lh")).Padding(14, pl, 8, pl).Gap(8).AlignItems(ui.Center).DragWindow().Children(func() {
		if a.t != nil && a.focus != 0 {
			if a.btn(c.Key("lup"), "", btnGhost, true, func() { icon(c, icoUp, 15, pal.t2, 2) }).Tooltip(T("返回上级（Esc）")).Clicked() && a.anal == nil {
				a.zoomTo(a.t.Nodes[a.focus].Parent)
			}
		}
		fs := float32(17)
		if a.slim {
			fs = 15
		}
		a.rise(ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
			if a.t == nil {
				ui.Text(c, "—").FontSize(fs).FontWeight(600)
				return
			}
			ui.Text(c, a.name(a.focus)).FontSize(fs).FontWeight(600).SingleLine().Ellipsis("…")
			ui.Text(c, fmtSize(a.t.Nodes[a.focus].Size)+" · "+nItems(int64(len(a.children())))).FontSize(12).TextColor(pal.t3).SingleLine()
		}), a.slimT0, 0)
	})
}

// swatchColor is a row's colour key: its safety level in safety mode, else its sector's colour,
// else its file category.
func (a *app) swatchColor(n int32) ui.Color {
	if a.mode == modeSafety {
		return a.pal.level[a.verdict(n).Level()]
	}
	for i := range a.sun.arcs {
		if h := &a.sun.arcs[i]; h.node == n {
			return hsl(h.h, h.s, h.l, 1)
		}
	}
	if a.t.Nodes[n].IsDir() {
		return a.pal.t3
	}
	return cats[catOf(a.t.Name(n))].c
}

func (a *app) row(c *ui.Context, n int32, i int) {
	pal := a.pal
	nd := &a.t.Nodes[n]
	lv := a.verdict(n).Level()
	sel := n == a.selected
	dir := nd.IsDir()
	slim, wide := a.slim, a.sz == szWide && !a.slim
	r := ui.Row(c).Height(32).Margin(0, 8).Padding(0, 4, 0, 10).Gap(9).Radius(6).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if slim {
		r.Height(30).Margin(0, 6).Padding(0, 6, 0, 10).Gap(8)
	}
	if i < 14 {
		a.rise(r, a.slimT0, i+1)
	}
	hot := r.Hovered() || n == a.hover
	switch {
	case sel:
		r.Background(pal.sel)
	case hot:
		r.Background(pal.hov)
	}
	r.Children(func() {
		rad := float32(4)
		if dir {
			rad = 2
		}
		ui.Box(c).Size(8, 8).Radius(rad).Background(a.swatchColor(n)).Shrink(0)
		col := pal.t2
		if dir {
			col = pal.text
		}
		if nd.Flags&scan.FlagCloud != 0 {
			col = pal.t3
		}
		ui.Text(c, a.t.Name(n)).TextColor(col).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
		if dir && !slim {
			if k := a.ev.Git(n); k == gitrepo.Repo || k == gitrepo.Worktree {
				ui.Text(c, gitKindLabel(k)).FontSize(10.5).Font(pal.mono).TextColor(pal.t3).Padding(1, 5).Radius(4).Background(pal.hov).SingleLine().Shrink(0)
			}
		}
		switch {
		case slim:
		case lv == safety.Safe:
			ui.Text(c, T("可清理")).FontSize(11.5).TextColor(pal.level[safety.Safe]).SingleLine().Shrink(0)
		case lv == safety.Protected:
			lockIcon(c, 13, pal.t4)
		}
		if wide {
			ui.Text(c, ageStr(a.ev.AgeDays(n))).FontSize(11.5).Width(64).TextAlign(ui.End).TextColor(pal.t4).SingleLine().Shrink(0)
		}
		sw := float32(64)
		if slim {
			sw = 60
		}
		ui.Text(c, fmtSize(nd.Size)).FontSize(12).Font(pal.mono).Width(sw).TextAlign(ui.End).TextColor(pal.t2).SingleLine().Shrink(0)
		if slim && !hot && !sel {
			return // the delete button only under the pointer: the slim column keeps the names
		}
		del := ui.Box(c.Key("del")).Size(26, 26).Radius(6).Shrink(0).Cursor(ui.CursorPointer)
		ic := pal.t4
		if hot || sel {
			ic = pal.t2
		}
		if del.Hovered() {
			if lv == safety.Protected {
				del.Background(pal.hov)
			} else {
				ic = pal.level[safety.Risky]
				del.Background(pal.level[safety.Risky].Alpha(.12))
			}
		}
		del.Children(func() {
			ui.Box(c).Fill().Justify(ui.Center).AlignItems(ui.Center).PassThrough().Children(func() {
				if lv == safety.Protected {
					lockIcon(c, 14, ic)
				} else {
					icon(c, icoTrash, 15, ic, 1.8)
				}
			})
		})
		if lv == safety.Protected {
			del.Tooltip(T("系统保护，禁止删除"))
		} else {
			del.Tooltip(Tf("移至%s（也可拖到%s）", trashName(), trashName()))
		}
		if del.Clicked() && a.anal == nil {
			a.selected = n
			a.throwRow, a.throwNode = r.Bounds(), n
			a.requestTrash(n)
		}
	})
	if sel {
		a.throwRow, a.throwNode = r.Bounds(), n
	}
	// rows slide in after a folder change, one after another
	if k := a.progress(a.rowsT0, time.Duration(min(i, 12))*9*time.Millisecond, 220*time.Millisecond); k < 1 {
		e := float32(easeStd(k))
		r.Translate(a.rowsDx*max(a.rightW, 160)*(1-e), 0)
	}
	// FLIP: after a delete, restore or re-sort, rows glide from where they were
	if d, ok := a.flip[n]; ok && !a.flipT0.IsZero() {
		if math.IsNaN(float64(d)) {
			if k := a.progress(a.flipT0, 0, 200*time.Millisecond); k < 1 {
				r.Translate(max(a.rightW, 160)*(1-float32(easeOut(k))), 0)
			}
		} else if d != 0 {
			if k := a.progress(a.flipT0, 0, 260*time.Millisecond); k < 1 {
				r.Translate(0, d*32*(1-float32(easeOut(k))))
			}
		}
	}
	if dx := a.shakeX(n); dx != 0 {
		r.Translate(dx, 0)
	}
	if a.drag.on && a.drag.node == n {
		r.Opacity(.35)
	}
	if hot && n != a.hover && !a.drag.on {
		a.hover = n
	}
	if r.Clicked() {
		if dir && nd.First != scan.None && a.anal == nil {
			a.zoomTo(n)
		} else {
			a.selected = n
		}
	}
	if r.RightClicked() {
		a.selected = n
	}
	r.ContextMenu(func(m *ui.Menu) { a.nodeMenu(m, n) })
	a.rowDrag(r, n)
}

// ------------------------------------------------------------------------------------------
// detail

// bullet renders one of levelBullets; the one that names the trash is a format.
func bullet(b string) string {
	if strings.Contains(b, "%s") {
		return Tf(b, trashName())
	}
	return T(b)
}

var levelBullets = [4][]string{
	{"内容可自动重建或重新下载", "不包含个人创作数据", "移至%s后可随时还原"},
	{"可能包含应用配置或本地状态", "删除前请确认是否仍在使用"},
	{"可能包含无法恢复的个人文件", "请确认已有备份再删除"},
	{"操作系统或系统服务依赖", "Tossy 禁止删除此项"},
}

func (a *app) habitOf(n int32) *habit.Match {
	if a.habit == nil || !a.set.Habits || n <= 0 {
		return nil
	}
	nd := &a.t.Nodes[n]
	return a.habit.Match(habit.Subject{Path: a.t.Path(n), Dir: nd.IsDir(), Size: nd.Size, AgeDays: a.ev.AgeDays(n)})
}

func (a *app) detail(c *ui.Context) ui.Element {
	pal := a.pal
	return ui.Scroll(c.Key("detail")).Grow(1).Shrink(1).MinHeight(0).Children(func() {
		if a.t == nil {
			return
		}
		n := a.detNode
		if n == scan.None || !a.t.Attached(n) {
			n = a.focus
		}
		nd := &a.t.Nodes[n]
		v := a.verdict(n)
		lv := v.Level()
		dir := nd.IsDir()
		ui.Column(c).Padding(18, 18, 16, 18).Gap(10).Children(func() {
			a.rise(ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, a.name(n)).FontSize(14).FontWeight(600).SingleLine().Ellipsis("…")
				ui.Text(c, a.displayPath(n)).FontSize(11).Font(pal.mono).TextColor(pal.t4).SingleLine().TruncateMiddle()
			}), time.Time{}, 0) // the whole detail slides as a drawer
			a.rise(ui.Row(c).Gap(10).AlignItems(ui.End).Children(func() {
				ui.Text(c, fmtSize(nd.Size)).FontSize(26).FontWeight(600).SingleLine().Shrink(0)
				meta := []string{}
				if dir {
					meta = append(meta, nItems(int64(nd.Count-1)))
				} else {
					meta = append(meta, T(cats[catOf(a.t.Name(n))].l))
				}
				meta = append(meta, Tf("修改于%s", ageStr(a.ev.AgeDays(n))))
				if a.disk.ok && a.disk.total > 0 {
					meta = append(meta, Tf("占磁盘 %s", diskPct(nd.Size, a.disk.total)))
				}
				ui.Text(c, strings.Join(meta, " · ")).FontSize(12).TextColor(pal.t3).SingleLine().Shrink(1).Ellipsis("…").Padding(0, 0, 5, 0)
			}), time.Time{}, 1) // the whole detail slides as a drawer
			// verdict
			a.rise(ui.Row(c).Gap(9).AlignItems(ui.Start).Children(func() {
				dc := pal.level[lv]
				if n == 0 {
					dc = pal.t3
				}
				ui.Box(c).Size(7, 7).Radius(3.5).Background(dc).Margin(6, 0, 0, 0).Shrink(0)
				ui.RichText(c).Grow(1).Shrink(1).FontSize(12.5).Children(func() {
					title, col, reason := T(levelTitles[lv]), pal.level[lv], T(v.Reason())
					if n == 0 {
						title, col, reason = T("扫描范围"), pal.text, T("当前分析的根目录：点击扇区进入子目录，点击中心返回上一级")
					}
					ui.Text(c, title+" ").FontWeight(600).TextColor(col)
					ui.Text(c, reason).TextColor(pal.t2)
				})
			}), time.Time{}, 2) // the whole detail slides as a drawer
			if m := a.habitOf(n); m != nil {
				ui.RichText(c).FontSize(12).Children(func() {
					ui.Text(c, T("依据你的习惯")).FontWeight(600).TextColor(pal.level[safety.Review])
					ui.Text(c, Tf(" · 你曾手动删除 %d 个 %s，自动清理会包含此项", m.D, habitLabel(m.Label))).TextColor(pal.t2)
				})
			}
			title := T("判断依据")
			if n == 0 {
				title = ""
			} else if v.Rule.Tag != "" {
				title += " · " + T(v.Rule.Tag)
			}
			if title != "" {
				a.fold(c, "why", title, &a.whyOpen, func() {
					ui.Column(c).Gap(4).Padding(2, 0, 0, 4).Children(func() {
						for i, b := range levelBullets[lv] {
							ui.Row(c.Key(i)).Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Box(c).Size(4, 4).Radius(2).Background(pal.t4)
								ui.Text(c, bullet(b)).FontSize(12).TextColor(pal.t2)
							})
						}
					})
				})
			}
			if dir {
				a.composition(c, n)
			} else {
				open := !a.prevShut
				a.fold(c, "prev", T("预览"), &open, func() { a.preview(c, n) })
				a.prevShut = !open
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(4, 0, 0, 0).Children(func() {
				if a.btn(c.Key("reveal"), revealLabel(), btnPlain, false, nil).Clicked() {
					mygo.Shell.ShowItemInFolder(a.t.Path(n))
				}
				if dir && n != a.focus && nd.First != scan.None {
					if a.btn(c.Key("open"), T("打开"), btnPlain, false, nil).Clicked() {
						a.zoomTo(n)
					}
				}
				ui.Spacer(c)
				ok, why := a.canTrash(n)
				label := Tf("移至%s", trashName())
				if lv == safety.Protected && n != 0 {
					label = T("受系统保护")
				}
				b := a.btn(c.Key("del"), label, btnSolid, false, nil).Disabled(!ok || a.anal != nil)
				if !ok && why != "" {
					b.Tooltip(why)
				}
				if b.Clicked() {
					a.requestTrash(n)
				}
			})
		})
	})
}

// fold is a <details>: a clickable title with a turning chevron, its body shown while open.
func (a *app) fold(c *ui.Context, key, title string, open *bool, body func()) {
	pal := a.pal
	ui.Column(c.Key(key)).Gap(6).Children(func() {
		h := ui.Row(c.Key("h")).Gap(4).AlignItems(ui.Center).Cursor(ui.CursorPointer).AlignSelf(ui.Start)
		col := pal.t3
		if h.Hovered() {
			col = pal.t2
		}
		h.Children(func() {
			o := *open
			ui.Box(c).Size(12, 12).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
				if o {
					p.Transform(ui.Rotation(90), r.X+r.W/2, r.Y+r.H/2, func() { strokeIcon(p, r, icoChevron, col, 2.4) })
				} else {
					strokeIcon(p, r, icoChevron, col, 2.4)
				}
			})
			ui.Text(c, title).FontSize(12).TextColor(col)
		})
		if h.Clicked() {
			*open = !*open
		}
		if *open {
			body()
		}
	})
}

// ------------------------------------------------------------------------------------------
// file categories

type cat struct {
	c ui.Color
	l string
}

const (
	catVideo = iota
	catImage
	catAudio
	catArchive
	catCode
	catDoc
	catData
	catLog
	catOther
)

var cats = [...]cat{
	{ui.Hex("#c98aa8"), "视频"}, {ui.Hex("#7d9fd0"), "图片"}, {ui.Hex("#a891c9"), "音频"},
	{ui.Hex("#c9a466"), "安装包/归档"}, {ui.Hex("#79b39a"), "代码/构建"}, {ui.Hex("#9b93c4"), "文档"},
	{ui.Hex("#6fb0b8"), "数据/虚拟磁盘"}, {ui.Hex("#8b8f99"), "日志"}, {ui.Hex("#62656e"), "其他/缓存"},
}

var catExt = func() map[string]int {
	m := map[string]int{}
	for k, s := range []string{
		"mp4 mov mkv avi m4v webm",
		"jpg jpeg png heic gif webp psd svg tiff raw",
		"mp3 wav flac m4a aac ogg",
		"dmg pkg xip iso zip gz tar exe msi deb rpm appimage run snap crate jar emlx 7z rar xz bz2 zst",
		"js mjs cjs ts tsx css html json go rs swift py pyc map o d a h c cc cpp swiftmodule rlib dylib so dll framework bundle kext tbd dia pom app dsym lproj nib car class wasm",
		"pdf docx doc md txt key pages xlsx pptx csv strings rtf epub",
		"db sqlite ldb plist dat bin raw vhdx unit pack edb vpk pak vdm journal img qcow2 vmdk",
		"log xcactivitylog dmp crash ips",
	} {
		for _, e := range strings.Fields(s) {
			if _, ok := m[e]; !ok {
				m[e] = k
			}
		}
	}
	return m
}()

func catOf(name string) int {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if k, ok := catExt[ext]; ok {
		return k
	}
	return catOther
}

type compCache struct {
	node int32
	ver  int
	sums [len(cats)]int64
	top  []int32
}

func (a *app) compOf(n int32) *compCache {
	cc := &a.comp
	if cc.node == n && cc.ver == a.ver && cc.top != nil {
		return cc
	}
	*cc = compCache{node: n, ver: a.ver, top: []int32{}}
	t := a.t
	stack := []int32{n}
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for ch := t.Nodes[i].First; ch != scan.None; ch = t.Nodes[ch].Next {
			if t.Nodes[ch].IsDir() {
				stack = append(stack, ch)
				continue
			}
			cc.sums[catOf(t.Name(ch))] += t.Nodes[ch].Size
			if len(cc.top) < 5 || t.Nodes[ch].Size > t.Nodes[cc.top[4]].Size {
				cc.top = append(cc.top, ch)
				sort.Slice(cc.top, func(x, y int) bool { return t.Nodes[cc.top[x]].Size > t.Nodes[cc.top[y]].Size })
				if len(cc.top) > 5 {
					cc.top = cc.top[:5]
				}
			}
		}
	}
	return cc
}

func (a *app) composition(c *ui.Context, n int32) {
	pal := a.pal
	cc := a.compOf(n)
	tot := max(a.t.Nodes[n].Size, 1)
	ks := make([]int, 0, len(cats))
	for k, s := range cc.sums {
		if s > 0 {
			ks = append(ks, k)
		}
	}
	sort.Slice(ks, func(i, j int) bool { return cc.sums[ks[i]] > cc.sums[ks[j]] })
	if len(ks) == 0 {
		return
	}
	ui.Box(c.Key("compbar")).Height(4).Radius(2).Clip().Tooltip(T("内容构成")).Draw(func(p *ui.Painter, r ui.Rect) {
		x := r.X
		for _, k := range ks {
			w := r.W * float32(cc.sums[k]) / float32(tot)
			p.Fill(ui.Rect{X: x, Y: r.Y, W: w, H: r.H}, cats[k].c, 0)
			x += w
		}
	})
	a.fold(c, "comp", T("构成与最大的文件"), &a.compOpen, func() {
		line := func(key any, col ui.Color, name string, size int64) ui.Element {
			return ui.Row(c.Key(key)).Gap(8).AlignItems(ui.Center).Height(22).Padding(0, 4).Radius(5).Children(func() {
				ui.Box(c).Size(7, 7).Radius(3.5).Background(col).Shrink(0)
				ui.Text(c, name).FontSize(12).TextColor(pal.t2).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
				ui.Text(c, fmtSize(size)).FontSize(12).Font(pal.mono).TextColor(pal.t3).SingleLine()
			})
		}
		ui.Column(c).Gap(0).Children(func() {
			for i, k := range ks[:min(6, len(ks))] {
				line(fmt.Sprint("k", i), cats[k].c, T(cats[k].l), cc.sums[k])
			}
			if len(cc.top) > 0 {
				ui.Text(c, T("最大的文件")).FontSize(11).TextColor(pal.t4).Padding(8, 4, 3, 4)
				for i, f := range cc.top {
					r := line(fmt.Sprint("f", i), cats[catOf(a.t.Name(f))].c, a.t.Name(f), a.t.Nodes[f].Size).Cursor(ui.CursorPointer)
					if r.Hovered() {
						r.Background(pal.hov)
						a.hover = f
					}
					if r.Clicked() && a.anal == nil {
						a.zoomTo(a.t.Nodes[f].Parent)
						a.selected = f
					}
				}
			}
		})
	})
}

// ------------------------------------------------------------------------------------------
// preview: the head of the file, as text or a hex dump

type previewCache struct {
	node  int32
	ver   int
	lines []string
	err   string
}

func (a *app) preview(c *ui.Context, n int32) {
	pal := a.pal
	pc := &a.prev
	if pc.node != n || pc.ver != a.ver || (pc.lines == nil && pc.err == "") {
		*pc = previewCache{node: n, ver: a.ver}
		pc.lines, pc.err = readHead(a.t.Path(n))
	}
	ui.Column(c.Key("pv")).Padding(10, 12).Radius(8).Background(pal.panel2).Clip().Children(func() {
		if pc.err != "" {
			ui.Text(c, pc.err).FontSize(11.5).TextColor(pal.t3)
			return
		}
		for i, l := range pc.lines {
			ui.Text(c.Key(i), l).FontSize(11).Font(pal.mono).TextColor(pal.t2).SingleLine().Ellipsis("…")
		}
	})
}

func readHead(path string) ([]string, string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, Tf("无法读取：%s", firstLine(err.Error()))
	}
	defer f.Close()
	buf := make([]byte, 1024)
	k, _ := io.ReadFull(f, buf)
	buf = buf[:k]
	if k == 0 {
		return nil, T("空文件")
	}
	if isText(buf) {
		s := strings.ReplaceAll(string(buf), "\r\n", "\n")
		if !utf8.ValidString(s) { // cut mid-rune at the end
			s = strings.ToValidUTF8(s, "")
		}
		lines := strings.Split(s, "\n")
		if len(lines) > 9 {
			lines = lines[:9]
		}
		for i, l := range lines {
			lines[i] = strings.ReplaceAll(l, "\t", "    ")
		}
		return lines, ""
	}
	var lines []string
	for off := 0; off < len(buf) && len(lines) < 8; off += 8 {
		end := min(off+8, len(buf))
		var hx strings.Builder
		for _, b := range buf[off:end] {
			fmt.Fprintf(&hx, "%02x ", b)
		}
		lines = append(lines, fmt.Sprintf("%08x  %-24s %s", off, hx.String(), printable(buf[off:end])))
	}
	return lines, ""
}

func isText(b []byte) bool {
	bad := 0
	for _, c := range b {
		if c == 0 {
			return false
		}
		if c < 9 || (c > 13 && c < 32) {
			bad++
		}
	}
	return bad*30 < len(b) && utf8.Valid(trimRune(b))
}

func trimRune(b []byte) []byte {
	for i := 0; i < 4 && len(b) > 0 && !utf8.Valid(b); i++ {
		b = b[:len(b)-1]
	}
	return b
}

func printable(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if c >= 32 && c < 127 {
			out[i] = c
		} else {
			out[i] = '.'
		}
	}
	return string(out)
}
