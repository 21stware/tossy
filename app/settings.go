package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// settings persist in <state>/settings.json. Every field is comparable so the view can save
// whenever the value differs from what is on disk.
type settings struct {
	Style        string `json:"style"`
	ReduceMotion bool   `json:"reduceMotion"`

	Root        string `json:"root"`        // default scan root; "" = home
	Exclude     string `json:"exclude"`     // one absolute path per line
	CrossDevice bool   `json:"crossDevice"` // descend into other volumes

	Review   bool    `json:"review"`   // show the evaluation sheet before cleaning
	Analysis bool    `json:"analysis"` // show the analysis sweep
	InUseMin float64 `json:"inUseMin"` // skip items written within this many minutes
	SkipApps bool    `json:"skipApps"` // skip caches of running apps

	Habits bool `json:"habits"` // learn from manual deletions
}

func defaultSettings() settings {
	return settings{Style: palettes[0].name, Review: true, Analysis: true, InUseMin: 10, SkipApps: true, Habits: true}
}

func (a *app) settingsPath() string { return filepath.Join(stateDir(), "settings.json") }

func (a *app) loadSettings() {
	a.set = defaultSettings()
	if b, err := os.ReadFile(a.settingsPath()); err == nil {
		json.Unmarshal(b, &a.set)
	}
	a.saved = a.set
	if i, _ := paletteByName(a.set.Style); i >= 0 {
		a.palIdx = i
	}
}

// syncSettings runs every frame: it folds UI state into the settings and saves on change.
func (a *app) syncSettings() {
	a.set.Style = palettes[a.palIdx].name
	if a.set == a.saved {
		return
	}
	a.saved = a.set
	if os.Getenv("FANFREE_STATE") == "" && underTest() {
		return
	}
	os.MkdirAll(stateDir(), 0o755)
	b, _ := json.MarshalIndent(a.set, "", "  ")
	os.WriteFile(a.settingsPath(), b, 0o644)
}

// underTest is true under `go test` (no window), where settings must not touch the user's.
func underTest() bool { return strings.HasSuffix(os.Args[0], ".test") || strings.Contains(os.Args[0], "/_test/") }

func (a *app) excludes() []string {
	var out []string
	for _, l := range strings.Split(a.set.Exclude, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if strings.HasPrefix(l, "~") {
				if h, err := os.UserHomeDir(); err == nil {
					l = filepath.Join(h, l[1:])
				}
			}
			out = append(out, l)
		}
	}
	return out
}

func (a *app) motion() bool { return a.pal.motion && !a.set.ReduceMotion }

// defaultRoot is where a fresh launch scans.
func (a *app) defaultRoot() string {
	if a.set.Root != "" {
		if st, err := os.Stat(a.set.Root); err == nil && st.IsDir() {
			return a.set.Root
		}
	}
	h, _ := os.UserHomeDir()
	return h
}

// pickFolder asks for a directory off the main thread, then calls done with it on the main thread.
func (a *app) pickFolder(title string, done func(string)) {
	if a.win == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: title, Directory: true, DefaultPath: a.root})
		if err != nil || len(paths) == 0 {
			return
		}
		a.onMain(func() { done(paths[0]) })
	}()
}

func revealLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "在访达中显示"
	case "windows":
		return "在资源管理器中显示"
	}
	return "在文件管理器中显示"
}

// ------------------------------------------------------------------------------------------
// panel


func (a *app) settingsModal(c *ui.Context) {
	pal := a.pal
	ui.Modal(c.Key("settings"), &a.showSettings, func() {
		ui.Column(c).Width(720).Height(600).Children(func() {
			ui.Row(c).Padding(16, 20).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "设置").FontSize(16).Bold().Grow(1)
				if ui.Button(c.Key("close"), "完成").Clicked() {
					a.showSettings = false
				}
			})
			ui.Box(c).Height(1).Background(pal.line)
			ui.Scroll(c.Key("body")).Grow(1).Children(func() {
				ui.Column(c).Padding(16, 20).Gap(22).Children(func() {
					a.setAppearance(c)
					a.setScan(c)
					a.setClean(c)
					a.setHabits(c)
					a.setTrash(c)
				})
			})
		})
	})
}

