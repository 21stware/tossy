package main

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// artStyle is how sunburst sectors are painted.
type artStyle int

const (
	artSolid     artStyle = iota // flat fills (classic)
	artHolo                      // translucent bodies, bright hairline edges (HUD)
	artPlate                     // cel-shaded armour plates with ink outlines (mech)
	artBlueprint                 // white linework on blue, faint washes
	artPhosphor                  // green phosphor: dim bodies, glowing edges
	artTE                        // light grey hardware plates, one orange accent per ring
	artInk                       // e-ink: greys, black outlines, hatching, no motion
	artToon                      // cartoon cels: candy fills, thick navy ink, a glossy highlight
)

// instrStyle is the instrument overlay drawn around the chart.
type instrStyle int

const (
	instrNone instrStyle = iota
	instrHUD
	instrMech
	instrBlueprint
	instrTerminal
	instrTE
)

// deco is painted behind the chart.
type deco int

const (
	decoNone   deco = iota
	decoGrid        // drafting grid
	decoScan        // CRT scanlines + vignette
	decoDots        // dot matrix
	decoPaper       // faint paper fibres
	decoPolka       // big soft polka dots
	decoHearts      // scattered little hearts
	decoPaint       // a painter's splashes and drops
)

// palette is everything a visual style decides; views read only from here.
type palette struct {
	name, label, desc string
	dark              bool

	bg, panel, panel2, line, hov, sel ui.Color
	text, t2, t3, t4                  ui.Color
	accent, accentText, amber         ui.Color
	level                             [4]ui.Color // safe, review, risky, protected

	// sunburst
	ramp      [][4]float32 // stops: t, hue, sat, light
	art       artStyle
	instr     instrStyle
	deco      deco
	agg       ui.Color // aggregated tail of small items
	ink       ui.Color // instruments, reticle, bezel, outlines
	labelText ui.Color // labels on translucent styles
	corner    float32  // sector corner radius
	gap       float32  // ring gap
	motion    bool     // zoom tweens, intro, flights; off for e-ink
	rule      float32  // a hairline between the chart and the side panel (0: none)
	glow      bool

	// controls
	font   string // UI font family ("" = system)
	mono   string // numbers and readouts
	radius float32
}

// holo styles paint translucent bodies with bright edges and light labels.
func (p *palette) holo() bool {
	return p.art == artHolo || p.art == artPhosphor || p.art == artBlueprint
}

var levelsDefault = [4]ui.Color{ui.Hex("#6fc39a"), ui.Hex("#d4a656"), ui.Hex("#de7f86"), ui.Hex("#7f838e")}

