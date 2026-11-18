// tossy.app: the style switcher (the page wears the app's ten styles) and the two languages.

const STYLES = [
  { id: "hud", bg: "#0d1013", en: ["HUD", "Holographic cockpit: glass sectors, glowing outlines and instrument rings."], zh: ["HUD", "全息座舱：半透明扇区、发光描边与仪表环。"] },
  { id: "mech", bg: "#e4e7eb", en: ["Mech", "Light armour plate: cobalt blue, signal red, hazard yellow and chamfers."], zh: ["机甲", "浅色装甲板：钴蓝、信号红、警示黄与倒角。"] },
  { id: "classic", bg: "#131315", en: ["Classic", "A quiet dark interface with soft, solid sectors."], zh: ["经典", "安静的暗色界面，柔和的实色扇区。"] },
  { id: "blueprint", bg: "#123f73", en: ["Blueprint", "Prussian blue ground, white linework, dimensions and a title block."], zh: ["蓝图", "普鲁士蓝底、白色线稿、尺寸标注与图签。"] },
  { id: "phosphor", bg: "#040904", en: ["Terminal", "A phosphor screen: green glow, scanlines and a command-line readout."], zh: ["终端", "磷光屏：绿色辉光、扫描线与命令行读数。"] },
  { id: "te", bg: "#dededa", en: ["Industrial", "Light grey hardware, orange accents, numbers and a dot matrix."], zh: ["工业", "浅灰机身、橙色强调、编号与点阵。"] },
  { id: "eink", bg: "#f2efe7", en: ["E-ink", "Paper ground, pure greyscale and hatching. Only the bin moves."], zh: ["墨水屏", "纸张底色、纯灰阶与阴影线，只有废纸篓会动。"] },
  { id: "happy", bg: "#fff6e8", en: ["Happy", "Seven pastel friends, sailor blue and polka dots."], zh: ["欢乐", "七位粉彩色的好朋友，配水手蓝与波点。"] },
  { id: "pinky", bg: "#fff0f4", en: ["Pinky", "Cherry-blossom pink, berry red, lavender and little hearts."], zh: ["粉粉", "樱花粉、莓果红、薰衣草紫与小爱心。"] },
  { id: "greeny", bg: "#effaf4", en: ["Greeny", "Mint, lake teal and cream, in a painter's studio."], zh: ["绿绿", "薄荷绿、湖水青与奶油白，颜料飞溅的画室。"] },
];

