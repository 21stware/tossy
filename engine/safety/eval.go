package safety

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fanfree/fanfree/engine/gitrepo"
	"github.com/fanfree/fanfree/engine/scan"
)

// Verdict for one node.
type Verdict struct {
	Rule      *Rule
	Inherited bool // the rule belongs to an ancestor
}

func (v Verdict) Level() Level   { return v.Rule.Level }
func (v Verdict) Reason() string { return v.Rule.Reason }

// Candidate is a removable unit found by Candidates.
type Candidate struct {
	Node int32
	Rule *Rule
	Size int64
}

// Evaluator caches per-node rule lookups for one tree. Not safe for concurrent use.
type Evaluator struct {
	t     *scan.Tree
	rules *ruleSet
	own   []*Rule // own rule per node (nil = none), valid where done[i]
	done  []bool
	now   time.Time
	git   map[int32]gitrepo.Kind // checkouts seen so far
	// readGit reads a .git file (a seam for tests).
	readGit func(path string) string
}

func New(t *scan.Tree) *Evaluator {
	return &Evaluator{t: t, rules: builtin, own: make([]*Rule, len(t.Nodes)), done: make([]bool, len(t.Nodes)), now: time.Now(),
		git: map[int32]gitrepo.Kind{}, readGit: readFile}
}

