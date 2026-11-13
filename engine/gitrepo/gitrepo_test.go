package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseStatus(t *testing.T) {
	s := parseStatus([]byte("# branch.oid abc\n# branch.head feat\n# branch.upstream origin/feat\n# branch.ab +2 -1\n" +
		"1 .M N... 100644 100644 100644 a b x.go\n2 R. N... 100644 100644 100644 a b y.go\tz.go\nu UU N... 1 2 3 4 a b c w.go\n? new.txt\n? tmp/\n"))
	if s.Branch != "feat" || s.Changed != 3 || s.Untracked != 2 || s.Lossless() {
		t.Fatalf("%+v", s)
	}
	if s := parseStatus([]byte("# branch.head (detached)\n")); s.Branch != "" || !s.Lossless() {
		t.Fatalf("detached: %+v", s)
	}
}

func TestParseDotGitFile(t *testing.T) {
	if k, gd := ParseDotGitFile("/r/wt", "gitdir: /r/main/.git/worktrees/wt\n"); k != Worktree || gd != filepath.Clean("/r/main/.git/worktrees/wt") {
		t.Fatal(k, gd)
	}
	if k, _ := ParseDotGitFile("/r/main/lib", "gitdir: ../.git/modules/lib"); k != Submodule {
		t.Fatal(k)
	}
	if k, _ := ParseDotGitFile("/r", "nonsense"); k != None {
		t.Fatal(k)
	}
}

// With a real git: a repository, two worktrees (one dirty, one never pushed), one deleted by hand.
func TestFamilyAndStat(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	main := filepath.Join(root, "main")
	g := func(dir string, args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(main, 0o755)
	g(main, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(main, "a.txt"), []byte("a"), 0o644)
	g(main, "add", ".")
	g(main, "commit", "-qm", "a")
	a, b, c := filepath.Join(root, "wt-a"), filepath.Join(root, "wt-b"), filepath.Join(root, "wt-c")
	g(main, "worktree", "add", "-q", "-b", "agent-a", a)
	g(main, "worktree", "add", "-q", "-b", "agent-b", b)
	g(main, "worktree", "add", "-q", "-b", "agent-c", c)
	os.WriteFile(filepath.Join(a, "a.txt"), []byte("changed"), 0o644)
	os.WriteFile(filepath.Join(b, "b.txt"), []byte("b"), 0o644)
	g(b, "add", ".")
	g(b, "commit", "-qm", "b")
	os.RemoveAll(c)

	if k, _ := DotGit(a); k != Worktree {
		t.Fatalf("kind %v", k)
	}
	fam := Family(a)
	if len(fam) != 4 || !fam[0].Main || fam[0].Branch != "main" {
		t.Fatalf("family %+v", fam)
	}
	byBranch := map[string]Tree{}
	for _, w := range fam {
		byBranch[w.Branch] = w
	}
	if !byBranch["agent-c"].Prunable || byBranch["agent-a"].Prunable {
		t.Fatalf("prunable %+v", fam)
	}
	if s := Stat(context.Background(), a); s.Changed != 1 || s.Branch != "agent-a" || s.LocalOnly != 0 || s.Lossless() {
		t.Fatalf("a: %+v", s)
	}
	// b's commit is on its branch only: unmerged, but deleting the worktree keeps the branch
	if s := Stat(context.Background(), b); s.Changed != 0 || s.LocalOnly != 1 || s.Whole || !s.Lossless() {
		t.Fatalf("b: %+v", s)
	}
	// the repository has no remote: both commits would go with its .git
	if s := Stat(context.Background(), main); !s.Whole || s.LocalOnly != 2 || s.Lossless() {
		t.Fatalf("main: %+v", s)
	}
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	mkdir := func(rel string) { os.MkdirAll(filepath.Join(root, rel), 0o755) }
	mkdir("old/a/.git")
	mkdir("old/a/sub/.git") // nested: still listed
	mkdir("old/deep/x/y/b/.git")
	mkdir("old/node_modules/dep/.git") // a dependency: skipped
	os.WriteFile(filepath.Join(root, "old/wt.git"), nil, 0o644)
	mkdir("old/wt")
	os.WriteFile(filepath.Join(root, "old/wt/.git"), []byte("gitdir: /r/.git/worktrees/wt\n"), 0o644)
	os.Symlink(filepath.Join(root, "old/a"), filepath.Join(root, "old/link")) // not followed

	got, complete := Find(context.Background(), filepath.Join(root, "old"), 10, 1000)
	if !complete || len(got) != 4 {
		t.Fatalf("found %v complete %v", got, complete)
	}
	if got[0] != filepath.Join(root, "old/a") {
		t.Fatalf("outermost first: %v", got)
	}
	if _, complete := Find(context.Background(), filepath.Join(root, "old"), 10, 2); complete {
		t.Fatal("a capped walk must say it is incomplete")
	}
}
