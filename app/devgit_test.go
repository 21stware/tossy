package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// A repository with agent worktrees: the detail lists them with what each would lose, the
// project's build caches are grouped, and a delete stops to say so.
func TestGitWorktrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	t.Setenv("FANFREE_STATE", t.TempDir())
	root := t.TempDir()
	repo := filepath.Join(root, "app")
	g := func(dir string, args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel string, n int) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, make([]byte, n), 0o644)
	}
	write("app/package.json", 100)
	write("app/.gitignore", 0)
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("node_modules\n.next\n"), 0o644)
	g(repo, "init", "-q", "-b", "main")
	g(repo, "add", ".")
	g(repo, "commit", "-qm", "init")
	for _, w := range []string{"agent-1", "agent-2", "agent-3"} {
		g(repo, "worktree", "add", "-q", "-b", w, filepath.Join(root, "worktrees", w))
		write("worktrees/"+w+"/node_modules/react/index.js", 400_000)
		write("worktrees/"+w+"/.next/cache/a", 200_000)
	}
	write("worktrees/agent-2/src/new.ts", 3000) // untracked work
	write("app/node_modules/react/index.js", 500_000)

	a := newApp()
	a.startScan(root)
	for deadline := time.Now().Add(10 * time.Second); a.scanning; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("scan timeout")
		}
	}
	tt := ui.NewTester(a.view, 1280, 820)
	tt.Advance(2 * time.Second)
	w2 := a.t.Find(filepath.Join(root, "worktrees", "agent-2"))
	if w2 < 0 || a.verdict(w2).Rule.ID != "git-worktree" {
		t.Fatalf("agent-2 verdict %v", a.verdict(w2).Rule.ID)
	}
	a.zoomTo(w2)
	a.git.open, a.git.devO = true, true
	tt.Advance(time.Second)
	for i := 0; i < 50 && !tt.HasText("1 个新文件未跟踪"); i++ {
		time.Sleep(20 * time.Millisecond)
		tt.Frame()
	}
	for _, s := range []string{"Git worktree · 共 4 个工作区 · 1 个新文件未跟踪", "1 个新文件未跟踪", "app · main（主工作区）", "node_modules", "可重建的开发缓存 · 588.0 KB", "此仓库有 3 个 worktree，本机可见部分合计 1.76 MB。合并后不再需要的，建议在仓库中运行 git worktree remove 移除。"} {
		if !tt.HasText(s) {
			t.Errorf("detail lacks %q: %v", s, tt.Texts())
		}
	}
	shot(t, tt, "git-1-detail")

	// the worktree's caches are auto-clean candidates; the worktree is not
	inPlan := map[string]bool{}
	for _, it := range a.candItems(0, 0) {
		inPlan[a.t.Path(it.node)[len(root)+1:]] = true
	}
	if !inPlan[filepath.Join("worktrees", "agent-1", "node_modules")] || !inPlan[filepath.Join("worktrees", "agent-1", ".next")] || inPlan[filepath.Join("worktrees", "agent-1")] {
		t.Errorf("plan %v", inPlan)
	}

	// deleting the worktrees folder asks first, and names the one with work in it
	wt := a.t.Nodes[w2].Parent
	a.requestTrash(wt)
	tt.Frame()
	if a.ask == nil || a.ask.kind != askGit || len(a.ask.list) != 3 {
		t.Fatalf("ask %+v", a.ask)
	}
	tt.Advance(time.Second)
	for i := 0; i < 50 && tt.HasText("正在检查…"); i++ {
		time.Sleep(20 * time.Millisecond)
		tt.Frame()
	}
	if !tt.HasText("删除 Git 工作区？") || !tt.HasText("1 个新文件未跟踪") {
		t.Errorf("ask texts: %v", tt.Texts())
	}
	shot(t, tt, "git-2-ask")
	a.askClose()
	tt.Frame()
	tt.Advance(time.Second)

	// deleting the repository warns about the worktrees that live off it
	a.requestTrash(a.t.Find(repo))
	tt.Frame()
	tt.Advance(time.Second)
	for i := 0; i < 50 && tt.HasText("正在检查…"); i++ {
		time.Sleep(20 * time.Millisecond)
		tt.Frame()
	}
	if !tt.HasText("另有 3 个 worktree 依赖这里的仓库，删除后它们将无法使用。") {
		t.Errorf("repo ask: %v", tt.Texts())
	}
	if !tt.HasText("1 个提交只在本机") || !tt.HasText("删除这个 Git 仓库？") {
		t.Errorf("repo ask: %v", tt.Texts())
	}
	shot(t, tt, "git-3-repo")
}
