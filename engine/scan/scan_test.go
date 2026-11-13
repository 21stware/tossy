package scan

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func write(t *testing.T, p string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i) // incompressible-ish, avoids sparse/compressed surprises
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	root := t.TempDir()
	write(t, filepath.Join(root, "big", "a.bin"), 1<<20)
	write(t, filepath.Join(root, "big", "b.bin"), 300<<10)
	write(t, filepath.Join(root, "small", "x.txt"), 10)
	write(t, filepath.Join(root, "small", "deep", "y", "z.txt"), 5000)
	write(t, filepath.Join(root, "top.dat"), 64<<10)
	os.MkdirAll(filepath.Join(root, "empty"), 0o755)
	return root
}

func TestScanBasics(t *testing.T) {
	root := fixture(t)
	tr, err := Scan(context.Background(), root, Options{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Stats.Files != 5 || tr.Stats.Dirs != 6 {
		t.Fatalf("files=%d dirs=%d", tr.Stats.Files, tr.Stats.Dirs)
	}
	// root + {big,small,top.dat,empty} + {a,b} + {x,deep} + y + z = 11
	if got := tr.Nodes[0].Count; got != 11 {
		t.Fatalf("count=%d", got)
	}
	// largest child first
	kids := tr.Children(0, nil)
	if tr.Name(kids[0]) != "big" {
		t.Fatalf("first child %q", tr.Name(kids[0]))
	}
	for i := 1; i < len(kids); i++ {
		if tr.Nodes[kids[i]].Size > tr.Nodes[kids[i-1]].Size {
			t.Fatal("children not sorted by size")
		}
	}
	// sizes aggregate
	var sum int64
	for _, k := range kids {
		sum += tr.Nodes[k].Size
	}
	if sum != tr.Nodes[0].Size || sum < (1<<20)+(300<<10) {
		t.Fatalf("sum=%d root=%d", sum, tr.Nodes[0].Size)
	}
	// Find / Path round-trip
	p := filepath.Join(root, "small", "deep", "y", "z.txt")
	i := tr.Find(p)
	if i == None || tr.Path(i) != p {
		t.Fatalf("find/path: %d %q", i, tr.Path(i))
	}
	if tr.Depth(i) != 4 {
		t.Fatalf("depth %d", tr.Depth(i))
	}
	// Remove updates ancestors
	before := tr.Nodes[0].Size
	big := tr.Find(filepath.Join(root, "big"))
	bs := tr.Nodes[big].Size
	tr.Remove(big)
	if tr.Nodes[0].Size != before-bs || tr.Find(filepath.Join(root, "big")) != None {
		t.Fatal("remove did not update tree")
	}
}

func TestHardlinkAndSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	root := t.TempDir()
	write(t, filepath.Join(root, "f"), 200<<10)
	if err := os.Link(filepath.Join(root, "f"), filepath.Join(root, "g")); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(root, "real"), 0o755)
	write(t, filepath.Join(root, "real", "r"), 100<<10)
	os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "ln"))
	tr, err := Scan(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Stats.Hardlinks != 1 {
		t.Fatalf("hardlinks=%d", tr.Stats.Hardlinks)
	}
	f, g := tr.Nodes[tr.Find(filepath.Join(root, "f"))], tr.Nodes[tr.Find(filepath.Join(root, "g"))]
	if (f.Size == 0) == (g.Size == 0) {
		t.Fatalf("exactly one link should carry the size: %d %d", f.Size, g.Size)
	}
	ln := tr.Find(filepath.Join(root, "ln"))
	if tr.Nodes[ln].Flags&FlagSymlink == 0 || tr.Nodes[ln].First != None {
		t.Fatal("symlink must be recorded and not followed")
	}
}

func TestCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tr, err := Scan(ctx, fixture(t), Options{})
	if err == nil || tr == nil {
		t.Fatal("expected partial tree and ctx error")
	}
}

func TestRemoveReattachKeepsOrder(t *testing.T) {
	root := t.TempDir()
	for _, d := range []struct {
		dir  string
		size int
	}{{"a", 300000}, {"b", 200000}, {"c", 100000}} {
		os.MkdirAll(filepath.Join(root, d.dir), 0o755)
		os.WriteFile(filepath.Join(root, d.dir, "big"), make([]byte, d.size), 0o644)
		os.WriteFile(filepath.Join(root, d.dir, "small"), make([]byte, 50000), 0o644)
	}
	tr, err := Scan(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	names := func() (s string) {
		for _, c := range tr.Children(0, nil) {
			s += tr.Name(c)
		}
		return
	}
	if names() != "abc" {
		t.Fatalf("start order %s", names())
	}
	big := tr.Find(filepath.Join(tr.Root, "a", "big"))
	total := tr.Nodes[0].Size
	tr.Remove(big)
	if names() != "bca" || tr.Attached(big) {
		t.Fatalf("after remove %s", names())
	}
	tr.Reattach(big)
	if names() != "abc" || tr.Nodes[0].Size != total || !tr.Attached(big) {
		t.Fatalf("after reattach %s", names())
	}
}