func (a *app) section(c *ui.Context, key, title string, fn func()) {
	pal := a.pal
	ui.Column(c.Key(key)).Gap(10).Children(func() {
		ui.Text(c, title).FontSize(12).Bold().TextColor(pal.t3)
		fn()
	})
}

// option is one labelled control row: label and hint on the left, the control on the right.
func (a *app) option(c *ui.Context, key, label, hint string, control func()) {
	pal := a.pal
	ui.Row(c.Key(key)).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
			ui.Text(c, label)
			if hint != "" {
				ui.Text(c, hint).FontSize(11.5).TextColor(pal.t3)
			}
		})
		control()
	})
}

func (a *app) setAppearance(c *ui.Context) {
	pal := a.pal
	a.section(c, "look", "外观", func() {
		ui.Row(c.Key("styles")).Wrap().Gap(10).Children(func() {
			for i, sp := range palettes {
				sel := i == a.palIdx
				card := ui.Column(c.Key(sp.name)).Width(156).Gap(6).Padding(8).Radius(8).Cursor(ui.CursorPointer).
					Border(1, pal.line).Children(func() {
					ui.Box(c).Height(74).Radius(5).Background(sp.bg).Draw(func(p *ui.Painter, r ui.Rect) { paintSwatch(p, r, sp) })
					ui.Text(c, sp.label).Bold()
					ui.Text(c, sp.desc).FontSize(10.5).TextColor(pal.t3).MaxLines(2).Ellipsis("…")
				})
				if sel {
					card.Border(2, pal.accent)
				}
				if card.Clicked() {
					a.palIdx = i
				}
			}
		})
		a.option(c, "rm", "减少动态效果", "关闭缩放过渡、开场动画与飞入废纸篓动画（墨水屏风格始终关闭）", func() {
			ui.Switch(c.Key("sw"), &a.set.ReduceMotion)
		})
	})
}

// paintSwatch draws a miniature of a style: its backdrop, a ring of ramp colours, an accent.
func paintSwatch(p *ui.Painter, r ui.Rect, sp *palette) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	r0, r1 := float32(10), float32(30)
	n := 9
	for k := 0; k < n; k++ {
		b0 := a0Top + float64(k)/float64(n)*tau
		b1 := a0Top + float64(k+1)/float64(n)*tau - .06
		h, s, l := sp.rampAt(float32(k) / float32(n))
		var path ui.Path
		sector(&path, cx, cy, r0, r1, float32(b0), float32(b1), min(sp.corner, 2))
		switch sp.art {
		case artHolo, artPhosphor:
			p.FillPath(&path, hsl(h, s, l*.8, .3))
			p.StrokePath(&path, .8, hsl(h, s, min(86, l+14), .9))
		case artBlueprint:
			p.FillPath(&path, ui.RGBA(255, 255, 255, .08))
			p.StrokePath(&path, .8, ui.RGBA(255, 255, 255, .9))
		case artInk, artPlate, artTE:
			p.FillPath(&path, hsl(h, s, l, 1))
			p.StrokePath(&path, .7, sp.ink.Alpha(.6))
		default:
			p.FillPath(&path, hsl(h, s, l, 1))
		}
	}
	p.Fill(ui.Rect{X: r.X + 8, Y: r.Y + r.H - 14, W: 22, H: 6}, sp.accent, 3)
	p.Fill(ui.Rect{X: r.X + 34, Y: r.Y + r.H - 14, W: 12, H: 6}, sp.level[0], 3)
	p.Fill(ui.Rect{X: r.X + 50, Y: r.Y + r.H - 14, W: 12, H: 6}, sp.level[2], 3)
}