var palettes = []*palette{
	{
		name: "hud", label: "HUD", desc: "全息座舱：半透明扇区、发光描边与仪表环", dark: true,
		bg: ui.Hex("#0d1013"), panel: ui.Hex("#13171a"), panel2: ui.Hex("#1a1f23"), line: ui.RGBA(127, 231, 242, .08),
		hov: ui.RGBA(127, 231, 242, .05), sel: ui.RGBA(127, 231, 242, .10),
		text: ui.Hex("#e6f3f2"), t2: ui.Hex("#9fb3b3"), t3: ui.Hex("#6c7c7d"), t4: ui.Hex("#45514f"),
		accent: ui.Hex("#5fdccf"), accentText: ui.Hex("#03211e"), amber: ui.Hex("#ffba5c"),
		level: levelsDefault,
		ramp:  [][4]float32{{0, 186, 72, 60}, {.3, 198, 70, 62}, {.55, 214, 60, 66}, {.75, 168, 58, 56}, {1, 186, 72, 60}},
		art:   artHolo, instr: instrHUD, agg: ui.RGBA(127, 231, 242, .05), ink: ui.RGB(127, 231, 242), labelText: ui.RGBA(225, 252, 250, .92),
		corner: 4, gap: 1.5, motion: true, mono: "monospace", radius: 6,
	},
	{
		name: "mech", label: "机甲", desc: "浅色装甲板：钴蓝、信号红、警示黄与倒角", dark: false,
		bg: ui.Hex("#e4e7eb"), panel: ui.Hex("#f1f3f5"), panel2: ui.Hex("#d6dae0"), line: ui.RGBA(22, 34, 64, .13),
		hov: ui.RGBA(22, 34, 64, .07), sel: ui.RGBA(31, 79, 168, .12),
		text: ui.Hex("#141a26"), t2: ui.Hex("#444e62"), t3: ui.Hex("#737c8f"), t4: ui.Hex("#9ea6b4"),
		accent: ui.Hex("#1f4fa8"), accentText: ui.Hex("#ffffff"), amber: ui.Hex("#d7262e"),
		level: [4]ui.Color{ui.Hex("#1d8a58"), ui.Hex("#c08400"), ui.Hex("#d7262e"), ui.Hex("#6b7385")},
		ramp: [][4]float32{{0, 224, 58, 36}, {.15, 224, 58, 36}, {.19, 216, 68, 52}, {.3, 216, 68, 52}, {.34, 214, 12, 86}, {.45, 214, 12, 86},
			{.46, 356, 12, 86}, {.49, 356, 72, 54}, {.6, 356, 72, 54}, {.64, 404, 92, 56}, {.75, 404, 92, 56}, {.76, 404, 6, 66},
			{.77, 212, 8, 62}, {.9, 212, 10, 54}, {.94, 224, 58, 36}, {1, 224, 58, 36}},
		art: artPlate, instr: instrMech, agg: ui.RGBA(20, 32, 64, .07), ink: ui.RGB(22, 34, 68), labelText: ui.Hex("#141a26"),
		corner: 1.5, gap: 2, motion: true, mono: "monospace", radius: 2,
	},
	{
		name: "classic", label: "经典", desc: "安静的暗色界面，柔和的实色扇区", dark: true,
		bg: ui.Hex("#131315"), panel: ui.Hex("#1a1a1d"), panel2: ui.Hex("#222226"), line: ui.RGBA(255, 255, 255, .06),
		hov: ui.RGBA(255, 255, 255, .035), sel: ui.RGBA(255, 255, 255, .065),
		text: ui.Hex("#ececef"), t2: ui.Hex("#a3a3ab"), t3: ui.Hex("#74747d"), t4: ui.Hex("#4e4e56"),
		accent: ui.Hex("#ececef"), accentText: ui.Hex("#111114"), amber: ui.Hex("#d4a656"),
		level: levelsDefault,
		ramp:  [][4]float32{{0, 214, 42, 62}, {.2, 244, 34, 66}, {.38, 285, 28, 66}, {.54, 340, 34, 66}, {.7, 382, 42, 64}, {.84, 404, 44, 60}, {1, 520, 28, 56}},
		art:   artSolid, agg: ui.RGBA(255, 255, 255, .045), ink: ui.RGB(255, 255, 255), labelText: ui.RGBA(255, 255, 255, .92),
		corner: 4, gap: 1.5, motion: true, mono: "monospace", radius: 6,
	},
	{
		name: "blueprint", label: "蓝图", desc: "工程蓝图：普鲁士蓝底、白色线稿、尺寸标注与图签", dark: true,
		bg: ui.Hex("#123f73"), panel: ui.Hex("#0f3766"), panel2: ui.Hex("#174a84"), line: ui.RGBA(255, 255, 255, .16),
		hov: ui.RGBA(255, 255, 255, .06), sel: ui.RGBA(255, 255, 255, .12),
		text: ui.Hex("#f2f7ff"), t2: ui.Hex("#c4d6ee"), t3: ui.Hex("#8eaad0"), t4: ui.Hex("#6283b0"),
		accent: ui.Hex("#ffffff"), accentText: ui.Hex("#123f73"), amber: ui.Hex("#ffe27a"),
		level: [4]ui.Color{ui.Hex("#9cf0c0"), ui.Hex("#ffe27a"), ui.Hex("#ffa8a8"), ui.Hex("#a9bedb")},
		ramp:  [][4]float32{{0, 212, 30, 88}, {1, 212, 30, 88}},
		art:   artBlueprint, instr: instrBlueprint, deco: decoGrid, agg: ui.RGBA(255, 255, 255, .06), ink: ui.RGB(255, 255, 255), labelText: ui.RGBA(255, 255, 255, .95),
		corner: 0, gap: 2, motion: true, rule: .45, font: "monospace", mono: "monospace", radius: 0,
	},
	{
		name: "phosphor", label: "终端", desc: "磷光屏：绿色辉光、扫描线与命令行读数", dark: true,
		bg: ui.Hex("#040904"), panel: ui.Hex("#071007"), panel2: ui.Hex("#0c180c"), line: ui.RGBA(51, 255, 102, .13),
		hov: ui.RGBA(51, 255, 102, .06), sel: ui.RGBA(51, 255, 102, .13),
		text: ui.Hex("#a6ffbb"), t2: ui.Hex("#62d67e"), t3: ui.Hex("#3a8d4e"), t4: ui.Hex("#285f35"),
		accent: ui.Hex("#33ff66"), accentText: ui.Hex("#021a07"), amber: ui.Hex("#ffb000"),
		level: [4]ui.Color{ui.Hex("#33ff66"), ui.Hex("#ffb000"), ui.Hex("#ff5f56"), ui.Hex("#3a8d4e")},
		ramp:  [][4]float32{{0, 134, 90, 56}, {.5, 146, 80, 50}, {1, 134, 90, 56}},
		art:   artPhosphor, instr: instrTerminal, deco: decoScan, agg: ui.RGBA(51, 255, 102, .05), ink: ui.RGB(51, 255, 102), labelText: ui.RGBA(190, 255, 205, .95),
		corner: 0, gap: 2, motion: true, glow: true, font: "monospace", mono: "monospace", radius: 0,
	},
	{
		name: "te", label: "工业", desc: "Teenage Engineering 式工业风：浅灰机身、橙色强调、编号与点阵", dark: false,
		bg: ui.Hex("#dededa"), panel: ui.Hex("#ebebe8"), panel2: ui.Hex("#f6f6f4"), line: ui.RGBA(0, 0, 0, .1),
		hov: ui.RGBA(0, 0, 0, .04), sel: ui.RGBA(255, 90, 31, .12),
		text: ui.Hex("#1a1a1a"), t2: ui.Hex("#4a4a4a"), t3: ui.Hex("#8a8a8a"), t4: ui.Hex("#b2b2b0"),
		accent: ui.Hex("#ff5a1f"), accentText: ui.Hex("#ffffff"), amber: ui.Hex("#ff5a1f"),
		level: [4]ui.Color{ui.Hex("#20a464"), ui.Hex("#f0a800"), ui.Hex("#ff5a1f"), ui.Hex("#9b9b9b")},
		ramp:  [][4]float32{{0, 40, 4, 80}, {.5, 40, 3, 74}, {1, 40, 4, 80}},
		art:   artTE, instr: instrTE, deco: decoDots, agg: ui.RGBA(0, 0, 0, .06), ink: ui.RGB(26, 26, 26), labelText: ui.Hex("#1a1a1a"),
		corner: 6, gap: 3, motion: true, mono: "monospace", radius: 10,
	},
	{
		name: "eink", label: "墨水屏", desc: "E-ink：纸张底色、纯灰阶与阴影线，只有废纸篓以分步刷新动起来，省电护眼", dark: false,
		bg: ui.Hex("#f2efe7"), panel: ui.Hex("#f2efe7"), panel2: ui.Hex("#e8e4da"), line: ui.RGBA(0, 0, 0, .2),
		hov: ui.RGBA(0, 0, 0, .05), sel: ui.RGBA(0, 0, 0, .1),
		text: ui.Hex("#1b1b1b"), t2: ui.Hex("#3a3a3a"), t3: ui.Hex("#666666"), t4: ui.Hex("#9a9a9a"),
		accent: ui.Hex("#1b1b1b"), accentText: ui.Hex("#f2efe7"), amber: ui.Hex("#1b1b1b"),
		level: [4]ui.Color{ui.Hex("#2b2b2b"), ui.Hex("#777777"), ui.Hex("#b3b3b3"), ui.Hex("#d9d5cc")},
		ramp:  [][4]float32{{0, 0, 0, 62}, {.5, 0, 0, 78}, {1, 0, 0, 62}},
		art:   artInk, deco: decoPaper, agg: ui.RGBA(0, 0, 0, .06), ink: ui.RGB(27, 27, 27), labelText: ui.Hex("#1b1b1b"),
		corner: 0, gap: 2, mono: "monospace", radius: 3,
	},
	{
		name: "happy", label: "欢乐", desc: "七位好朋友：焦糖熊、薰衣草兔、奶油狗、薄荷猫、蜜桃熊、湖水龟、樱花狐，配水手蓝与波点", dark: false,
		bg: ui.Hex("#fff6e8"), panel: ui.Hex("#fffbf3"), panel2: ui.Hex("#fdebd0"), line: ui.RGBA(35, 45, 90, .12),
		hov: ui.RGBA(47, 95, 168, .06), sel: ui.RGBA(47, 95, 168, .13),
		text: ui.Hex("#22305a"), t2: ui.Hex("#4a5579"), t3: ui.Hex("#7d849f"), t4: ui.Hex("#adb1c4"),
		accent: ui.Hex("#2f5fa8"), accentText: ui.Hex("#ffffff"), amber: ui.Hex("#f5b82e"),
		level: [4]ui.Color{ui.Hex("#3bb58a"), ui.Hex("#f5a524"), ui.Hex("#e8546a"), ui.Hex("#a59bc9")},
		// seven bands, one per friend's own pastel fur, ordered so neighbours never look alike:
		// tan bear, lavender bunny, butter pup, mint cat, peach-cream bear, aqua turtle, pink fox
		ramp: [][4]float32{{0, 30, 40, 64}, {.12, 30, 40, 64}, {.143, 265, 45, 80}, {.26, 265, 45, 80}, {.286, 404, 75, 80}, {.4, 404, 75, 80},
			{.429, 515, 45, 74}, {.54, 515, 45, 74}, {.571, 736, 65, 85}, {.68, 736, 65, 85}, {.714, 905, 45, 74}, {.83, 905, 45, 74},
			{.857, 1063, 75, 83}, {.97, 1063, 75, 83}, {1, 1110, 40, 64}},
		art: artToon, deco: decoPolka, agg: ui.RGBA(35, 31, 74, .06), ink: ui.RGB(35, 31, 74), labelText: ui.Hex("#23204a"),
		corner: 7, gap: 2.5, motion: true, mono: "monospace", radius: 14,
	},
	{
		name: "pinky", label: "粉粉", desc: "粉色小狐狸：樱花粉、莓果红、薰衣草紫与奶油白，软萌描边与小爱心", dark: false,
		bg: ui.Hex("#fff0f4"), panel: ui.Hex("#fff7f9"), panel2: ui.Hex("#ffdfe8"), line: ui.RGBA(122, 46, 92, .12),
		hov: ui.RGBA(236, 95, 145, .07), sel: ui.RGBA(236, 95, 145, .14),
		text: ui.Hex("#5a2546"), t2: ui.Hex("#7e4b6a"), t3: ui.Hex("#a7809a"), t4: ui.Hex("#cdb0c2"),
		accent: ui.Hex("#ec5f91"), accentText: ui.Hex("#ffffff"), amber: ui.Hex("#b58ae0"),
		level: [4]ui.Color{ui.Hex("#5cc7a0"), ui.Hex("#f5a25d"), ui.Hex("#e8457e"), ui.Hex("#b4a3d6")},
		// fur pink, inner-ear berry, the flower's lavender, muzzle cream, lilac, blush peach
		ramp: [][4]float32{{0, 342, 82, 74}, {.14, 342, 82, 74}, {.167, 330, 64, 62}, {.3, 330, 64, 62}, {.333, 268, 60, 76}, {.47, 268, 60, 76},
			{.5, 352, 92, 84}, {.63, 352, 92, 84}, {.667, 292, 46, 72}, {.8, 292, 46, 72}, {.833, 375, 88, 80}, {.97, 375, 88, 80}, {1, 342, 82, 74}},
		art: artToon, deco: decoHearts, agg: ui.RGBA(122, 46, 92, .06), ink: ui.RGB(122, 46, 92), labelText: ui.Hex("#5a2546"),
		corner: 9, gap: 2.5, motion: true, mono: "monospace", radius: 16,
	},
	{
		name: "greeny", label: "绿绿", desc: "绿色小画家猫：薄荷绿、湖水青、奶油白，颜料飞溅的画室", dark: false,
		bg: ui.Hex("#effaf4"), panel: ui.Hex("#f7fdfa"), panel2: ui.Hex("#d9f2e6"), line: ui.RGBA(24, 74, 66, .12),
		hov: ui.RGBA(38, 165, 138, .07), sel: ui.RGBA(38, 165, 138, .14),
		text: ui.Hex("#1d4440"), t2: ui.Hex("#43695f"), t3: ui.Hex("#7a988f"), t4: ui.Hex("#adc4bc"),
		accent: ui.Hex("#22a386"), accentText: ui.Hex("#ffffff"), amber: ui.Hex("#ffc244"),
		level: [4]ui.Color{ui.Hex("#2bb38c"), ui.Hex("#f2a33a"), ui.Hex("#ea5f62"), ui.Hex("#9aa8c4")},
		// mint fur, deep teal, cream muzzle, sea glass, leaf, and the yellow on the palette
		ramp: [][4]float32{{0, 158, 56, 66}, {.14, 158, 56, 66}, {.167, 174, 52, 48}, {.3, 174, 52, 48}, {.333, 44, 78, 86}, {.47, 44, 78, 86},
			{.5, 190, 50, 68}, {.63, 190, 50, 68}, {.667, 128, 44, 60}, {.8, 128, 44, 60}, {.833, 46, 96, 66}, {.97, 46, 96, 66}, {1, 158, 56, 66}},
		art: artToon, deco: decoPaint, agg: ui.RGBA(24, 74, 66, .06), ink: ui.RGB(24, 74, 66), labelText: ui.Hex("#1d4440"),
		corner: 8, gap: 2.5, motion: true, mono: "monospace", radius: 14,
	},
}

