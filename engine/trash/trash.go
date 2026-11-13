// Package trash moves files to the operating system's trash (macOS Trash, freedesktop Trash,
// Windows Recycle Bin) and keeps a journal of everything FanFree moved, so items can be
// restored from inside the app and "empty" only ever deletes what FanFree put there.
package trash

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Source says why an item was removed; it drives the trash view's grouping and habit learning.
type Source string

const (
	ByRule   Source = "rule"   // picked by a safety rule (auto-clean)
	ByManual Source = "manual" // the user removed it by hand
	ByHabit  Source = "habit"  // picked because it matches a learned habit
)

// Item is one journaled move.
type Item struct {
	ID     string    `json:"id"`
	Orig   string    `json:"orig"`   // where it was
	Stored string    `json:"stored"` // where it is now (inside the OS trash)
	Info   string    `json:"info,omitempty"`
	Size   int64     `json:"size"`
	Dir    bool      `json:"dir"`
	When   time.Time `json:"when"`
	Source Source    `json:"source"`
	Rule   string    `json:"rule,omitempty"`  // safety rule id
	Habit  string    `json:"habit,omitempty"` // habit label, for Source == ByHabit
	Level  int       `json:"level,omitempty"` // safety level when it was moved
}

// Meta is what the caller knows about a path it trashes.
type Meta struct {
	Size   int64
	Dir    bool
	Source Source
	Rule   string
	Habit  string
	Level  int
}

// ErrNotFound: unknown item id.
var ErrNotFound = errors.New("trash: no such item")

// Bin is the journal. Methods are safe for concurrent use.
type Bin struct {
	mu      sync.Mutex
	journal string
	items   map[string]*Item
	mover   mover
}

// mover is the OS-specific part; swapped out in tests.
type mover interface {
	// trash moves path into the trash and returns its new location (+ optional metadata file).
	trash(path string) (stored, info string, err error)
	// restore moves stored back to orig and removes metadata.
	restore(it *Item) error
	// purge permanently deletes the stored copy and metadata.
	purge(it *Item) error
}

// Open loads (or creates) the journal in stateDir.
func Open(stateDir string) (*Bin, error) {
	return open(stateDir, osMover{})
}

func open(stateDir string, m mover) (*Bin, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	b := &Bin{journal: filepath.Join(stateDir, "trash.json"), items: map[string]*Item{}, mover: m}
	data, err := os.ReadFile(b.journal)
	if err == nil {
		var list []*Item
		if json.Unmarshal(data, &list) == nil {
			for _, it := range list {
				// drop entries the user emptied/restored outside FanFree
				if _, e := os.Lstat(it.Stored); e == nil {
					b.items[it.ID] = it
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return b, nil
}

func newID() string {
	var x [8]byte
	rand.Read(x[:])
	return hex.EncodeToString(x[:])
}

// Move trashes path. Nothing is deleted: the OS trash keeps the bytes until Empty.
func (b *Bin) Move(path string, meta Meta) (*Item, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	fi, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	stored, info, err := b.mover.trash(abs)
	// moving a directory to a new parent rewrites its ".." entry, which needs write permission on
	// the directory itself — read-only trees (package caches) need a temporary u+w
	if errors.Is(err, os.ErrPermission) && fi.IsDir() && fi.Mode().Perm()&0o200 == 0 {
		if os.Chmod(abs, fi.Mode().Perm()|0o200) == nil {
			if stored, info, err = b.mover.trash(abs); err == nil {
				os.Chmod(stored, fi.Mode().Perm())
			} else {
				os.Chmod(abs, fi.Mode().Perm())
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("trash %s: %w", abs, err)
	}
	it := &Item{ID: newID(), Orig: abs, Stored: stored, Info: info, Size: meta.Size, Dir: meta.Dir, When: time.Now(), Source: meta.Source, Rule: meta.Rule, Habit: meta.Habit, Level: meta.Level}
	if it.Source == "" {
		it.Source = ByManual
	}
	b.mu.Lock()
	b.items[it.ID] = it
	err = b.saveLocked()
	b.mu.Unlock()
	return it, err
}

// Restore puts an item back where it came from.
func (b *Bin) Restore(id string) (*Item, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	it := b.items[id]
	if it == nil {
		return nil, ErrNotFound
	}
	if _, err := os.Lstat(it.Orig); err == nil {
		return nil, fmt.Errorf("restore: %s already exists", it.Orig)
	}
	if err := os.MkdirAll(filepath.Dir(it.Orig), 0o755); err != nil {
		return nil, err
	}
	err := b.mover.restore(it)
	if fi, e := os.Lstat(it.Stored); err != nil && e == nil && errors.Is(err, os.ErrPermission) && fi.IsDir() && fi.Mode().Perm()&0o200 == 0 {
		os.Chmod(it.Stored, fi.Mode().Perm()|0o200)
		if err = b.mover.restore(it); err == nil {
			os.Chmod(it.Orig, fi.Mode().Perm())
		} else {
			os.Chmod(it.Stored, fi.Mode().Perm())
		}
	}
	if err != nil {
		return nil, err
	}
	delete(b.items, id)
	return it, b.saveLocked()
}

// Empty permanently deletes the given items (all FanFree items if ids is nil). Returns bytes freed.
func (b *Bin) Empty(ids []string) (freed int64, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ids == nil {
		for id := range b.items {
			ids = append(ids, id)
		}
	}
	var errs []error
	for _, id := range ids {
		it := b.items[id]
		if it == nil {
			continue
		}
		if e := b.mover.purge(it); e != nil {
			errs = append(errs, e)
			continue
		}
		freed += it.Size
		delete(b.items, id)
	}
	if e := b.saveLocked(); e != nil {
		errs = append(errs, e)
	}
	return freed, errors.Join(errs...)
}

// List returns items, newest first.
func (b *Bin) List() []Item {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Item, 0, len(b.items))
	for _, it := range b.items {
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].When.After(out[j].When) })
	return out
}

// Size of everything FanFree has in the trash.
func (b *Bin) Size() (n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, it := range b.items {
		n += it.Size
	}
	return
}

func (b *Bin) saveLocked() error {
	list := make([]*Item, 0, len(b.items))
	for _, it := range b.items {
		list = append(list, it)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].When.Before(list[j].When) })
	data, err := json.MarshalIndent(list, "", " ")
	if err != nil {
		return err
	}
	tmp := b.journal + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.journal)
}

// uniqueName returns dir/name, or dir/"name 2", "name 3"… if taken (Finder-style).
func uniqueName(dir, name string) string {
	p := filepath.Join(dir, name)
	if _, err := os.Lstat(p); os.IsNotExist(err) {
		return p
	}
	ext := filepath.Ext(name)
	stem := name[:len(name)-len(ext)]
	if ext == name { // dotfile like ".npm"
		stem, ext = name, ""
	}
	for i := 2; ; i++ {
		p = filepath.Join(dir, fmt.Sprintf("%s %d%s", stem, i, ext))
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			return p
		}
	}
}
