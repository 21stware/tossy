package safety

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/21stware/tossy/engine/gitrepo"
	"github.com/21stware/tossy/engine/scan"
)

func mk(t *testing.T, root, rel string, n int, age time.Duration) {
	p := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
	if age > 0 {
		ts := time.Now().Add(-age)
		os.Chtimes(p, ts, ts)
	}
}

func TestEvaluator(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "proj/node_modules/a/index.js", 50000, 0)
	mk(t, root, "proj/src/main.go", 9000, 0)
	mk(t, root, "proj/.git/objects/pack.bin", 80000, 0)
	mk(t, root, "proj/.git/node_modules/x.js", 9000, 0) // inside risky: never picked
	mk(t, root, "Downloads/old.dmg", 70000, 90*24*time.Hour)
	mk(t, root, "Downloads/new.dmg", 70000, 0)
	mk(t, root, "Downloads/notes.txt", 7000, 0)
	tr, err := scan.Scan(context.Background(), root, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := New(tr)
	f := func(rel string) int32 { return tr.Find(filepath.Join(root, rel)) }

	if v := e.Verdict(f("proj/node_modules/a/index.js")); v.Level() != Safe || !v.Inherited || v.Rule.ID != "node-modules" {
		t.Fatalf("node_modules child: %+v", v.Rule)
	}
	if v := e.Verdict(f("proj/.git/objects")); v.Level() != Risky {
		t.Fatalf(".git: %v", v.Level())
	}
	if v := e.Verdict(f("proj/src/main.go")); v.Rule.ID != "git-repo" || !v.Inherited {
		t.Fatalf("inside a repository: %s", v.Rule.ID)
	}
	if v := e.Verdict(f("Downloads/notes.txt")); v.Rule.ID != "downloads" {
		t.Fatalf("downloads: %s", v.Rule.ID)
	}
	if e.Own(f("Downloads/old.dmg")).ID != "installer-old" || e.Own(f("Downloads/new.dmg")).ID != "installer-new" {
		t.Fatal("installer age rules")
	}
	got := map[string]bool{}
	for _, c := range e.Candidates(0, Safe) {
		got[filepath.ToSlash(tr.Path(c.Node)[len(root)+1:])] = true
	}
	want := map[string]bool{"proj/node_modules": true, "Downloads/old.dmg": true}
	if len(got) != len(want) {
		t.Fatalf("candidates %v", got)
	}
	for k := range want {
		if !got[k] {
			t.Fatalf("missing %s in %v", k, got)
		}
	}
	if n := len(e.Candidates(0, Review)); n != 3 {
		t.Fatalf("review candidates %d", n)
	}
	s := e.LevelSums(0)
	if s[Risky] == 0 || s[Safe] == 0 || s[Safe]+s[Review]+s[Risky]+s[Protected] != tr.Nodes[0].Size {
		t.Fatalf("sums %v total %d", s, tr.Nodes[0].Size)
	}
}

