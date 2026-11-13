package habit

import (
	"strings"

	"github.com/21stware/tossy/engine/safety"
	"github.com/21stware/tossy/engine/scan"
)

// Pick is a node picked because it matches a learned habit.
type Pick struct {
	Node  int32
	Match *Match
	Size  int64
}

// prefixes: every habit's parent folder and all of its ancestors (normalised). A folder outside
// this set can't contain a habit match, so the walk prunes it — a full-disk tree costs ~nothing.
func (s *Store) prefixes() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	for k, e := range s.m {
		if e.score() < threshold {
			continue
		}
		p := k[:strings.LastIndexByte(k, '|')]
		for p != "" && !out[p] {
			out[p] = true
			i := strings.LastIndexByte(p, '/')
			if i <= 0 {
				out["/"] = true
				break
			}
			p = p[:i]
		}
	}
	return out
}

// Candidates walks scope and returns review-level nodes matching an active habit. Subtrees in
// skip (already picked by a rule), anything with its own risky/protected rule, and cloud
// placeholders are never entered.
func (s *Store) Candidates(t *scan.Tree, e *safety.Evaluator, scope int32, skip map[int32]bool) []Pick {
	pre := s.prefixes()
	if len(pre) == 0 {
		return nil
	}
	var out []Pick
	var rec func(i int32, p string)
	rec = func(i int32, p string) {
		n := &t.Nodes[i]
		if r := e.Own(i); r != nil && r.Level >= safety.Risky {
			return
		}
		if !pre[p] {
			return
		}
		for c := n.First; c != scan.None; c = t.Nodes[c].Next {
			cn := &t.Nodes[c]
			if skip[c] || cn.Size == 0 || cn.Flags&scan.FlagCloud != 0 {
				continue
			}
			if e.Verdict(c).Level() == safety.Review {
				if m := s.Match(Subject{Path: t.Path(c), Dir: cn.IsDir(), Size: cn.Size, AgeDays: e.AgeDays(c)}); m != nil {
					out = append(out, Pick{Node: c, Match: m, Size: cn.Size})
					continue
				}
			}
			if cn.IsDir() {
				rec(c, strings.TrimSuffix(p, "/")+"/"+strings.ToLower(t.Name(c)))
			}
		}
	}
	root := norm(strings.TrimRight(t.Path(scope), `/\`))
	if root == "" {
		root = "/"
	}
	rec(scope, root)
	return out
}
