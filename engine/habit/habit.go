// Package habit learns from what the user deletes by hand, so auto-clean can take the same kind
// of item next time — and explain why. A "signature" generalises one deletion to its parent
// folder plus a shape (sub-folder, file extension, or "older than two months"). A signature that
// reaches a score of 2 becomes a habit; restoring a habit-picked item counts double against it.
package habit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const threshold = 2

// Entry is the learned state of one signature.
type Entry struct {
	D     int     `json:"d"`              // manual deletions
	R     int     `json:"r"`              // restores of habit picks (negative feedback)
	Max   int64   `json:"max"`            // largest item deleted under this signature
	Label string  `json:"label"`          // human explanation, e.g. "Downloads 中的 .dmg 文件"
	W     float64 `json:"w"`              // weight (weaker signals < 1)
	Seed  bool    `json:"seed,omitempty"` // example history, not the user's own
}

func (e *Entry) score() float64 { return float64(e.D-2*e.R) * e.W }

// Subject describes an item for matching/learning.
type Subject struct {
	Path    string // absolute
	Dir     bool
	Size    int64
	AgeDays int
}

// Match explains why an item is picked.
type Match struct {
	Key   string
	Label string
	D     int
	Seed  bool
	Score float64
}

// Store holds all signatures and persists them as JSON.
type Store struct {
	mu   sync.Mutex
	path string
	m    map[string]*Entry
}

// Load reads path; a missing file is an empty store.
func Load(path string) (*Store, error) {
	s := &Store{path: path, m: map[string]*Entry{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	return s, json.Unmarshal(data, &s.m)
}

// Save writes atomically.
func (s *Store) Save() error {
	s.mu.Lock()
	data, err := json.MarshalIndent(s.m, "", " ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

type sig struct {
	key, label string
	w          float64
}

var groupy = regexp.MustCompile(`(?i)^(devices|data|cache|files|archives)$`)

func components(p string) []string {
	p = filepath.ToSlash(strings.TrimPrefix(p, filepath.VolumeName(p)))
	var out []string
	for _, c := range strings.Split(p, "/") {
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}

func norm(p string) string { return strings.ToLower(filepath.ToSlash(p)) }

// sigs never generalise over shallow folders (/, /Users, /Users/me): the parent must be ≥ 3 deep.
func sigs(s Subject) []sig {
	parent := filepath.Dir(s.Path)
	pc := components(parent)
	if len(pc) < 3 {
		return nil
	}
	where := pc[len(pc)-1]
	if len(pc) > 3 && groupy.MatchString(where) {
		where = pc[len(pc)-2] + "/" + where
	}
	base := norm(parent)
	var out []sig
	if s.Dir {
		out = append(out, sig{base + "|dir", where + " 中的文件夹", 1})
	} else if ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(s.Path), ".")); ext != "" && ext != strings.ToLower(filepath.Base(s.Path))[1:] {
		out = append(out, sig{base + "|." + ext, where + " 中的 ." + ext + " 文件", 1})
	}
	if s.AgeDays > 60 {
		out = append(out, sig{base + "|old", where + " 中 2 个月前的内容", .75})
	}
	return out
}

// Learn records a manual deletion (dd=+1), its undo (dd=-1), or a restore of a habit pick (rr=+1).
// It returns the label of a habit that just became active (or, for restores, just switched off).
func (s *Store) Learn(sub Subject, dd, rr int) (changed string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range sigs(sub) {
		e := s.m[g.key]
		if e == nil {
			e = &Entry{Label: g.label, W: g.w}
			s.m[g.key] = e
		}
		before := e.score()
		e.D = max(0, e.D+dd)
		e.R += rr
		if dd > 0 && sub.Size > e.Max {
			e.Max = sub.Size
		}
		after := e.score()
		if changed == "" && ((before < threshold && after >= threshold) || (rr > 0 && before >= threshold && after < threshold)) {
			changed = e.Label
		}
		if e.D == 0 && e.R == 0 {
			delete(s.m, g.key)
		}
	}
	return changed
}

// Match returns the strongest active habit for sub, or nil. Size is bounded so one deleted
// 10 MB log never licenses deleting a 40 GB folder.
func (s *Store) Match(sub Subject) *Match {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *Match
	for _, g := range sigs(sub) {
		e := s.m[g.key]
		if e == nil {
			continue
		}
		sc := e.score()
		if sc >= threshold && sub.Size <= e.Max*4+64<<20 && (best == nil || sc > best.Score) {
			best = &Match{Key: g.key, Label: e.Label, D: e.D, Seed: e.Seed, Score: sc}
		}
	}
	return best
}

// Entries returns a copy of all signatures (for a "learned habits" settings page).
func (s *Store) Entries() map[string]Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]Entry, len(s.m))
	for k, v := range s.m {
		out[k] = *v
	}
	return out
}

// Forget removes one signature.
func (s *Store) Forget(key string) {
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
}