func TestPathRules(t *testing.T) {
	cases := map[string]string{
		"/Users/me/Library/Caches":            "user-caches",
		"/home/me/.cache":                     "xdg-cache",
		"C:\\Users\\me\\AppData\\Local\\Temp": "win-temp",
		"/Users/me/go/pkg/mod":                "go-modcache",
		"/private/var/log":                    "var-log",
		"/System":                             "os-top",
		// Windows, on any drive
		`C:\Users\me\.cache`:                                              "xdg-cache",
		`D:\Users\me\go\pkg\mod`:                                          "go-modcache",
		`D:\Windows\Temp`:                                                 "win-temp",
		`D:\Windows`:                                                      "os-top",
		`C:\ProgramData`:                                                  "os-top",
		`C:\Users\me\AppData\Local\pip\cache`:                             "win-tool-cache",
		`C:\Users\me\AppData\Local\D3DSCache`:                             "gpu-shader-cache",
		`C:\Users\me\AppData\Local\NVIDIA\DXCache`:                        "gpu-shader-cache",
		`C:\Users\me\AppData\Roaming\Code\Cache`:                          "chromium-cache",
		`C:\Users\me\AppData\Local\wsl\{89797847}\ext4.vhdx`:              "wsl-disk",
		`C:\ProgramData\Microsoft\Windows\WER\ReportArchive`:              "win-error-reports",
		`C:\Users\me\AppData\Local\Google\Chrome\User Data\Default\Cache`: "chromium-cache",
		`C:\Users\me\AppData\Roaming\npm\node_modules`:                    "npm-global",
		"/home/me/.nvm/versions/node/v22.0.0/lib/node_modules":            "npm-global",
		"/home/me/proj/node_modules":                                      "node-modules",
		// Linux
		"/root/.cache":                          "xdg-cache",
		"/home/me/.config/Code/Cache":           "chromium-cache",
		"/home/me/.var/app/org.gimp.GIMP/cache": "flatpak-cache",
		"/home/me/.local/share":                 "xdg-data",
		"/var/cache/dnf":                        "linux-pkg-cache",
		"/var/cache/pacman/pkg":                 "linux-pkg-cache",
		"/lost+found":                           "os-top",
	}
	for p, id := range cases {
		n := Norm(p)
		base := n[lastSlash(n)+1:]
		var hit string
		for _, r := range builtin.byName[base] {
			if r.Path == nil || r.Path.MatchString(n) {
				hit = r.ID
				break
			}
		}
		if hit != id {
			t.Errorf("%s → %q, want %q", p, hit, id)
		}
	}
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

// Build output is only recognised beside the project file that makes it one.
func TestDevCaches(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"rs/Cargo.toml", "rs/target/debug/app", "java/pom.xml", "java/target/classes/A.class", "misc/target/keep.txt",
		"and/build.gradle.kts", "and/build/out.dex", "and/.gradle/x", "cpp/CMakeLists.txt", "cpp/cmake-build-debug/a.o",
		"web/package.json", "web/.next/cache/x", "web/.turbo/x", "web/dist/app.js",
		"cs/App.csproj", "cs/obj/x.dll", "cs/bin/x.dll", "py/pyproject.toml", "py/.pytest_cache/v", "py/pkg.egg-info/PKG-INFO",
		"swift/Package.swift", "swift/.build/x", "fl/pubspec.yaml", "fl/.dart_tool/x",
		"ios/App.xcodeproj/project.pbxproj", "ios/build/x", "ios/Podfile", "ios/Pods/x",
		"notes/build/plan.md", "home/.cache/huggingface/hub/model.bin",
	} {
		mk(t, root, rel, 5000, 0)
	}
	tr, err := scan.Scan(context.Background(), root, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := New(tr)
	for rel, id := range map[string]string{
		"rs/target": "cargo-target", "java/target": "maven-target", "misc/target": "",
		"and/build": "gradle-build", "and/.gradle": "gradle-build", "cpp/cmake-build-debug": "cmake-build",
		"web/.next": "web-framework", "web/.turbo": "web-tool-cache", "web/dist": "web-dist",
		"cs/obj": "dotnet-build", "cs/bin": "dotnet-build", "py/.pytest_cache": "py-tool-cache", "py/pkg.egg-info": "py-egg-info",
		"swift/.build": "swiftpm-build", "fl/.dart_tool": "flutter-build", "ios/build": "xcode-build", "ios/Pods": "cocoapods",
		"notes/build": "", "home/.cache/huggingface": "hf-cache",
	} {
		got := ""
		if r := e.Own(tr.Find(filepath.Join(root, rel))); r != nil {
			got = r.ID
		}
		if got != id {
			t.Errorf("%s → %q, want %q", rel, got, id)
		}
	}
}

func TestCheckouts(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "repo/.git/HEAD", 30, 0)
	mk(t, root, "repo/node_modules/dep/.git/HEAD", 30, 0) // a dependency's clone: inside a cache
	mk(t, root, "repo/src/a.go", 3000, 0)
	mk(t, root, "wt/feature/.git", 30, 0)
	os.WriteFile(filepath.Join(root, "wt/feature/.git"), []byte("gitdir: "+filepath.Join(root, "repo/.git/worktrees/feature")+"\n"), 0o644)
	mk(t, root, "wt/feature/node_modules/x.js", 9000, 0)
	mk(t, root, "repo/lib/sub/.git", 30, 0)
	os.WriteFile(filepath.Join(root, "repo/lib/sub/.git"), []byte("gitdir: ../../.git/modules/sub\n"), 0o644)
	mk(t, root, "Documents/thesis/.git/HEAD", 30, 0)
	tr, err := scan.Scan(context.Background(), root, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := New(tr)
	f := func(rel string) int32 { return tr.Find(filepath.Join(root, rel)) }
	if r := e.Own(f("wt/feature")); r == nil || r.ID != "git-worktree" {
		t.Fatalf("worktree: %v", r)
	}
	if r := e.Own(f("repo/lib/sub")); r != nil {
		t.Fatalf("submodule has its own rule %s", r.ID)
	}
	if v := e.Verdict(f("Documents/thesis")); v.Level() != Risky {
		t.Fatalf("a repository in Documents stays personal: %v", v.Rule.ID)
	}
	var got []string
	for _, n := range e.Checkouts(0, 10) {
		got = append(got, filepath.ToSlash(tr.Path(n)[len(root)+1:]))
	}
	if len(got) != 3 {
		t.Fatalf("checkouts %v", got)
	}
	// caches inside a worktree are still cleaned by rule; the worktree itself never is
	cands := map[string]bool{}
	for _, c := range e.Candidates(0, Safe) {
		cands[filepath.ToSlash(tr.Path(c.Node)[len(root)+1:])] = true
	}
	if !cands["wt/feature/node_modules"] || cands["wt/feature"] {
		t.Fatalf("candidates %v", cands)
	}
}

// A clone whose folder name matches a cache rule is a repository, not a cache.
func TestCheckoutNamedLikeCache(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "src/sccache/.git/HEAD", 30, 0)
	mk(t, root, "src/sccache/src/main.rs", 9000, 0)
	mk(t, root, "src/sccache/node_modules/x.js", 9000, 0)
	mk(t, root, "home/ccache/obj.o", 9000, 0) // a real compiler cache stays one
	tr, err := scan.Scan(context.Background(), root, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := New(tr)
	f := func(rel string) int32 { return tr.Find(filepath.Join(root, rel)) }
	if r := e.Own(f("src/sccache")); r == nil || r.ID != "git-repo" {
		t.Fatalf("sccache clone: %v", r)
	}
	if r := e.Own(f("home/ccache")); r == nil || r.ID != "compiler-cache" {
		t.Fatalf("ccache dir: %v", r)
	}
	cands := map[string]bool{}
	for _, c := range e.Candidates(0, Safe) {
		cands[filepath.ToSlash(tr.Path(c.Node)[len(root)+1:])] = true
	}
	if cands["src/sccache"] || !cands["src/sccache/node_modules"] || !cands["home/ccache"] {
		t.Fatalf("candidates %v", cands)
	}
}

// The developer's view: rule candidates by kind, and a lens of them judged as the scan is.
func TestDevLens(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"web/package.json", "web/node_modules/a/index.js", "web/node_modules/a/dist/a.js", "web/node_modules/a/package.json",
		"web/dist/app.js", "web/.turbo/x", "web/src/app.ts",
		"rs/Cargo.toml", "rs/target/debug/app", "py/.venv/lib/site.py", "py/.venv/lib/__pycache__/site.pyc",
		"home/.cache/huggingface/hub/model.bin", "home/.npm/_cacache/x", "home/.android/avd/Pixel.avd/userdata.img",
		"Documents/proj/node_modules/x.js", "Downloads/app.dmg",
	} {
		mk(t, root, rel, 5000, 0)
	}
	tr, err := scan.Scan(context.Background(), root, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := New(tr)
	want := map[string]DevKind{
		"web/node_modules": DevDeps, "web/dist": DevBuild, "web/.turbo": DevCache, "rs/target": DevBuild,
		"py/.venv": DevDeps, "home/.cache/huggingface": DevModels, "home/.npm": DevPackages, "home/.android/avd": DevDevices,
	}
	items := e.DevItems(0)
	byKind := map[DevKind][]scan.Item{}
	rules := map[int32]*Rule{}
	for _, it := range items {
		rel := filepath.ToSlash(tr.Path(it.Node)[len(root)+1:])
		if want[rel] != it.Kind {
			t.Errorf("%s: kind %v, want %v", rel, it.Kind, want[rel])
		}
		delete(want, rel)
		if !it.Rule.Pick {
			t.Errorf("%s: its rule %s is not a pick", rel, it.Rule.ID)
		}
		byKind[it.Kind] = append(byKind[it.Kind], scan.Item{Node: it.Node})
		rules[it.Node] = it.Rule
	}
	for rel := range want {
		t.Errorf("%s is not among the developer items", rel)
	}

	var groups []scan.Group
	for _, k := range DevKinds() {
		groups = append(groups, scan.Group{Name: k.String(), Items: byKind[k]})
	}
	l, orig := tr.Lens("dev", groups)
	own := map[int32]*Rule{}
	for i, o := range orig {
		if r := rules[o]; r != nil && l.Nodes[l.Nodes[i].Parent].Parent == 0 {
			own[int32(i)] = r
		}
	}
	for _, g := range l.Children(0, nil) {
		own[g] = &Rule{ID: "group", Level: Review}
	}
	le := e.Lens(l, orig, own)
	f := func(rel string) int32 {
		n := l.Find(filepath.Join(root, rel))
		if n == scan.None {
			t.Fatalf("no %s in the lens", rel)
		}
		return n
	}
	if v := le.Verdict(f("web/node_modules/a/index.js")); v.Rule.ID != "node-modules" || !v.Inherited {
		t.Errorf("inside node_modules: %s", v.Rule.ID)
	}
	// a rule that needs the project file beside the folder still holds, though the lens has no src/
	if v := le.Verdict(f("rs/target")); v.Rule.ID != "cargo-target" || v.Level() != Safe {
		t.Errorf("target: %s", v.Rule.ID)
	}
	// the plan of the lens is the scan's, less what is not a developer's
	got := map[string]bool{}
	var size int64
	for _, c := range le.Candidates(0, Safe) {
		got[filepath.ToSlash(l.Path(c.Node)[len(root)+1:])] = true
		size += c.Size
	}
	for _, rel := range []string{"web/node_modules", "web/.turbo", "rs/target", "home/.npm", "py/.venv/lib/__pycache__"} {
		if !got[rel] {
			t.Errorf("%s is not in the lens's plan %v", rel, got)
		}
		delete(got, rel)
	}
	if len(got) != 0 {
		t.Errorf("the lens's plan also takes %v", got)
	}
	if size <= 0 || size >= l.Nodes[0].Size {
		t.Errorf("the plan takes %d of %d", size, l.Nodes[0].Size)
	}
	if sums := le.LevelSums(0); sums[Safe]+sums[Review]+sums[Risky]+sums[Protected] != l.Nodes[0].Size {
		t.Errorf("level sums %v do not add up to the root's %d", sums, l.Nodes[0].Size)
	}
}

// Every rule a developer kind names exists.
func TestDevKindsNameRules(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range builtin.all {
		ids[r.ID] = true
	}
	for id := range devOf {
		if !ids[id] {
			t.Errorf("devOf names %q, which is not a rule", id)
		}
	}
}

// A scan that leaves unread what DevSkip names finds the same developer content.
func TestDevSkip(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"web/package.json", "web/node_modules/a/index.js", "web/.git/objects/pack.bin", "web/.git/node_modules/x.js",
		"rs/Cargo.toml", "rs/target/debug/app", "rs/tmp/scratch.bin",
		"Documents/proj/node_modules/x.js", "Pictures/2025/a.jpg", "app/desktop/node_modules/y.js",
		"mono/desktop/package.json", "mono/desktop/node_modules/d.js",
		"home/.claude/projects/s/1.jsonl", "home/.cache/huggingface/hub/model.bin", "home/.npm/_cacache/x",
		"home/.Trash/old/node_modules/z.js", "crashdumps/.git/HEAD", "crashdumps/node_modules/c.js",
	} {
		mk(t, root, rel, 5000, 0)
	}
	list := func(opt scan.Options) (map[string]int64, *scan.Tree) {
		tr, err := scan.Scan(context.Background(), root, opt)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]int64{}
		for _, it := range New(tr).DevItems(0) {
			out[filepath.ToSlash(tr.Path(it.Node)[len(root)+1:])] = it.Size
		}
		return out, tr
	}
	full, _ := list(scan.Options{})
	fast, tr := list(scan.Options{Skip: DevSkip})
	if len(full) == 0 || len(fast) != len(full) {
		t.Fatalf("developer items: %v in full, %v skipping", full, fast)
	}
	for rel, size := range full {
		if fast[rel] != size {
			t.Errorf("%s: %d in full, %d skipping", rel, size, fast[rel])
		}
	}
	// a checkout named like a cache, and a project named like a personal folder, are entered:
	// they are read
	for _, rel := range []string{"crashdumps/node_modules", "mono/desktop/node_modules"} {
		if _, ok := fast[rel]; !ok {
			t.Errorf("%s was not found: %v", rel, fast)
		}
	}
	// a folder of an everyday name is skipped only where it is a home directory's own; here
	// (a temporary folder) only reading it tells whether it is a project
	for rel, skip := range map[string]bool{
		"web/.git": true, "home/.claude/projects": true, "Documents": false, "Pictures": false, "app/desktop": false,
		"web/node_modules": false, "rs/target": false, "rs/tmp": false, "home/.cache/huggingface": false, "home/.npm": false, "crashdumps": false,
	} {
		n := tr.Find(filepath.Join(root, rel))
		if n == scan.None {
			t.Fatalf("%s is not recorded", rel)
		}
		if got := tr.Nodes[n].First == scan.None; got != skip {
			t.Errorf("%s: skipped %v, want %v", rel, got, skip)
		}
		if !tr.Nodes[n].IsDir() {
			t.Errorf("%s is no longer a folder", rel)
		}
	}
	// the .git that was not read still makes its folder a checkout
	if k := New(tr).Git(tr.Find(filepath.Join(root, "web"))); k != gitrepo.Repo {
		t.Errorf("web is %v, not a repository", k)
	}
}