func paletteByName(n string) (int, *palette) {
	for i, p := range palettes {
		if p.name == n {
			return i, p
		}
	}
	return 0, palettes[0]
}

func (p *palette) rampAt(t float32) (h, s, l float32) {
	t = min(1, max(0, t))
	r := p.ramp
	i := 1
	for i < len(r)-1 && r[i][0] < t {
		i++
	}
	a, b := r[i-1], r[i]
	u := (t - a[0]) / max(b[0]-a[0], 1e-6)
	return a[1] + (b[1]-a[1])*u, a[2] + (b[2]-a[2])*u, a[3] + (b[3]-a[3])*u
}

// theme maps the palette onto the toolkit's controls.
func (p *palette) theme() *ui.Theme {
	t := ui.DarkTheme()
	if !p.dark {
		t = ui.LightTheme()
	}
	t.Background = p.bg
	t.Surface = p.panel2
	t.SurfaceHover = p.panel2.Mix(p.text, .06)
	t.SurfacePressed = p.panel2.Mix(p.text, .1)
	t.Border = p.line
	t.Text = p.text
	t.TextMuted = p.t3
	t.Accent = p.accent
	t.AccentHover = p.accent.Mix(p.bg, .12)
	t.AccentPressed = p.accent.Mix(p.bg, .22)
	t.AccentText = p.accentText
	t.Selection = p.accent.Alpha(.3)
	t.Focus = p.accent.Alpha(.6)
	t.Danger = p.level[2]
	t.Warning = p.level[1]
	t.Success = p.level[0]
	t.Radius = p.radius
	if p.font != "" {
		t.Font = p.font
	}
	return t
}

