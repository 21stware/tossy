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

	"github.com/fanfree/fanfree/engine/habit"
	"github.com/fanfree/fanfree/engine/safety"
	"github.com/fanfree/fanfree/engine/scan"
)

// The browse scene: the folder in focus as a list, and the selection in detail.

func (a *app) browsePanel(c *ui.Context) {
	pal := a.pal
	ui.Column(c.Key("browse")).Grow(1).Shrink(1).Children(func() {
		a.listHeader(c)
		a.throwNode = scan.None
		if a.t == nil {
			ui.Box(c).Grow(1)
		} else {
			rows := a.children()
			a.list.Key = func(i int) any { return rows[i] }
			ui.List(c.Key("list"), &a.list, len(rows), func(i int) { a.row(c, rows[i], i) }).Grow(1).Shrink(1).Padding(2, 0, 6, 0).Children(func() {
				if len(rows) == 0 {
					ui.Text(c, "此文件夹为空").TextColor(pal.t3).FontSize(12.5).Padding(24).AlignSelf(ui.Center)
				}
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
			if a.btn(c.Key("lup"), "", btnGhost, true, func() { icon(c, icoUp, 15, pal.t2, 2) }).Tooltip("返回上级（Esc）").Clicked() && a.anal == nil {
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
			ui.Text(c, fmtSize(a.t.Nodes[a.focus].Size)+" · "+fmtNum(int64(len(a.children())))+" 项").FontSize(12).TextColor(pal.t3).SingleLine()
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
		switch {
		case slim:
		case lv == safety.Safe:
			ui.Text(c, "可清理").FontSize(11.5).TextColor(pal.level[safety.Safe]).SingleLine().Shrink(0)
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
			del.Tooltip("系统保护，禁止删除")
		} else {
			del.Tooltip("移至" + trashName() + "（也可拖到" + trashName() + "）")
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
		r.Opacity(e).Translate(a.rowsDx*(1-e), 0)
	}
	// FLIP: after a delete, restore or re-sort, rows glide from where they were
	if d, ok := a.flip[n]; ok && !a.flipT0.IsZero() {
		if math.IsNaN(float64(d)) {
			if k := a.progress(a.flipT0, 0, 200*time.Millisecond); k < 1 {
				e := float32(easeOut(k))
				r.Opacity(e).Scale(.98+.02*e, .98+.02*e)
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
		a.selected = n
	}
	if r.DoubleClicked() && dir && nd.First != scan.None && a.anal == nil {
		a.zoomTo(n)
	}
	if r.RightClicked() {
		a.selected = n
	}
	r.ContextMenu(func(m *ui.Menu) { a.nodeMenu(m, n) })
	a.rowDrag(r, n)
}

// ------------------------------------------------------------------------------------------
// detail

var levelBullets = [4][]string{
	{"内容可自动重建或重新下载", "不包含个人创作数据", "移至" + trashName() + "后可随时还原"},
	{"可能包含应用配置或本地状态", "删除前请确认是否仍在使用"},
	{"可能包含无法恢复的个人文件", "请确认已有备份再删除"},
	{"操作系统或系统服务依赖", "FanFree 禁止删除此项"},
}

func (a *app) habitOf(n int32) *habit.Match {
	if a.habit == nil || !a.set.Habits || n <= 0 {
		return nil
	}
	nd := &a.t.Nodes[n]
	return a.habit.Match(habit.Subject{Path: a.t.Path(n), Dir: nd.IsDir(), Size: nd.Size, AgeDays: a.ev.AgeDays(n)})
}

// detailSummary is the detail panel at rest: the selection's name, size and verdict in two
// lines. A press on it brings the full detail.
func (a *app) detailSummary(c *ui.Context) {
	pal := a.pal
	if a.t == nil {
		return
	}
	n := a.selected
	if n == scan.None || !a.t.Attached(n) {
		n = a.focus
	}
	nd := &a.t.Nodes[n]
	lv := a.verdict(n).Level()
	ui.Column(c.Key("dsum")).Padding(13, 14, 12, 18).Gap(5).Cursor(ui.CursorPointer).Children(func() {
		a.rise(ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.name(n)).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
			icon(c, icoChevron, 12, pal.t4, 1.8)
		}), a.detT0, 0)
		a.rise(ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmtSize(nd.Size)).FontSize(18).FontWeight(600).SingleLine().Shrink(0)
			if n != 0 {
				ui.Box(c).Size(6, 6).Radius(3).Background(pal.level[lv]).Shrink(0)
				ui.Text(c, levelNames[lv]).FontSize(11.5).TextColor(pal.level[lv]).SingleLine().Shrink(1).Ellipsis("…")
			}
		}), a.detT0, 1)
	})
}

// detailLine is the detail squeezed to one line (the trash has the room): name and size.
func (a *app) detailLine(c *ui.Context) {
	pal := a.pal
	if a.t == nil {
		return
	}
	n := a.selected
	if n == scan.None || !a.t.Attached(n) {
		n = a.focus
	}
	a.rise(ui.Row(c.Key("dline")).Height(detailMin).Padding(0, 14, 0, 18).Gap(8).AlignItems(ui.Center).Cursor(ui.CursorPointer).Children(func() {
		ui.Text(c, a.name(n)).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
		ui.Text(c, fmtSize(a.t.Nodes[n].Size)).FontSize(12).Font(pal.mono).TextColor(pal.t3).SingleLine().Shrink(0)
	}), a.detT0, 0)
}

func (a *app) detail(c *ui.Context) ui.Element {
	pal := a.pal
	return ui.Scroll(c.Key("detail")).Grow(1).Shrink(1).MinHeight(0).Children(func() {
		if a.t == nil {
			return
		}
		n := a.selected
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
			}), a.detT0, 0)
			a.rise(ui.Row(c).Gap(10).AlignItems(ui.End).Children(func() {
				ui.Text(c, fmtSize(nd.Size)).FontSize(26).FontWeight(600).SingleLine().Shrink(0)
				meta := []string{}
				if dir {
					meta = append(meta, fmtNum(int64(nd.Count-1))+" 项")
				} else {
					meta = append(meta, cats[catOf(a.t.Name(n))].l)
				}
				meta = append(meta, "修改于"+ageStr(a.ev.AgeDays(n)))
				if a.disk.ok && a.disk.total > 0 {
					meta = append(meta, "占磁盘 "+diskPct(nd.Size, a.disk.total))
				}
				ui.Text(c, strings.Join(meta, " · ")).FontSize(12).TextColor(pal.t3).SingleLine().Shrink(1).Ellipsis("…").Padding(0, 0, 5, 0)
			}), a.detT0, 1)
			// verdict
			a.rise(ui.Row(c).Gap(9).AlignItems(ui.Start).Children(func() {
				dc := pal.level[lv]
				if n == 0 {
					dc = pal.t3
				}
				ui.Box(c).Size(7, 7).Radius(3.5).Background(dc).Margin(6, 0, 0, 0).Shrink(0)
				ui.RichText(c).Grow(1).Shrink(1).FontSize(12.5).Children(func() {
					title, col, reason := levelTitles[lv], pal.level[lv], v.Reason()
					if n == 0 {
						title, col, reason = "扫描范围", pal.text, "当前分析的根目录：点击扇区进入子目录，点击中心返回上一级"
					}
					ui.Text(c, title+" ").FontWeight(600).TextColor(col)
					ui.Text(c, reason).TextColor(pal.t2)
				})
			}), a.detT0, 2)
			if m := a.habitOf(n); m != nil {
				ui.RichText(c).FontSize(12).Children(func() {
					ui.Text(c, "依据你的习惯").FontWeight(600).TextColor(pal.level[safety.Review])
					ui.Text(c, fmt.Sprintf(" · 你曾手动删除 %d 个 %s，自动清理会包含此项", m.D, m.Label)).TextColor(pal.t2)
				})
			}
			title := "判断依据"
			if n == 0 {
				title = ""
			} else if v.Rule.Tag != "" {
				title += " · " + v.Rule.Tag
			}
			if title != "" {
				a.fold(c, "why", title, &a.whyOpen, func() {
					ui.Column(c).Gap(4).Padding(2, 0, 0, 4).Children(func() {
						for i, b := range levelBullets[lv] {
							ui.Row(c.Key(i)).Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Box(c).Size(4, 4).Radius(2).Background(pal.t4)
								ui.Text(c, b).FontSize(12).TextColor(pal.t2)
							})
						}
					})
				})
			}
			if dir {
				a.composition(c, n)
			} else {
				open := !a.prevShut
				a.fold(c, "prev", "预览", &open, func() { a.preview(c, n) })
				a.prevShut = !open
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(4, 0, 0, 0).Children(func() {
				if a.btn(c.Key("reveal"), revealLabel(), btnPlain, false, nil).Clicked() {
					mygo.Shell.ShowItemInFolder(a.t.Path(n))
				}
				if dir && n != a.focus && nd.First != scan.None {
					if a.btn(c.Key("open"), "打开", btnPlain, false, nil).Clicked() {
						a.zoomTo(n)
					}
				}
				ui.Spacer(c)
				ok, why := a.canTrash(n)
				label := "移至" + trashName()
				if lv == safety.Protected && n != 0 {
					label = "受系统保护"
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
	ui.Box(c.Key("compbar")).Height(4).Radius(2).Clip().Tooltip("内容构成").Draw(func(p *ui.Painter, r ui.Rect) {
		x := r.X
		for _, k := range ks {
			w := r.W * float32(cc.sums[k]) / float32(tot)
			p.Fill(ui.Rect{X: x, Y: r.Y, W: w, H: r.H}, cats[k].c, 0)
			x += w
		}
	})
	a.fold(c, "comp", "构成与最大的文件", &a.compOpen, func() {
		line := func(key any, col ui.Color, name string, size int64) ui.Element {
			return ui.Row(c.Key(key)).Gap(8).AlignItems(ui.Center).Height(22).Padding(0, 4).Radius(5).Children(func() {
				ui.Box(c).Size(7, 7).Radius(3.5).Background(col).Shrink(0)
				ui.Text(c, name).FontSize(12).TextColor(pal.t2).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
				ui.Text(c, fmtSize(size)).FontSize(12).Font(pal.mono).TextColor(pal.t3).SingleLine()
			})
		}
		ui.Column(c).Gap(0).Children(func() {
			for i, k := range ks[:min(6, len(ks))] {
				line(fmt.Sprint("k", i), cats[k].c, cats[k].l, cc.sums[k])
			}
			if len(cc.top) > 0 {
				ui.Text(c, "最大的文件").FontSize(11).TextColor(pal.t4).Padding(8, 4, 3, 4)
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
		return nil, "无法读取：" + firstLine(err.Error())
	}
	defer f.Close()
	buf := make([]byte, 1024)
	k, _ := io.ReadFull(f, buf)
	buf = buf[:k]
	if k == 0 {
		return nil, "空文件"
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
