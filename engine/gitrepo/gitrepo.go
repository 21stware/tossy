// Package gitrepo reads just enough of a Git checkout to warn before it is deleted: whether a
// folder is a repository, a linked worktree or a submodule, which worktrees share its object
// store, and (by asking the git binary) whether it holds work that exists nowhere else.
package gitrepo

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Kind of checkout a folder is.
type Kind uint8

const (
	None      Kind = iota
	Repo           // main working tree: .git is a directory
	Worktree       // linked worktree (git worktree add): .git is a file pointing into <repo>/.git/worktrees/
	Submodule      // .git is a file pointing into <repo>/.git/modules/
)

func (k Kind) String() string { return [...]string{"none", "repo", "worktree", "submodule"}[k] }

// DotGit classifies dir by its .git entry and returns the git directory it points to.
func DotGit(dir string) (Kind, string) {
	p := filepath.Join(dir, ".git")
	fi, err := os.Lstat(p)
	if err != nil {
		return None, ""
	}
	if fi.IsDir() {
		return Repo, p
	}
	if !fi.Mode().IsRegular() || fi.Size() > 4096 {
		return None, ""
	}
	return ParseDotGitFile(dir, readSmall(p))
}

// ParseDotGitFile classifies the content of a .git file found in dir.
func ParseDotGitFile(dir, content string) (Kind, string) {
	line, _, _ := strings.Cut(strings.TrimSpace(content), "\n")
	gd, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
	if !ok {
		return None, ""
	}
	gd = strings.TrimSpace(gd)
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(dir, gd)
	}
	gd = filepath.Clean(gd)
	s := filepath.ToSlash(gd)
	switch {
	case strings.Contains(s, "/worktrees/"):
		return Worktree, gd
	case strings.Contains(s, "/modules/"):
		return Submodule, gd
	}
	return None, ""
}

func readSmall(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	b := make([]byte, 4096)
	n, _ := f.Read(b)
	return string(b[:n])
}

// Tree is one working tree of a repository.
type Tree struct {
	Path     string
	Branch   string // short branch name, or "" when detached
	Main     bool   // the main working tree
	Locked   bool   // git worktree lock
	Prunable bool   // its folder is gone: git worktree prune would drop the record
}

// Family lists every working tree that shares dir's repository: the main one first, then the
// linked ones in name order. dir may be any of them. It reads .git files only; no git binary.
func Family(dir string) []Tree {
	kind, gd := DotGit(dir)
	var common string
	switch kind {
	case Repo:
		common = gd
	case Worktree:
		common = commonDir(gd)
	default:
		return nil
	}
	var out []Tree
	if filepath.Base(common) == ".git" {
		main := filepath.Dir(common)
		out = append(out, Tree{Path: main, Branch: headBranch(common), Main: true, Prunable: !exists(common)})
	}
	ents, _ := os.ReadDir(filepath.Join(common, "worktrees"))
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		wd := filepath.Join(common, "worktrees", e.Name())
		g := strings.TrimSpace(readSmall(filepath.Join(wd, "gitdir")))
		if g == "" {
			continue
		}
		if !filepath.IsAbs(g) {
			g = filepath.Join(wd, g)
		}
		p := filepath.Dir(filepath.Clean(g))
		out = append(out, Tree{Path: p, Branch: headBranch(wd), Locked: exists(filepath.Join(wd, "locked")), Prunable: !exists(g)})
	}
	return out
}

// commonDir is where a linked worktree's repository lives (<repo>/.git).
func commonDir(gd string) string {
	if c := strings.TrimSpace(readSmall(filepath.Join(gd, "commondir"))); c != "" {
		if !filepath.IsAbs(c) {
			c = filepath.Join(gd, c)
		}
		return filepath.Clean(c)
	}
	return filepath.Dir(filepath.Dir(gd)) // <common>/worktrees/<name>
}

func headBranch(gd string) string {
	h := strings.TrimSpace(readSmall(filepath.Join(gd, "HEAD")))
	if r, ok := strings.CutPrefix(h, "ref: "); ok {
		return strings.TrimPrefix(r, "refs/heads/")
	}
	return ""
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// Status of a working tree, as far as losing it is concerned.
type Status struct {
	Branch    string // "" when detached
	Changed   int    // tracked files modified, staged or conflicted
	Untracked int    // untracked files (ignored ones excluded)
	// LocalOnly counts commits that exist nowhere else. For a repository (Whole): commits on any
	// local branch that no remote has, all lost with its .git. For a linked worktree: commits on
	// its HEAD that no other branch or remote has; deleting the folder keeps the branch, so they
	// survive, but the work is unmerged.
	LocalOnly int
	Whole     bool
	Err       error
}

// Lossless reports that deleting the tree loses nothing git does not have elsewhere.
func (s Status) Lossless() bool {
	return s.Err == nil && s.Changed == 0 && s.Untracked == 0 && (!s.Whole || s.LocalOnly == 0)
}

// Stat asks git about dir. It never takes git's index lock.
func Stat(ctx context.Context, dir string) Status {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	out, err := run(ctx, dir, "status", "--porcelain=v2", "--branch", "--untracked-files=normal")
	if err != nil {
		return Status{Err: err}
	}
	s := parseStatus(out)
	kind, _ := DotGit(dir)
	s.Whole = kind == Repo
	args := []string{"rev-list", "--count", "HEAD", "--branches", "--not", "--remotes"}
	if !s.Whole {
		args = []string{"rev-list", "--count", "HEAD", "--not", "--remotes"}
		if s.Branch != "" {
			args = append(args, "--exclude="+s.Branch)
		}
		args = append(args, "--branches")
	}
	if o, err := run(ctx, dir, args...); err == nil {
		s.LocalOnly, _ = strconv.Atoi(strings.TrimSpace(string(o)))
	}
	return s
}

func parseStatus(out []byte) Status {
	var s Status
	for _, ln := range bytes.Split(out, []byte{'\n'}) {
		l := string(ln)
		switch {
		case strings.HasPrefix(l, "# branch.head "):
			if b := strings.TrimPrefix(l, "# branch.head "); b != "(detached)" {
				s.Branch = b
			}
		case strings.HasPrefix(l, "1 "), strings.HasPrefix(l, "2 "), strings.HasPrefix(l, "u "):
			s.Changed++
		case strings.HasPrefix(l, "? "):
			s.Untracked++
		}
	}
	return s
}

func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	return cmd.Output()
}

// Find lists the repositories and linked worktrees at or under root, outermost first, at most
// limit. It reads the file system (the scan may be stale), never follows symlinks, does not
// descend into node_modules (a dependency's clone is not the user's work), and gives up after
// visiting maxDirs folders, reporting that the list may be incomplete.
func Find(ctx context.Context, root string, limit, maxDirs int) (found []string, complete bool) {
	queue := []string{root}
	seen := 0
	for len(queue) > 0 {
		if ctx.Err() != nil || len(found) >= limit {
			return found, false
		}
		dir := queue[0]
		queue = queue[1:]
		if seen++; seen > maxDirs {
			return found, false
		}
		if k, _ := DotGit(dir); k == Repo || k == Worktree {
			found = append(found, dir)
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() || e.Name() == ".git" || e.Name() == "node_modules" {
				continue // DirEntry.IsDir is false for symlinks: they are not followed
			}
			queue = append(queue, filepath.Join(dir, e.Name()))
		}
	}
	return found, true
}
