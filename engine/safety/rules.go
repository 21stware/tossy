// Package safety decides, for every node of a scanned tree, whether removing it is safe.
// Rules match a node by (lowercased) name and optionally by its normalised absolute path;
// a node without its own rule inherits its parent's verdict.
package safety

import (
	"regexp"
	"strings"
)

// Level of a verdict, from most to least removable.
type Level uint8

const (
	Safe      Level = iota // rebuildable or re-downloadable
	Review                 // probably fine, the user should confirm
	Risky                  // personal data
	Protected              // OS needs it; never removed by FanFree
)

func (l Level) String() string { return [...]string{"safe", "review", "risky", "protected"}[l] }

// Rule describes one class of content.
type Rule struct {
	ID     string
	Level  Level
	Reason string
	Tag    string
	// Path, if set, must match the normalised absolute path (lowercase, '/' separators, "c:/…" on Windows).
	Path *regexp.Regexp
	// Pick: the node is removed as a whole. Split: each child is a separate candidate (e.g. ~/Library/Caches/*).
	Pick, Split bool
	// Exts restricts a file rule to extensions; MinAgeDays/MaxAgeDays bound the file's age.
	names []string
}

type ruleSet struct {
	byName map[string][]*Rule
	all    []*Rule
}

var builtin = func() *ruleSet {
	rs := &ruleSet{byName: map[string][]*Rule{}}
	add := func(id, names string, lv Level, reason, tag, path string, mode string) {
		r := &Rule{ID: id, Level: lv, Reason: reason, Tag: tag, Pick: mode == "pick", Split: mode == "split"}
		if path != "" {
			r.Path = regexp.MustCompile(path)
		}
		for _, n := range strings.Split(names, "|") {
			r.names = append(r.names, n)
			rs.byName[n] = append(rs.byName[n], r)
		}
		rs.all = append(rs.all, r)
	}
	const home = `^(/users|/home|c:/users)/[^/]+`

	// ---------- safe: regenerated or re-downloaded on demand
	add("user-caches", "caches", Safe, "应用缓存，删除后应用会按需重建", "缓存", `^/users/[^/]+/library/caches$`, "split")
	add("xdg-cache", ".cache", Safe, "用户级缓存（pip / yarn / 浏览器等），可安全清理", "缓存", `^/home/[^/]+/\.cache$|^/users/[^/]+/\.cache$`, "split")
	add("gradle-caches", "caches", Safe, "Gradle 依赖与构建缓存，按需重新下载", "包缓存", `/\.gradle/caches$`, "pick")
	add("xcode-deriveddata", "deriveddata", Safe, "Xcode 编译中间产物，重新编译时自动生成", "构建产物", "", "split")
	add("ios-devicesupport", "ios devicesupport|watchos devicesupport|tvos devicesupport", Safe, "旧版设备调试符号，连接设备时会重新下载", "开发缓存", "", "split")
	add("simulator-caches", "caches", Safe, "模拟器缓存，按需重建", "开发缓存", `/coresimulator/caches$`, "pick")
	add("apt-archives", "archives", Safe, "APT 已下载的安装包，软件已安装完成", "包缓存", `^/var/cache/apt/archives$`, "pick")
	add("node-modules", "node_modules", Safe, "npm 依赖目录，可通过 npm install 重新安装", "依赖", "", "pick")
	add("pycache", "__pycache__", Safe, "Python 字节码缓存，运行时自动生成", "构建产物", "", "pick")
	add("cargo-target", "target", Safe, "Cargo 构建产物，cargo build 可重新生成", "构建产物", `/[^/]+/target$`, "pick")
	add("npm-cache", ".npm|npm-cache|_cacache", Safe, "npm 下载缓存，等价于 npm cache clean", "包缓存", "", "pick")
	add("yarn-cache", ".yarn-cache", Safe, "Yarn 下载缓存", "包缓存", "", "pick")
	add("pnpm-store", "store", Safe, "pnpm 内容寻址存储，按需重新下载", "包缓存", `/(\.pnpm-store|pnpm)/store$|/library/pnpm/store$`, "pick")
	add("cargo-registry", "registry", Safe, "Cargo crate 缓存，按需重新下载", "包缓存", `/\.cargo/registry$`, "pick")
	add("go-modcache", "mod", Safe, "Go 模块缓存，等价于 go clean -modcache", "包缓存", home+`/go/pkg/mod$`, "pick")
	add("go-buildcache", "go-build", Safe, "Go 编译缓存，等价于 go clean -cache", "构建产物", "", "pick")
	add("maven-repo", "repository", Safe, "Maven 本地仓库，按需重新下载", "包缓存", `/\.m2/repository$`, "pick")
	add("cocoapods", "pods", Safe, "CocoaPods 依赖，pod install 可重新生成", "依赖", `/[^/]+/pods$`, "pick")
	add("homebrew-cache", "homebrew", Safe, "Homebrew 下载缓存，等价于 brew cleanup", "包缓存", home+`/library/caches/homebrew$`, "pick")
	add("trash-mac", ".trash", Safe, "废纸篓中已删除的文件", "废纸篓", home+`/\.trash$`, "pick")
	add("trash-win", "$recycle.bin", Safe, "回收站中已删除的文件", "回收站", "", "pick")
	add("trash-xdg", "trash", Safe, "回收站中已删除的文件", "回收站", `/\.local/share/trash$`, "pick")
	add("user-logs", "logs", Safe, "应用与诊断日志，仅用于排错", "日志", `^/users/[^/]+/library/logs$`, "split")
	add("win-logs", "logs", Safe, "Windows 组件日志（CBS 等）", "日志", `^c:/windows/logs$`, "pick")
	add("var-log", "log", Safe, "系统日志，旧日志可安全清理（journalctl --vacuum）", "日志", `^(/private)?/var/log$`, "split")
	add("win-temp", "temp", Safe, "临时文件，生成它们的进程已不再需要", "临时文件", `^c:/windows/temp$|/appdata/local/temp$`, "pick")
	add("tmp", "tmp", Safe, "系统临时目录", "临时文件", `^(/private)?/tmp$|^/var/tmp$`, "pick")
	add("prefetch", "prefetch", Safe, "预读缓存，系统会自动重建", "缓存", `^c:/windows/prefetch$`, "pick")
	add("win-update-dl", "download", Safe, "Windows 更新下载缓存，更新已安装", "更新缓存", `softwaredistribution/download$`, "pick")
	add("inetcache", "inetcache", Safe, "Edge / IE 网页缓存", "缓存", "", "pick")
	add("chromium-cache", "cache|code cache|gpucache", Safe, "浏览器 / Electron 缓存", "缓存", `/(default|profile [0-9]+|[^/]*electron[^/]*|application support/[^/]+)/(cache|code cache|gpucache)$`, "pick")
	add("crashdumps", "crashdumps|diagnosticreports", Safe, "程序崩溃转储与诊断报告，仅用于排错", "诊断", "", "pick")

	// ---------- review
	add("coresimulator", "coresimulator", Review, "iOS 模拟器设备数据，删除后需重新创建模拟器", "开发", "", "pick")
	add("docker-desktop", "com.docker.docker", Review, "Docker 虚拟磁盘（镜像与卷），建议使用 docker system prune", "虚拟机", "", "pick")
	add("docker-linux", "docker", Review, "Docker 镜像与容器卷，建议使用 docker system prune", "虚拟机", `^/var/lib/docker$|/appdata/local/docker$`, "pick")
	add("colima", ".colima|.lima|.orbstack", Review, "容器虚拟机磁盘，删除会丢失其中的镜像与数据", "虚拟机", "", "pick")
	add("android-avd", "avd", Review, "Android 模拟器镜像，删除后需重新创建", "开发", `/\.android/avd$`, "pick")
	add("xcode-archives", "archives", Review, "Xcode 归档，含 dSYM，可能用于线上崩溃符号化", "开发", `/xcode/archives$`, "pick")
	add("app-support", "application support|roaming|.config", Review, "应用数据，可能含配置、登录态与本地数据库", "应用数据", "", "")
	add("apps", "applications|program files|program files (x86)|opt|snap", Review, "已安装的应用程序，请通过卸载流程移除", "应用", "", "")
	add("win-installer", "installer", Review, "MSI 安装缓存，删除会导致程序无法修复或卸载", "系统缓存", `^c:/windows/installer$`, "")
	add("package-cache", "package cache", Review, "安装程序包缓存，部分软件修复时需要", "安装缓存", "", "pick")
	add("hiberfil", "hiberfil.sys", Review, "休眠文件：可执行 powercfg /h off 关闭休眠来释放", "系统", "", "")
	add("downloads", "downloads", Review, "下载目录，需逐项确认", "下载", "", "")
	add("media", "movies|videos", Review, "视频文件，请确认已备份", "媒体", "", "")
	add("toolchains", ".rustup|steam|.android|sdk", Review, "工具链 / 游戏数据，删除后需重新安装", "应用数据", "", "")
	add("venv", ".venv|venv", Review, "Python 虚拟环境，可用 requirements 重新创建", "依赖", "", "pick")

	// ---------- risky: personal data
	add("personal", "documents|desktop|pictures|music|mail|messages|.ssh|.gnupg|.git|photos library.photoslibrary|mobile documents|icloud drive", Risky, "个人数据，删除后无法重新生成", "个人数据", "", "")

	// ---------- protected
	add("os-top", "system|library|private|usr|bin|sbin|etc|boot|lib|lib64|windows|var|cores", Protected, "操作系统组件，删除会导致系统无法运行", "系统", `^(c:)?/[^/]+$`, "")
	add("win-core", "winsxs|system32|syswow64", Protected, "Windows 组件存储，删除会导致系统无法运行", "系统", "", "")
	add("swap", "pagefile.sys|swapfile.sys|swapfile|sleepimage|swapfile0|swapfile1", Protected, "虚拟内存 / 交换文件，由系统管理", "系统", "", "")
	return rs
}()

var installerExt = map[string]bool{"dmg": true, "pkg": true, "xip": true, "iso": true, "exe": true, "msi": true, "deb": true, "rpm": true, "appimage": true, "run": true}

var (
	ruleInstallerOld = &Rule{ID: "installer-old", Level: Safe, Reason: "安装包，超过 30 天未使用，对应软件已安装", Tag: "安装包", Pick: true}
	ruleInstallerNew = &Rule{ID: "installer-new", Level: Review, Reason: "近期下载的安装包，可能尚未安装", Tag: "安装包", Pick: true}
	ruleDefault      = &Rule{ID: "default", Level: Review, Reason: "未识别的内容，删除前请确认用途", Tag: "未分类"}
	ruleRoot         = &Rule{ID: "root", Level: Protected, Reason: "磁盘根目录", Tag: "系统"}
	ruleCloud        = &Rule{ID: "cloud", Level: Protected, Reason: "云端占位文件，内容不在本机，删除可能同步删除云端数据", Tag: "云端"}
)

// RuleCount is how many built-in rules the evaluator knows.
func RuleCount() int { return len(builtin.all) }
