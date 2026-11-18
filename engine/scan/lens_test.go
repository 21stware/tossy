package scan

import (
	"context"
	"path/filepath"
	"testing"
)

// A lens regroups nodes of a scan under folders of its own: sizes, order, names and paths.
func TestLens(t *testing.T) {
	root := fixture(t)
	tr, err := Scan(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	f := func(rel string) int32 {
		n := tr.Find(filepath.Join(root, rel))
		if n == None {
			t.Fatalf("no %s in the scan", rel)
		}
		return n
	}
	big, deep, top := f("big"), f("small/deep"), f("top.dat")
	l, orig := tr.Lens("Lens", []Group{
		{Name: "one", Items: []Item{{Node: deep, Name: "small/deep"}, {Node: top}}},
		{Name: "none"},
		{Name: "two", Items: []Item{{Node: big}}},
	})
	if !l.IsLens() || tr.IsLens() {
		t.Fatal("IsLens")
	}
	if got, want := l.Nodes[0].Size, tr.Nodes[big].Size+tr.Nodes[deep].Size+tr.Nodes[top].Size; got != want {
		t.Fatalf("root size %d, want %d", got, want)
	}
	if got, want := l.Nodes[0].Count, 1+2+tr.Nodes[big].Count+tr.Nodes[deep].Count+tr.Nodes[top].Count; got != want {
		t.Fatalf("root count %d, want %d", got, want)
	}
	// groups by size, the empty one left out
	gs := l.Children(0, nil)
	if len(gs) != 2 || l.Name(gs[0]) != "two" || l.Name(gs[1]) != "one" {
		t.Fatalf("groups: %v", gs)
	}
	for _, g := range gs {
		if !l.Virtual(g) || l.Path(g) != "" || orig[g] != None {
			t.Fatalf("group %s is not virtual", l.Name(g))
		}
	}
	if !l.Virtual(0) || l.Path(0) != "" || l.Name(0) != "Lens" {
		t.Fatal("the root is not virtual")
	}
	// items by size, under their display names, at their real paths
	one := l.Children(gs[1], nil)
	if len(one) != 2 || l.Name(one[0]) != "top.dat" || l.Name(one[1]) != "small/deep" {
		t.Fatalf("items of one: %q %q", l.Name(one[0]), l.Name(one[1]))
	}
	if l.Virtual(one[1]) || l.Path(one[1]) != tr.Path(deep) || orig[one[1]] != deep {
		t.Fatalf("item path %q", l.Path(one[1]))
	}
	// the subtree came along, node for node
	for i := range l.Nodes {
		o := orig[i]
		if o == None {
			continue
		}
		if l.Path(int32(i)) != tr.Path(o) || l.Nodes[i].Size != tr.Nodes[o].Size || l.Nodes[i].Count != tr.Nodes[o].Count {
			t.Fatalf("node %d (%s) differs from its original %s", i, l.Path(int32(i)), tr.Path(o))
		}
	}
	z := filepath.Join(root, "small", "deep", "y", "z.txt")
	if n := l.Find(z); n == None || l.Path(n) != z || l.Name(n) != "z.txt" {
		t.Fatalf("Find(%s) = %d", z, n)
	}
	if n := l.Find(filepath.Join(root, "big")); n == None || l.Nodes[n].Parent != gs[0] {
		t.Fatalf("Find(big) = %d", n)
	}
	if l.Find(filepath.Join(root, "small")) != None || l.Find(filepath.Join(root, "bigger")) != None {
		t.Fatal("Find found what the lens does not hold")
	}
	// removing an item takes its size out of its group and the root, and Reattach puts it back
	before := l.Nodes[0].Size
	l.Remove(one[0])
	if l.Nodes[0].Size != before-tr.Nodes[top].Size || l.Nodes[gs[1]].Size != tr.Nodes[deep].Size {
		t.Fatal("sizes after Remove")
	}
	l.Reattach(one[0])
	if l.Nodes[0].Size != before || l.Children(gs[1], nil)[0] != one[0] {
		t.Fatal("sizes and order after Reattach")
	}
	// the scan itself is untouched
	if tr.Name(deep) != "deep" || tr.Path(0) != tr.Root || tr.Virtual(0) {
		t.Fatal("the scan changed")
	}
}
