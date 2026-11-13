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
	// Markers, if set, must name a sibling of the node: the project file that proves what the
	// folder is (a "target" is only Rust's beside a Cargo.toml). "*.ext" matches by extension.
	Markers []string
	names   []string
}

type ruleSet struct {
	byName   map[string][]*Rule
	suffixes []suffixRule // names matched by their end ("*.egg-info")
	prefixes []suffixRule // and by their start ("cmake-build-*")
	all      []*Rule
}

type suffixRule struct {
	s string
	r *Rule
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
			switch {
			case strings.HasPrefix(n, "*"):
				rs.suffixes = append(rs.suffixes, suffixRule{n[1:], r})
			case strings.HasSuffix(n, "*"):
				rs.prefixes = append(rs.prefixes, suffixRule{n[:len(n)-1], r})
			default:
				rs.byName[n] = append(rs.byName[n], r)
			}
		}
		rs.all = append(rs.all, r)
	}
	// addM adds a pick rule that needs a project file beside the folder.
	addM := func(id, names string, lv Level, reason, tag, markers string) {
		add(id, names, lv, reason, tag, "", "pick")
		rs.all[len(rs.all)-1].Markers = strings.Split(strings.ToLower(markers), "|")
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
	add("npm-cache", ".npm|npm-cache|_cacache", Safe, "npm 下载缓存，等价于 npm cache clean", "包缓存", "", "pick")
	add("yarn-cache", ".yarn-cache", Safe, "Yarn 下载缓存", "包缓存", "", "pick")
	add("pnpm-store", "store", Safe, "pnpm 内容寻址存储，按需重新下载", "包缓存", `/(\.pnpm-store|pnpm)/store$|/library/pnpm/store$`, "pick")
	add("cargo-registry", "registry", Safe, "Cargo crate 缓存，按需重新下载", "包缓存", `/\.cargo/registry$`, "pick")
	add("go-modcache", "mod", Safe, "Go 模块缓存，等价于 go clean -modcache", "包缓存", home+`/go/pkg/mod$`, "pick")
	add("go-buildcache", "go-build", Safe, "Go 编译缓存，等价于 go clean -cache", "构建产物", "", "pick")
	add("maven-repo", "repository", Safe, "Maven 本地仓库，按需重新下载", "包缓存", `/\.m2/repository$`, "pick")
	addM("cocoapods", "pods", Safe, "CocoaPods 依赖，pod install 可重新生成", "依赖", "Podfile|Podfile.lock")
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

	// ---------- safe: global toolchain caches
	add("gradle-wrapper", "dists", Safe, "Gradle 发行版缓存，按需重新下载", "包缓存", `/\.gradle/wrapper/dists$`, "pick")
	add("cargo-git", "git", Safe, "Cargo git 依赖缓存，按需重新下载", "包缓存", `/\.cargo/git$`, "pick")
	add("nuget-cache", "packages", Safe, "NuGet 包缓存，按需重新下载", "包缓存", `/\.nuget/packages$`, "pick")
	add("bun-cache", "cache", Safe, "Bun 安装缓存，按需重新下载", "包缓存", `/\.bun/install/cache$`, "pick")
	add("pub-cache", ".pub-cache", Safe, "Dart / Flutter 包缓存，flutter pub get 会重新下载", "包缓存", "", "pick")
	add("conda-pkgs", "pkgs", Safe, "Conda 包下载缓存，等价于 conda clean --all", "包缓存", `/(\.conda|anaconda3|miniconda3|miniforge3|mambaforge)/pkgs$`, "pick")
	add("ivy-cache", "cache", Safe, "Ivy / sbt 依赖缓存，按需重新下载", "包缓存", `/\.ivy2/cache$`, "pick")
	add("nvm-cache", ".cache", Safe, "nvm 下载缓存", "包缓存", `/\.nvm/\.cache$`, "pick")
	add("android-build-cache", "build-cache|cache", Safe, "Android 构建缓存，重新构建时自动生成", "构建缓存", `/\.android/(build-cache|cache)$`, "pick")
	add("xcode-previews", "previews", Safe, "SwiftUI 预览缓存，打开预览时自动生成", "开发缓存", `/xcode/userdata/previews$`, "pick")
	add("compiler-cache", ".ccache|ccache|sccache", Safe, "C/C++/Rust 编译缓存（ccache / sccache），按需重建", "构建缓存", "", "pick")

	// ---------- safe: build output inside a project, recognised by the project file beside it
	addM("cargo-target", "target", Safe, "Rust 编译产物，cargo build 可重新生成（等价于 cargo clean）", "构建产物", "Cargo.toml")
	addM("maven-target", "target", Safe, "Maven / sbt 构建输出，重新构建时自动生成", "构建产物", "pom.xml|build.sbt")
	addM("gradle-build", "build|.gradle|.cxx|.externalnativebuild|.kotlin", Safe, "Gradle / Android 构建产物，重新构建时自动生成", "构建产物", "build.gradle|build.gradle.kts|settings.gradle|settings.gradle.kts")
	addM("cmake-build", "build|out|cmake-build-*", Safe, "CMake 构建目录，重新配置后可重新生成", "构建产物", "CMakeLists.txt")
	addM("flutter-build", "build|.dart_tool", Safe, "Flutter / Dart 构建产物与工具缓存，重新构建时自动生成", "构建产物", "pubspec.yaml")
	addM("swiftpm-build", ".build", Safe, "Swift Package 构建产物，swift build 可重新生成", "构建产物", "Package.swift")
	addM("xcode-build", "build", Safe, "Xcode 本地构建输出，重新编译时自动生成", "构建产物", "*.xcodeproj|*.xcworkspace")
	addM("dotnet-build", "bin|obj", Safe, ".NET 编译输出，dotnet build 可重新生成", "构建产物", "*.csproj|*.fsproj|*.vbproj")
	addM("web-framework", ".next|.nuxt|.output|.svelte-kit|.angular|.docusaurus|.astro|.expo|.vinxi|.react-router", Safe, "前端框架构建缓存（Next.js / Nuxt / SvelteKit 等），下次 dev / build 自动生成", "构建产物", "package.json")
	addM("web-tool-cache", ".turbo|.parcel-cache|.cache|.vite|.swc|.rpt2_cache|.webpack|.eslintcache|.stylelintcache", Safe, "前端工具缓存（Turborepo / Parcel / Vite / SWC 等），自动重建", "构建缓存", "package.json")
	addM("elixir-build", "_build|deps", Safe, "Elixir 编译产物与依赖，mix deps.get / compile 可重建", "构建产物", "mix.exs")
	addM("composer-vendor", "vendor", Safe, "PHP Composer 依赖，composer install 可重新安装", "依赖", "composer.json")
	addM("unity-cache", "library|temp|obj", Safe, "Unity 导入缓存与临时文件，打开项目时自动重建", "构建缓存", "ProjectSettings")
	addM("unreal-cache", "intermediate|deriveddatacache|binaries", Safe, "Unreal 中间文件与派生数据缓存，重新生成项目时重建", "构建缓存", "*.uproject")
	addM("test-coverage", "coverage|.nyc_output|htmlcov", Safe, "测试覆盖率报告，重新运行测试即可生成", "测试缓存", "package.json|pyproject.toml|setup.py|setup.cfg|tox.ini|go.mod|Cargo.toml|pom.xml")
	add("py-tool-cache", ".pytest_cache|.mypy_cache|.ruff_cache|.hypothesis|.ipynb_checkpoints|.pytype|.pyre", Safe, "Python 工具缓存（pytest / mypy / ruff 等），运行时自动重建", "构建缓存", "", "pick")
	add("py-test-envs", ".tox|.nox", Safe, "tox / nox 测试环境，下次运行时重建", "依赖", "", "pick")
	add("py-egg-info", "*.egg-info", Safe, "Python 打包元数据，安装时自动生成", "构建产物", "", "pick")
	add("haskell-build", ".stack-work|dist-newstyle", Safe, "Haskell 构建产物，stack / cabal 构建时重新生成", "构建产物", "", "pick")
	add("zig-cache", "zig-cache|.zig-cache|zig-out", Safe, "Zig 编译缓存与输出，zig build 可重新生成", "构建产物", "", "pick")
	add("elm-stuff", "elm-stuff", Safe, "Elm 编译缓存，自动重建", "构建产物", "", "pick")

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
	addM("web-dist", "dist|build|out|storybook-static", Review, "前端构建输出，通常可由 npm run build 重新生成；部分库会把它提交到仓库", "构建产物", "package.json")
	addM("py-dist", "build|dist", Review, "Python 打包输出，python -m build 可重新生成", "构建产物", "setup.py|pyproject.toml|setup.cfg")
	addM("go-vendor", "vendor", Review, "Go vendor 依赖，go mod vendor 可重新生成；部分项目会提交到仓库", "依赖", "go.mod")
	add("terraform", ".terraform", Review, "Terraform 插件与模块缓存，terraform init 可重新下载；也记录了当前 workspace", "依赖", "", "pick")

	// ---------- review: AI models and agent data — re-downloadable, but big and slow to get back
	add("hf-cache", "huggingface", Review, "Hugging Face 模型与数据集缓存，删除后需重新下载（通常很大）", "模型缓存", `/\.cache/huggingface$`, "pick")
	add("torch-cache", "torch", Review, "PyTorch 预训练权重缓存，删除后按需重新下载", "模型缓存", `/\.cache/torch$`, "pick")
	add("ollama-models", "models", Review, "Ollama 本地大模型，删除后需重新 ollama pull", "模型缓存", `/\.ollama/models$`, "pick")
	add("lmstudio-models", "models", Review, "LM Studio 本地模型，删除后需重新下载", "模型缓存", `/(\.lmstudio|\.cache/lm-studio)/models$`, "pick")
	add("ai-sessions", "projects|sessions", Review, "AI 编程助手的会话记录，删除后无法恢复或续接历史对话", "AI 会话", `/\.(claude/projects|codex/sessions)$`, "pick")

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
	// A checkout is never cleaned by rule: it may hold work that exists nowhere else.
	ruleRepo     = &Rule{ID: "git-repo", Level: Review, Reason: "Git 仓库：可能含未提交的改动与未推送的分支", Tag: "代码仓库"}
	ruleWorktree = &Rule{ID: "git-worktree", Level: Review, Reason: "Git worktree：可能含未提交或未推送的改动，建议用 git worktree remove 移除", Tag: "Git worktree"}
)

// RuleCount is how many built-in rules the evaluator knows.
func RuleCount() int { return len(builtin.all) }

// Rules lists every rule a verdict can carry: the built-in ones and the special cases. A
// front end translates their reasons and tags from it.
func Rules() []*Rule {
	return append(append([]*Rule(nil), builtin.all...), ruleInstallerOld, ruleInstallerNew, ruleDefault, ruleRoot, ruleCloud, ruleRepo, ruleWorktree)
}