// Simplified Chinese; the English is the page itself.
const ZH = {
  title: "Tossy — 看清磁盘被什么占满，只清理安全的部分",
  "nav.features": "功能",
  "nav.dev": "开发者视图",
  "nav.cli": "命令行",
  "nav.download": "下载",
  "hero.readout": "磁盘清理分析器 · macOS · Linux",
  "hero.h1": "看清磁盘被什么占满，<br>只清理<em>安全</em>的部分。",
  "hero.lede": "Tossy 把磁盘画成一张可以层层点进去的旭日图，给每一项一个安全判定，一键清掉可以放心清理的内容。清掉的东西，在清空废纸篓之前都能放回原处。",
  "hero.source": "在 GitHub 查看源码",
  "hero.fine": "macOS 12 及以上，Apple 芯片与 Intel · Linux x86-64 与 ARM64 · 桌面应用与命令行工具",
  "hero.alt": "Tossy：磁盘画成旭日图，旁边是文件夹列表，角落是废纸篓",
  "dock.label": "风格",
  "dock.note": "十种风格。选一个：应用的 Dock 图标会跟着变，这个页面也会。",
  "v.eyebrow": "安全判定",
  "v.h2": "每一项都有一个判定。",
  "v.p": "约 90 条内置规则把磁盘上的内容分成四个等级。没有专属规则的项目，沿用上一级文件夹的判定。",
  "v.note": "<code>dist</code>、<code>build</code>、<code>target</code> 这类常见名字，只有在真正的项目里才算数，也就是带有 <code>package.json</code>、<code>Cargo.toml</code> 或 <code>go.mod</code> 的文件夹。主目录下的个人文件夹，不论里面放了什么，都按个人数据对待。",
  "v.safe": "可清理",
  "v.safe.m": "需要时会自动重建或重新下载。",
  "v.safe.x": "应用缓存",
  "v.review": "需确认",
  "v.review.m": "多半可以清理，但应该由你确认。",
  "v.review.x": "<span>Docker 与虚拟机磁盘</span><span>iOS 模拟器</span><span>本地模型</span><span>Python 虚拟环境</span><span>下载文件夹</span>",
  "v.personal": "个人数据",
  "v.personal.m": "你自己的数据。动它之前，Tossy 一定先问你。",
  "v.protected": "系统保护",
  "v.protected.m": "系统需要它。Tossy 绝不删除。",
  "v.alt": "自动清理正在分析主目录：图表按安全等级着色",
  "v.cap": "自动清理进行中：图表切换为安全等级配色，并标出将要清理的内容。",
  "c.eyebrow": "结构",
  "c.h2": "整块磁盘，一张图看完。",
  "c.p": "每一环是一层文件夹，扇区的宽度就是它占用的空间。点击扇区进入下一层；点中心、面包屑或按退格键返回上一层。",
  "c.1": "<b>快速并行扫描。</b>每次系统调用读取一整批目录项，用紧凑的树结构容纳数百万个条目。",
  "c.2": "<b>列表与详情。</b>图表旁的面板按大小列出当前文件夹；选中任意一项，就能看到它的判定和理由。",
  "c.3": "<b>跟着你动。</b>删除或恢复时扇区平滑变形，顶部的磁盘条显示腾出了多少空间。",
  "c.alt": "进入 projects 文件夹，列表中选中了一行",
  "d.eyebrow": "开发者视图",
  "d.h2": "写代码留下的一切，都在这里。",
  "d.p": "一个开关（⌘D）把同一次扫描变成开发者视图：只看开发过程留下的东西，不管它在磁盘的哪个角落，按种类而不是按文件夹归类。",
  "d.kinds": "<span>依赖</span><span>构建产物</span><span>构建缓存</span><span>包缓存</span><span>模拟器与设备</span><span>容器与虚拟机</span><span>本地模型</span><span>应用缓存</span>",
  "d.note": "在这个视图里，自动清理只处理开发内容，而且依然只清理安全的部分。模拟器、虚拟机磁盘、模型和虚拟环境会列出来，但留给你逐个决定。",
  "d.alt": "开发者视图：扫描结果按种类归类",
  "g.h2": "删除代码仓库之前，先问问 git。",
  "g.p": "未提交的修改、未跟踪的文件、只存在于这台机器上的提交：在仓库或工作树被删掉之前，Tossy 会告诉你将失去什么。",
  "g.1": "<b>工作树一目了然。</b>仓库会列出所有共用对象库的工作树。AI 编程助手往往会留下一大堆。",
  "g.2": "<b>按项目清缓存。</b>项目文件夹会列出其中的依赖与构建缓存，一键清理。",
  "g.alt": "一个 Git 工作树：每个工作树会丢失什么，以及其中可重建的缓存",
  "k.eyebrow": "清理",
  "k.h2": "一键清理，依然谨慎。",
  "k.1h": "自动清理",
  "k.1p": "依据规则和你的习惯扫过整次扫描结果，边找边展示，然后把清理计划丢进废纸篓。它只清理能按需重新生成的内容，或者与你手动删除过的东西相符的内容。",
  "k.2h": "使用中保护",
  "k.2p": "最近几分钟内写入过的内容，以及正在运行的应用的缓存，都会跳过。清理绝不会从运行中的程序脚下抽走缓存。",
  "k.3h": "学习你的习惯",
  "k.3p": "同一类东西手动删得够多，下次自动清理就会带上它，并告诉你原因。把习惯选中的东西恢复回来，会加倍抵消这个习惯。",
  "t.eyebrow": "废纸篓",
  "t.h2": "只做标记，不搬文件。",
  "t.p": "放进 Tossy 的废纸篓，只是在日志里记下一个标记。文件留在原处，系统废纸篓完全不受影响。恢复就是去掉标记；清空则原地彻底删除被标记的路径。",
  "t.1": "<b>东西越多，桶越大</b>，从桌面纸篓一路长到工地垃圾箱，还能看到里面的东西来自哪里。",
  "t.2": "<b>清空时有车来收。</b>每一项只有真正删除后才会离开，所以这段动画本身就是进度条。",
  "t.alt": "废纸篓视图：里面的内容来自哪里，以及装着它们的桶",
  "l.eyebrow": "命令行",
  "l.h2": "同一个引擎，也在终端里。",
  "l.p": "桌面应用和 <code>tossy</code> 共用同一个 Go 引擎：同样的扫描器、同样的规则、同一个废纸篓、同一套习惯。在一边删除的东西，可以在另一边恢复。",
  "l.c1": "# 列出文件夹下最大的条目",
  "l.c2": "# 哪些可以放心清理（只读）",
  "l.c3": "# 按种类列出开发内容（只读）",
  "l.c4": "# 先演练；加 -yes 才把安全的内容移入废纸篓",
  "l.c5": "# 改主意了",
  "x.eyebrow": "下载",
  "x.h2": "获取 Tossy。",
  "x.mac.req": "macOS 12 及以上 · Apple 芯片与 Intel",
  "x.mac.p": "打开 <code>tossy-&lt;version&gt;.dmg</code>，把 Tossy 拖进“应用程序”。应用已用 Developer ID 签名并经 Apple 公证，可以直接打开。",
  "x.linux.req": "x86-64 与 ARM64 · 需要 GTK 3",
  "x.linux.any": "任意发行版，按用户安装",
  "x.fine": "每个版本都附带命令行工具和 <code>SHA256SUMS</code>。界面支持 English、简体中文、繁體中文和日本語。",
  "f.releases": "版本发布",
  "f.issues": "问题反馈",
  "f.mygo": "基于 MyGo 构建",
  dl: ["下载", "下载 macOS 版", "下载 Linux 版"],
  style: "风格",
  langButton: ["EN", "Switch to English"],
};
const EN = { dl: ["Download", "Download for macOS", "Download for Linux"], style: "Style", langButton: ["中文", "切换到简体中文"] };