// Norm turns an OS path into the form rules match against: lowercase, '/' separators.
func Norm(p string) string {
	p = strings.ToLower(strings.ReplaceAll(filepath.ToSlash(p), `\`, "/"))
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

func ext(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return strings.ToLower(name[i+1:])
	}
	return ""
}

// AgeDays since last modification.
func (e *Evaluator) AgeDays(i int32) int {
	m := e.t.Nodes[i].MTime
	if m == 0 {
		return 0
	}
	return int(e.now.Sub(time.Unix(int64(m), 0)).Hours() / 24)
}

// Own returns the rule that matches node i itself (nil if none).
func (e *Evaluator) Own(i int32) *Rule {
	if e.done[i] {
		return e.own[i]
	}
	e.done[i] = true
	t := e.t
	n := &t.Nodes[i]
	if n.Flags&scan.FlagCloud != 0 {
		e.own[i] = ruleCloud
		return ruleCloud
	}
	name := strings.ToLower(t.Name(i))
	var r *Rule
	var p string
	try := func(x *Rule) bool {
		if x.Path != nil {
			if p == "" {
				p = Norm(t.Path(i))
			}
			if !x.Path.MatchString(p) {
				return false
			}
		}
		return x.Markers == nil || e.hasMarker(n.Parent, x.Markers)
	}
	for _, x := range e.rules.byName[name] {
		if try(x) {
			r = x
			break
		}
	}
	if r == nil && n.IsDir() {
		for _, x := range e.rules.suffixes {
			if strings.HasSuffix(name, x.s) && len(name) > len(x.s) && try(x.r) {
				r = x.r
				break
			}
		}
		for _, x := range e.rules.prefixes {
			if r == nil && strings.HasPrefix(name, x.s) && try(x.r) {
				r = x.r
			}
		}
	}
	if n.IsDir() && i > 0 && (r == nil || r.Level == Safe && e.t.Child(i, ".git") != scan.None) {
		// A checkout is judged as one even when its name matches a cache rule: a clone of
		// sccache or ccache is source code, not a compiler cache. A submodule inherits.
		r = e.checkout(i)
	}
	if r == nil && !n.IsDir() && i > 0 && installerExt[ext(name)] && strings.EqualFold(t.Name(n.Parent), "downloads") {
		if e.AgeDays(i) > 30 {
			r = ruleInstallerOld
		} else {
			r = ruleInstallerNew
		}
	}
	e.own[i] = r
	return r
}

// hasMarker reports whether directory d holds an entry named like one of markers
// (lowercase; "*.ext" matches by extension).
func (e *Evaluator) hasMarker(d int32, markers []string) bool {
	if d < 0 {
		return false
	}
	t := e.t
	for c := t.Nodes[d].First; c != scan.None; c = t.Nodes[c].Next {
		nm := strings.ToLower(t.Name(c))
		for _, m := range markers {
			if m == nm || (strings.HasPrefix(m, "*") && strings.HasSuffix(nm, m[1:])) {
				return true
			}
		}
	}
	return false
}

// checkout gives a Git working tree its own rule, unless an ancestor already says something
// stricter (a repository inside Documents stays personal data).
func (e *Evaluator) checkout(i int32) *Rule {
	g := e.t.Child(i, ".git")
	if g == scan.None {
		return nil
	}
	kind := gitrepo.Repo
	if !e.t.Nodes[g].IsDir() {
		kind, _ = gitrepo.ParseDotGitFile(e.t.Path(i), e.readGit(e.t.Path(g)))
	}
	e.git[i] = kind
	var r *Rule
	switch kind {
	case gitrepo.Repo:
		r = ruleRepo
	case gitrepo.Worktree:
		r = ruleWorktree
	default:
		return nil // a submodule belongs to its repository
	}
	if par := e.t.Nodes[i].Parent; par > 0 && e.Verdict(par).Level() > r.Level {
		return nil
	}
	return r
}

// Git says what kind of checkout node i is (gitrepo.None if it is not one).
func (e *Evaluator) Git(i int32) gitrepo.Kind {
	e.Own(i)
	return e.git[i]
}

// Checkouts lists the Git working trees at or under scope, outermost first, at most limit.
// Nothing inside node_modules-like rule picks or personal data is entered.
func (e *Evaluator) Checkouts(scope int32, limit int) []int32 {
	var out []int32
	t := e.t
	stack := []int32{scope}
	for len(stack) > 0 && len(out) < limit {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := &t.Nodes[i]
		if !n.IsDir() || n.Flags&(scan.FlagSymlink|scan.FlagCloud) != 0 {
			continue
		}
		if k := e.Git(i); k == gitrepo.Repo || k == gitrepo.Worktree {
			out = append(out, i)
		}
		if r := e.Own(i); r != nil && r.Pick && r.Level == Safe {
			continue // caches don't hold checkouts worth a warning
		}
		for c := n.First; c != scan.None; c = t.Nodes[c].Next {
			if t.Nodes[c].IsDir() && t.Name(c) != ".git" {
				stack = append(stack, c)
			}
		}
	}
	return out
}

func readFile(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	b := make([]byte, 1024)
	k, _ := f.Read(b)
	return string(b[:k])
}

// Verdict for node i: its own rule, else the nearest ancestor's; direct children of the scan root default to Review.
func (e *Evaluator) Verdict(i int32) Verdict {
	inh := false
	for j := i; ; j = e.t.Nodes[j].Parent {
		if r := e.Own(j); r != nil {
			return Verdict{Rule: r, Inherited: inh}
		}
		if j == 0 {
			return Verdict{Rule: ruleRoot, Inherited: inh}
		}
		if e.t.Nodes[j].Parent == 0 {
			return Verdict{Rule: ruleDefault, Inherited: inh}
		}
		inh = true
	}
}

// Candidates collects non-overlapping removable units under scope whose level is at most max.
// Risky subtrees are never entered; protected rules are never picked (but rules below them may match).
func (e *Evaluator) Candidates(scope int32, max Level) []Candidate {
	var out []Candidate
	t := e.t
	var rec func(i int32)
	rec = func(i int32) {
		n := &t.Nodes[i]
		if n.Size == 0 && i != scope {
			return
		}
		if r := e.Own(i); r != nil {
			if r.Level == Risky || r == ruleCloud {
				return
			}
			if r.Level <= max && r.Level != Protected {
				if r.Split && n.IsDir() {
					for c := n.First; c != scan.None; c = t.Nodes[c].Next {
						if e.Own(c) != nil {
							rec(c) // a child with a rule of its own (a model cache in ~/.cache) is judged by it
						} else if t.Nodes[c].Size > 0 && t.Nodes[c].Flags&scan.FlagCloud == 0 {
							out = append(out, Candidate{Node: c, Rule: r, Size: t.Nodes[c].Size})
						}
					}
					return
				}
				if r.Pick {
					out = append(out, Candidate{Node: i, Rule: r, Size: n.Size})
					return
				}
			}
		}
		for c := n.First; c != scan.None; c = t.Nodes[c].Next {
			rec(c)
		}
	}
	rec(scope)
	return out
}

// LevelSums adds up file sizes under i by verdict level.
func (e *Evaluator) LevelSums(i int32) (s [4]int64) {
	t := e.t
	var rec func(j int32, lv Level)
	rec = func(j int32, lv Level) {
		if r := e.Own(j); r != nil {
			lv = r.Level
		} else if j == 0 {
			lv = Protected
		} else if t.Nodes[j].Parent == 0 {
			lv = Review
		}
		n := &t.Nodes[j]
		if n.First == scan.None {
			s[lv] += n.Size
			return
		}
		for c := n.First; c != scan.None; c = t.Nodes[c].Next {
			rec(c, lv)
		}
	}
	rec(i, e.Verdict(i).Level())
	return
}