// hsl converts h (degrees, any range), s and l (0–100) and alpha (0–1).
func hsl(h, s, l, a float32) ui.Color {
	h = float32(math.Mod(float64(h), 360))
	if h < 0 {
		h += 360
	}
	s, l = min(1, max(0, s/100)), min(1, max(0, l/100))
	c := (1 - float32(math.Abs(float64(2*l-1)))) * s
	x := c * (1 - float32(math.Abs(math.Mod(float64(h/60), 2)-1)))
	m := l - c/2
	var r, g, b float32
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return ui.RGBA(uint8((r+m)*255+.5), uint8((g+m)*255+.5), uint8((b+m)*255+.5), a)
}

// toHSL is the inverse of hsl, for deriving sector shades from a style's level colours.
func toHSL(c ui.Color) (h, s, l float32) {
	r, g, b := float32(c.R)/255, float32(c.G)/255, float32(c.B)/255
	mx, mn := max(r, g, b), min(r, g, b)
	l = (mx + mn) / 2
	if mx == mn {
		return 0, 0, l * 100
	}
	d := mx - mn
	if l > .5 {
		s = d / (2 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	switch mx {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h * 60, s * 100, l * 100
}

// luma is perceived lightness 0..1, for choosing label ink on solid fills.
func luma(c ui.Color) float32 {
	return (.2126*float32(c.R) + .7152*float32(c.G) + .0722*float32(c.B)) / 255
}
