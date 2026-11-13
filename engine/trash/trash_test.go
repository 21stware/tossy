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

func TestMoveRestoreEmpty(t *testing.T) {
	work, state, bin := t.TempDir(), t.TempDir(), t.TempDir()
	t.Cleanup(func() { removeAll(work); removeAll(bin) })
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
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatal("original still present")
	}
	if b.Size() != 14 || len(b.List()) != 2 {
		t.Fatalf("size %d list %d", b.Size(), len(b.List()))
	}
	// journal survives reopen
	b2, err := open(state, dirMover{bin})
	if err != nil || len(b2.List()) != 2 {
		t.Fatalf("reopen: %v %d", err, len(b2.List()))
	}
	// restore puts it back
	if _, err := b2.Restore(ia.ID); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(a); string(data) != "hello" {
		t.Fatal("restore content")
	}
	if _, err := b2.Restore(ia.ID); err != ErrNotFound {
		t.Fatal("double restore must fail")
	}
	// read-only directories round-trip too
	if _, err := b2.Restore(im.ID); err != nil {
		t.Fatal("restore read-only dir:", err)
	}
	if fi, _ := os.Stat(filepath.Join(work, "mod")); fi == nil || fi.Mode().Perm() != 0o555 {
		t.Fatal("mode not preserved")
	}
	if im, err = b2.Move(filepath.Join(work, "mod"), Meta{Size: 9, Dir: true, Source: ByRule}); err != nil {
		t.Fatal(err)
	}
	// empty removes read-only trees for good
	freed, err := b2.Empty(nil)
	if err != nil || freed != 9 {
		t.Fatalf("empty: %v freed %d", err, freed)
	}
	if _, err := os.Lstat(im.Stored); !os.IsNotExist(err) {
		t.Fatal("stored copy survived Empty")
	}
	if len(b2.List()) != 0 {
		t.Fatal("journal not cleared")
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

// Real OS trash round-trip; opt-in because it touches the user's Trash.
func TestOSTrashRoundTrip(t *testing.T) {
	if os.Getenv("FANFREE_OS_TRASH") == "" {
		t.Skip("set FANFREE_OS_TRASH=1")
	}
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".fanfree-trash-test.txt")
	os.WriteFile(p, []byte("x"), 0o644)
	b, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := b.Move(p, Meta{Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("stored at %s", it.Stored)
	if _, err := os.Lstat(it.Stored); err != nil {
		t.Fatal("not in trash:", err)
	}
	if _, err := b.Restore(it.ID); err != nil {
		t.Fatal(err)
	}
	it, _ = b.Move(p, Meta{Size: 1})
	if _, err := b.Empty([]string{it.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatal("original back?")
	}
	if _, err := os.Lstat(it.Stored); !os.IsNotExist(err) {
		t.Fatal("trash copy survived")
	}
}
