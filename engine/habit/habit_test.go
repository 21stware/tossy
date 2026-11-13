package habit

import (
	"context"
	"os"

	"github.com/21stware/tossy/engine/safety"
	"github.com/21stware/tossy/engine/scan"
	"path/filepath"
	"testing"
)

func TestLearnMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "habits.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	dl := "/Users/me/Downloads/"
	mk := func(name string, size int64) Subject { return Subject{Path: dl + name, Size: size, AgeDays: 3} }

	if got := s.Learn(mk("a.dmg", 100<<20), 1, 0); got != "" {
		t.Fatalf("one deletion should not form a habit, got %q", got)
	}
	if s.Match(mk("c.dmg", 50<<20)) != nil {
		t.Fatal("matched below threshold")
	}
	if got := s.Learn(mk("b.dmg", 200<<20), 1, 0); got != "Downloads 中的 .dmg 文件" {
		t.Fatalf("label = %q", got)
	}
	m := s.Match(mk("c.dmg", 50<<20))
	if m == nil || m.D != 2 {
		t.Fatalf("match = %+v", m)
	}
	// other extensions and other folders don't match
	if s.Match(mk("c.zip", 1)) != nil || s.Match(Subject{Path: "/Users/me/Desktop/x.dmg", Size: 1}) != nil {
		t.Fatal("over-generalised")
	}
	// size bound: max*4 + 64 MB
	if s.Match(mk("huge.dmg", 200<<20*4+65<<20)) != nil {
		t.Fatal("size bound ignored")
	}
	// shallow parents never generalise
	s.Learn(Subject{Path: "/Users/me/x.log", Size: 1}, 1, 0)
	s.Learn(Subject{Path: "/Users/me/y.log", Size: 1}, 1, 0)
	if s.Match(Subject{Path: "/Users/me/z.log", Size: 1}) != nil {
		t.Fatal("learned at home level")
	}

	// persistence
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s2, err := Load(path)
	if err != nil || s2.Match(mk("c.dmg", 1)) == nil {
		t.Fatalf("reload: %v", err)
	}

	// restoring a habit pick switches it off (2 - 2*1 = 0)
	if got := s2.Learn(mk("c.dmg", 1), 0, 1); got == "" {
		t.Fatal("restore should report the habit turning off")
	}
	if s2.Match(mk("d.dmg", 1)) != nil {
		t.Fatal("habit still active after restore")
	}
}

func TestOldAndUndo(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "h.json"))
	old := func(n string) Subject { return Subject{Path: "/home/me/proj/logs/" + n, Size: 10, AgeDays: 90} }
	// .75 weight: three old deletions needed (2.25 ≥ 2); the |.log signature already fires at 2
	s.Learn(old("a.txt"), 1, 0)
	s.Learn(old("b.bin"), 1, 0)
	if m := s.Match(old("c.dat")); m != nil {
		t.Fatalf("old matched too early: %+v", m)
	}
	s.Learn(old("d.csv"), 1, 0)
	m := s.Match(old("e.dat"))
	if m == nil || m.Label != "logs 中 2 个月前的内容" {
		t.Fatalf("old match = %+v", m)
	}
	// undo removes the evidence; empty entries are dropped
	s.Learn(old("d.csv"), -1, 0)
	if s.Match(old("e.dat")) != nil {
		t.Fatal("undo ignored")
	}
	s.Learn(old("a.txt"), -1, 0)
	s.Learn(old("b.bin"), -1, 0)
	if n := len(s.Entries()); n != 0 {
		t.Fatalf("%d entries left", n)
	}
}

func TestCandidates(t *testing.T) {
	root := t.TempDir()
	dl := filepath.Join(root, "a", "b", "dl")
	os.MkdirAll(filepath.Join(dl, "node_modules", "x"), 0o755)
	for _, f := range []string{"a.dmg", "b.dmg", "c.dmg", "keep.txt", "node_modules/x/y.dmg"} {
		os.WriteFile(filepath.Join(dl, f), make([]byte, 5000), 0o644)
	}
	os.MkdirAll(filepath.Join(root, "a", "documents", "dl"), 0o755)
	os.WriteFile(filepath.Join(root, "a", "documents", "dl", "z.dmg"), make([]byte, 5000), 0o644)

	tr, err := scan.Scan(context.Background(), root, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := Load(filepath.Join(t.TempDir(), "h.json"))
	for _, n := range []string{"a.dmg", "b.dmg"} {
		i := tr.Find(filepath.Join(tr.Root, "a", "b", "dl", n))
		s.Learn(Subject{Path: tr.Path(i), Size: tr.Nodes[i].Size}, 1, 0)
	}
	e := safety.New(tr)
	skip := map[int32]bool{}
	for _, c := range e.Candidates(0, safety.Safe) {
		skip[c.Node] = true
	}
	got := map[string]bool{}
	for _, p := range s.Candidates(tr, e, 0, skip) {
		got[tr.Name(p.Node)] = true
	}
	if len(got) != 3 || !got["a.dmg"] || !got["c.dmg"] {
		t.Fatalf("picks = %v", got)
	}
}