const root = document.documentElement;
const $ = (id) => document.getElementById(id);
const store = {
  get(k) { try { return localStorage.getItem(k); } catch { return null; } },
  set(k, v) { try { localStorage.setItem(k, v); } catch {} },
};
const zh = () => root.lang === "zh-Hans";

// ---------- language ----------

const english = new Map(); // element → its English markup, as the page was served
const englishTitle = document.title;

function applyLang() {
  const dict = zh() ? ZH : null;
  for (const el of document.querySelectorAll("[data-i18n]")) {
    if (!english.has(el)) english.set(el, el.innerHTML);
    el.innerHTML = (dict && dict[el.dataset.i18n]) || english.get(el);
  }
  for (const [data, attr] of [["i18nAlt", "alt"], ["i18nAria", "aria-label"]]) {
    const sel = data === "i18nAlt" ? "[data-i18n-alt]" : "[data-i18n-aria]";
    for (const el of document.querySelectorAll(sel)) {
      const key = el.dataset[data] + "@" + attr;
      if (!english.has(key)) english.set(key, el.getAttribute(attr));
      el.setAttribute(attr, (dict && dict[el.dataset[data]]) || english.get(key));
    }
  }
  document.title = dict ? ZH.title : englishTitle;

  const t = zh() ? ZH : EN;
  const os = /Mac|iPhone|iPad/.test(navigator.userAgent) ? 1 : /Linux|X11/.test(navigator.userAgent) && !/Android/.test(navigator.userAgent) ? 2 : 0;
  $("dl-hero").textContent = t.dl[os];
  $("dl-foot").textContent = t.dl[os];
  $("lang").textContent = t.langButton[0];
  $("lang").setAttribute("aria-label", t.langButton[1]);
  describeStyle();
}

$("lang").addEventListener("click", () => {
  root.lang = zh() ? "en" : "zh-Hans";
  store.set("tossy-lang", root.lang);
  applyLang();
});

// ---------- styles ----------

const dock = $("dock");
const shot = $("hero-shot");
const current = () => Math.max(0, STYLES.findIndex((s) => s.id === root.dataset.style));

function describeStyle() {
  const i = current();
  const [name, desc] = STYLES[i][zh() ? "zh" : "en"];
  const t = zh() ? ZH : EN;
  $("style-readout").textContent = `${t.style} ${String(i + 1).padStart(2, "0")}/${STYLES.length} · ${name}`;
  $("style-desc").textContent = desc;
  $("window-style").textContent = name;
  dock.querySelectorAll("button").forEach((b, k) => {
    const label = STYLES[k][zh() ? "zh" : "en"][0];
    b.setAttribute("aria-label", label);
    b.firstElementChild.textContent = label;
    b.setAttribute("aria-checked", k === i ? "true" : "false");
    b.tabIndex = k === i ? 0 : -1;
  });
}

const shotOf = (s) => `shots/style-${s.id}.webp`;
let wanted = 0;

function setStyle(i, focus) {
  const s = STYLES[i];
  root.dataset.style = s.id;
  store.set("tossy-style", s.id);
  document.querySelector('meta[name="theme-color"]').content = s.bg;
  describeStyle();
  if (focus) dock.children[i].focus();

  // the window shows the new style once its picture has arrived
  const mine = ++wanted;
  const img = new Image();
  img.onload = () => {
    if (mine !== wanted) return;
    shot.src = img.src;
    if (!matchMedia("(prefers-reduced-motion: reduce)").matches) {
      shot.animate([{ opacity: 0.25 }, { opacity: 1 }], { duration: 260, easing: "ease-out" });
    }
  };
  img.src = shotOf(s);
}

STYLES.forEach((s, i) => {
  const b = document.createElement("button");
  b.type = "button";
  b.setAttribute("role", "radio");
  b.style.setProperty("--i", i);
  b.append(Object.assign(document.createElement("span"), { className: "tip" }));
  b.addEventListener("click", () => setStyle(i));
  b.addEventListener("pointerenter", () => { new Image().src = shotOf(s); }, { once: true });
  dock.append(b);
});

dock.addEventListener("keydown", (e) => {
  const step = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[e.key];
  if (!step) return;
  e.preventDefault();
  setStyle((current() + step + STYLES.length) % STYLES.length, true);
});

if (current() !== 0) shot.src = shotOf(STYLES[current()]);
document.querySelector('meta[name="theme-color"]').content = STYLES[current()].bg;
applyLang();
