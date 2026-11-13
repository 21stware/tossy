package trash

import (
	"os"
	"path/filepath"
	"testing"
)

// dirMover trashes into a plain directory — same semantics, no OS trash touched.
type dirMover struct{ dir string }

func (m dirMover) trash(p string) (string, string, error) {
	dst := uniqueName(m.dir, filepath.Base(p))
	return dst, "", os.Rename(p, dst)
}
func (dirMover) restore(it *Item) error { return os.Rename(it.Stored, it.Orig) }
func (dirMover) purge(it *Item) error   { return removeAll(it.Stored) }

func TestMarkRestoreEmpty(t *testing.T) {
	work, state, bin := t.TempDir(), t.TempDir(), t.TempDir()
	t.Cleanup(func() { removeAll(work) })
	b, err := open(state, dirMover{bin})
	if err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(work, "a.txt")
	os.WriteFile(a, []byte("hello"), 0o644)
	// read-only tree, like Go's module cache
	ro := filepath.Join(work, "mod", "pkg@v1")
	os.MkdirAll(ro, 0o755)
	os.WriteFile(filepath.Join(ro, "x.go"), []byte("package x"), 0o444)
	os.Chmod(ro, 0o555)
	os.Chmod(filepath.Dir(ro), 0o555)

	ia, err := b.Move(a, Meta{Size: 5, Source: ByManual})
	if err != nil {
		t.Fatal(err)
	}
	im, err := b.Move(filepath.Join(work, "mod"), Meta{Size: 9, Dir: true, Source: ByRule, Rule: "go-modcache"})
	if err != nil {
		t.Fatal(err)
	}
	// a mark: the file stays where it is, nothing reaches the (fake) OS trash
	if data, _ := os.ReadFile(a); string(data) != "hello" || ia.Stored != a {
		t.Fatal("marking moved the file")
	}
	if ents, _ := os.ReadDir(bin); len(ents) != 0 {
		t.Fatal("something reached the OS trash")
	}
	if _, err := b.Move(a, Meta{Size: 5}); err == nil {
		t.Fatal("double mark must fail")
	}
	if b.Size() != 14 || len(b.List()) != 2 {
		t.Fatalf("size %d list %d", b.Size(), len(b.List()))
	}
	// journal survives reopen
	b2, err := open(state, dirMover{bin})
	if err != nil || len(b2.List()) != 2 {
		t.Fatalf("reopen: %v %d", err, len(b2.List()))
	}
	// restore drops the mark
	if _, err := b2.Restore(ia.ID); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(a); string(data) != "hello" {
		t.Fatal("restore content")
	}
	if _, err := b2.Restore(ia.ID); err != ErrNotFound {
		t.Fatal("double restore must fail")
	}
	// empty deletes marked items in place, read-only trees included, and leaves the rest
	freed, err := b2.Empty(nil)
	if err != nil || freed != 9 {
		t.Fatalf("empty: %v freed %d", err, freed)
	}
	if _, err := os.Lstat(im.Orig); !os.IsNotExist(err) {
		t.Fatal("marked dir survived Empty")
	}
	if _, err := os.Lstat(a); err != nil {
		t.Fatal("unmarked file deleted")
	}
	if len(b2.List()) != 0 {
		t.Fatal("journal not cleared")
	}
}

// Journals from earlier versions hold items really moved into the OS trash.
func TestLegacyMovedItems(t *testing.T) {
	work, state, bin := t.TempDir(), t.TempDir(), t.TempDir()
	a := filepath.Join(work, "a.txt")
	stored := filepath.Join(bin, "a.txt")
	os.WriteFile(stored, []byte("old"), 0o644)
	b, _ := open(state, dirMover{bin})
	b.items["x"] = &Item{ID: "x", Orig: a, Stored: stored, Size: 3}
	if _, err := b.Restore("x"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(a); string(data) != "old" {
		t.Fatal("legacy restore")
	}
}

func TestUniqueName(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "a.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(d, ".npm"), nil, 0o644)
	if got := filepath.Base(uniqueName(d, "a.txt")); got != "a 2.txt" {
		t.Fatal(got)
	}
	if got := filepath.Base(uniqueName(d, ".npm")); got != ".npm 2" {
		t.Fatal(got)
	}
}
