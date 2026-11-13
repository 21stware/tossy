package safety

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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
		got[tr.Path(c.Node)[len(root)+1:]] = true
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
		got = append(got, tr.Path(n)[len(root)+1:])
	}
	if len(got) != 3 {
		t.Fatalf("checkouts %v", got)
	}
	// caches inside a worktree are still cleaned by rule; the worktree itself never is
	cands := map[string]bool{}
	for _, c := range e.Candidates(0, Safe) {
		cands[tr.Path(c.Node)[len(root)+1:]] = true
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
		cands[tr.Path(c.Node)[len(root)+1:]] = true
	}
	if cands["src/sccache"] || !cands["src/sccache/node_modules"] || !cands["home/ccache"] {
		t.Fatalf("candidates %v", cands)
	}
}
