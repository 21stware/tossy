package safety

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fanfree/fanfree/engine/scan"
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
	if v := e.Verdict(f("proj/src/main.go")); v.Rule.ID != "default" {
		t.Fatalf("default: %s", v.Rule.ID)
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