func (a *app) setScan(c *ui.Context) {
	pal := a.pal
	a.section(c, "scan", "扫描", func() {
		root := a.set.Root
		if root == "" {
			root = "主目录（默认）"
		}
		a.option(c, "root", "启动时扫描", root, func() {
			ui.Row(c).Gap(6).Children(func() {
				if a.set.Root != "" && ui.Button(c.Key("reset"), "恢复默认").Clicked() {
					a.set.Root = ""
				}
				if ui.Button(c.Key("pick"), "选择…").Clicked() {
					a.pickFolder("选择默认扫描位置", func(p string) { a.set.Root = p })
				}
			})
		})
		a.option(c, "xdev", "跨越其他磁盘", "扫描挂载在目录内的其他卷（默认只扫描同一磁盘，和 du -x 一致）", func() {
			ui.Switch(c.Key("sw"), &a.set.CrossDevice)
		})
		ui.Column(c.Key("ex")).Gap(6).Children(func() {
			ui.Text(c, "排除的位置")
			ui.Text(c, "每行一个路径，可用 ~ 表示主目录；排除的目录只记录、不进入。下次扫描生效。").FontSize(11.5).TextColor(pal.t3)
			ui.TextArea(c.Key("ta"), &a.set.Exclude).Height(74).Font("monospace").FontSize(12)
		})
	})
}

func (a *app) setClean(c *ui.Context) {
	a.section(c, "clean", "清理", func() {
		a.option(c, "review", "清理前先复核方案", "分析结束后在右侧列出每一项，可逐项取消；关闭则分析后直接清理", func() {
			ui.Switch(c.Key("sw"), &a.set.Review)
		})
		a.option(c, "anal", "显示分析扫描", "清理前的扫描动画，同时检查文件占用", func() {
			ui.Switch(c.Key("sw"), &a.set.Analysis)
		})
		a.option(c, "apps", "跳过正在运行的应用的缓存", "如浏览器、Slack、VS Code、Xcode 正在运行，跳过它们的缓存", func() {
			ui.Switch(c.Key("sw"), &a.set.SkipApps)
		})
		a.option(c, "inuse", "跳过最近写入的项目", fmt.Sprintf("%d 分钟内有写入的缓存视为正在使用", int(a.set.InUseMin)), func() {
			ui.Slider(c.Key("sl"), &a.set.InUseMin, 0, 120).Width(180)
		})
	})
}

func (a *app) setHabits(c *ui.Context) {
	pal := a.pal
	a.section(c, "habits", "清理习惯", func() {
		a.option(c, "on", "从手动删除中学习", "同类项目手动删除多次后，自动清理会一并包含；从废纸篓恢复会让习惯减弱", func() {
			ui.Switch(c.Key("sw"), &a.set.Habits)
		})
		if a.habit == nil {
			return
		}
		es := a.habit.Entries()
		keys := make([]string, 0, len(es))
		for k := range es {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return es[keys[i]].D > es[keys[j]].D })
		if len(keys) == 0 {
			ui.Text(c, "还没有学到习惯").FontSize(12).TextColor(pal.t3)
		}
		for _, k := range keys {
			e := es[k]
			ui.Row(c.Key(k)).Gap(10).Padding(6, 10).Radius(6).Background(pal.panel2).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
					label := e.Label
					if label == "" {
						label = k
					}
					ui.Text(c, label).SingleLine().Ellipsis("…")
					sub := fmt.Sprintf("手动删除 %d 次 · 恢复 %d 次 · 最大 %s", e.D, e.R, fmtSize(e.Max))
					if e.Seed {
						sub += " · 示例"
					}
					ui.Text(c, sub).FontSize(11).TextColor(pal.t3)
				})
				if ui.Button(c.Key("forget"), "忘记").Clicked() {
					a.habit.Forget(k)
					a.habit.Save()
					a.plan = nil
				}
			})
		}
	})
}

func (a *app) setTrash(c *ui.Context) {
	pal := a.pal
	a.section(c, "trash", "废纸篓", func() {
		ui.Text(c, "FanFree 把清理的项目移到系统废纸篓，并记录来源以便恢复与学习。清空只删除 FanFree 移入的项目。").FontSize(12).TextColor(pal.t2)
		ui.Text(c, "记录位置："+stateDir()).FontSize(11).Font("monospace").TextColor(pal.t3).SingleLine().TruncateMiddle()
	})
}