// Where DevSkip decides by place.
func TestDevSkipPlaces(t *testing.T) {
	for _, c := range []struct {
		dir, name string
		skip      bool
	}{
		{"/Users/x", "Documents", true}, {"/Users/x", "Desktop", true}, {"/home/x", "Pictures", true}, {"/Users/x/Library", "Mail", true},
		{`C:\Users\x\OneDrive - Corp`, "Documents", true},
		{"/Users/x/repo/mono", "desktop", false}, {"/Users/x/Desktop", "desktop", false}, {"/Volumes/Backup", "Documents", false},
		{"/Users/x/repo/web", ".git", true}, {"/Users/x", ".ssh", true}, {"/Users/x/Library", "Mobile Documents", true},
		{"/Users/x", ".Trash", true}, {"/Users/x/repo", "tmp", false}, {"/Users/x/repo", "node_modules", false}, {"/Users/x/repo", "src", false},
	} {
		if got := DevSkip(c.dir, c.name); got != c.skip {
			t.Errorf("DevSkip(%q, %q) = %v, want %v", c.dir, c.name, got, c.skip)
		}
	}
}

// A project that is merely called like a personal folder is judged as a project; a folder of
// that name that is not one stays personal data, and so does anything inside personal data.
func TestNamesake(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"mono/.git/HEAD", "mono/desktop/package.json", "mono/desktop/src/main.ts", "mono/desktop/node_modules/a.js",
		"mono/mail/Cargo.toml", "mono/mail/target/debug/mail",
		"clone/music/.git/HEAD", "clone/music/player.c",
		"notes/documents/scan.pdf", "notes/documents/node_modules/n.js",
		"Documents/desktop/package.json", "Documents/desktop/node_modules/b.js",
		"keys/.ssh/package.json", "keys/.ssh/node_modules/c.js",
	} {
		mk(t, root, rel, 5000, 0)
	}
	tr, err := scan.Scan(context.Background(), root, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := New(tr)
	f := func(rel string) int32 {
		n := tr.Find(filepath.Join(root, rel))
		if n == scan.None {
			t.Fatalf("no %s", rel)
		}
		return n
	}
	for rel, want := range map[string]string{
		"mono/desktop":              "git-repo",     // part of the checkout it sits in
		"mono/desktop/src/main.ts":  "git-repo",     //
		"mono/desktop/node_modules": "node-modules", //
		"mono/mail/target":          "cargo-target", //
		"clone/music":               "git-repo",     // a repository of its own
		"notes/documents":           "personal",     // nothing says it is a project
		"notes/documents/scan.pdf":  "personal",     //
		"Documents":                 "personal",     //
		"Documents/desktop":         "personal",     // inside personal data: the folder above decides
		"keys/.ssh":                 "personal",     // not an everyday word: personal whatever it holds
	} {
		if v := e.Verdict(f(rel)); v.Rule.ID != want {
			t.Errorf("%s is judged by %s, want %s", rel, v.Rule.ID, want)
		}
	}
	got := map[string]bool{}
	for _, c := range e.Candidates(0, Safe) {
		got[filepath.ToSlash(tr.Path(c.Node)[len(root)+1:])] = true
	}
	for rel, want := range map[string]bool{
		"mono/desktop/node_modules": true, "mono/mail/target": true,
		"notes/documents/node_modules": false, "Documents/desktop/node_modules": false, "keys/.ssh/node_modules": false,
	} {
		if got[rel] != want {
			t.Errorf("%s among the safe candidates: %v, want %v (%v)", rel, got[rel], want, got)
		}
	}
	// a home directory's own folders are personal whatever they hold
	for p, want := range map[string]bool{
		"/users/x/documents": true, "/home/x/desktop": true, "c:/users/x/pictures": true, "c:/users/x/onedrive - corp/documents": true,
		"/users/x/library/mail": true, "/users/x/library/messages": true,
		"/users/x/repo/desktop": false, "/users/x/desktop/desktop": false, "/volumes/backup/documents": false, "/users/x/mail": false,
	} {
		if personalPlace.MatchString(p) != want {
			t.Errorf("personalPlace(%s) = %v, want %v", p, !want, want)
		}
	}
}
