package scan

import (
	"path/filepath"
	"sort"
	"strings"
)

// A lens is a second tree over a scanned one, for looking at part of it in another order: its
// root holds a folder per group, and each group the chosen nodes of the scan with everything
// under them. The root and the groups are virtual (they have no path on disk); below them the
// lens is the scan, so sizes, names and paths are the real ones.
//
// A lens has its own node indices. It shares the scan's name arena, and what it adds (the
// virtual folders, an item's display name) lives in a small side table.

// Group is one virtual folder of a lens.
type Group struct {
	Name  string
	Items []Item
}

// Item puts node Node of the scanned tree, with its subtree, into a group. Name, if set, is
// shown in place of the node's own name: "web/node_modules" says more than "node_modules" in
// a list of forty of them.
type Item struct {
	Node int32
	Name string
}

// Lens builds the lens of groups, its root named root. Groups without items are left out; items
// must not hold one another. The second result maps each node of the lens to its node in t,
// None for the virtual ones.
func (t *Tree) Lens(root string, groups []Group) (*Tree, []int32) {
	l := &Tree{Root: root, names: t.names, lens: true, alias: map[int32]string{}, anchor: map[int32]string{}}
	orig := []int32{None}
	l.Nodes = append(l.Nodes, Node{Flags: FlagDir, Parent: None, First: None, Next: None, Count: 1})
	l.alias[0] = root

	// largest first, at every level: the order the scan leaves its own folders in
	type sized struct {
		g    *Group
		size int64
	}
	gs := make([]sized, 0, len(groups))
	for i := range groups {
		g := &groups[i]
		items := g.Items[:0:0]
		var sum int64
		for _, it := range g.Items {
			if it.Node > 0 && int(it.Node) < len(t.Nodes) {
				items = append(items, it)
				sum += t.Nodes[it.Node].Size
			}
		}
		if len(items) == 0 {
			continue
		}
		sort.SliceStable(items, func(a, b int) bool { return t.Nodes[items[a].Node].Size > t.Nodes[items[b].Node].Size })
		gs = append(gs, sized{&Group{Name: g.Name, Items: items}, sum})
	}
	sort.SliceStable(gs, func(a, b int) bool { return gs[a].size > gs[b].size })

	// link appends n under parent, after prev (None for the first child)
	link := func(n Node, src, parent, prev int32) int32 {
		i := int32(len(l.Nodes))
		n.Parent, n.First, n.Next = parent, None, None
		l.Nodes = append(l.Nodes, n)
		orig = append(orig, src)
		if prev == None {
			l.Nodes[parent].First = i
		} else {
			l.Nodes[prev].Next = i
		}
		return i
	}
	type pair struct{ src, dst int32 }
	var queue []pair
	prevG := None
	for _, sg := range gs {
		gi := link(Node{Flags: FlagDir, Count: 1}, None, 0, prevG)
		prevG = gi
		l.alias[gi] = sg.g.Name
		prevI := None
		for _, it := range sg.g.Items {
			ii := link(t.Nodes[it.Node], it.Node, gi, prevI)
			prevI = ii
			l.anchor[ii] = t.Path(it.Node)
			if it.Name != "" {
				l.alias[ii] = it.Name
			}
			g, n := &l.Nodes[gi], &l.Nodes[ii]
			g.Size += n.Size
			g.Count += n.Count
			g.MTime = max(g.MTime, n.MTime)
			// the subtree, level by level, each folder's children in the scan's order
			queue = append(queue[:0], pair{it.Node, ii})
			for len(queue) > 0 {
				q := queue[len(queue)-1]
				queue = queue[:len(queue)-1]
				prev := None
				for c := t.Nodes[q.src].First; c != None; c = t.Nodes[c].Next {
					prev = link(t.Nodes[c], c, q.dst, prev)
					if t.Nodes[c].First != None {
						queue = append(queue, pair{c, prev})
					}
				}
			}
		}
		r, g := &l.Nodes[0], &l.Nodes[gi]
		r.Size += g.Size
		r.Count += g.Count
		r.MTime = max(r.MTime, g.MTime)
	}
	l.Stats = Stats{Bytes: l.Nodes[0].Size}
	for i := range l.Nodes {
		if orig[i] == None {
			continue
		}
		if l.Nodes[i].IsDir() {
			l.Stats.Dirs++
		} else {
			l.Stats.Files++
		}
	}
	return l, orig
}

// Virtual reports whether node i stands for no file or folder on disk: the root and the groups
// of a lens.
func (t *Tree) Virtual(i int32) bool {
	return t.lens && (i == 0 || t.Nodes[i].Parent == 0)
}

// IsLens reports whether t is a lens of a scanned tree.
func (t *Tree) IsLens() bool { return t.lens }

// findLens resolves an absolute path in a lens: under the item whose folder holds it.
func (t *Tree) findLens(path string) int32 {
	path = filepath.Clean(path)
	sep := string(filepath.Separator)
	for i, p := range t.anchor {
		if path == p {
			return i
		}
		if !strings.HasPrefix(path, strings.TrimSuffix(p, sep)+sep) {
			continue
		}
		rel, err := filepath.Rel(p, path)
		if err != nil {
			return None
		}
		cur := i
		for _, part := range strings.Split(rel, sep) {
			if cur = t.Child(cur, part); cur == None {
				return None
			}
		}
		return cur
	}
	return None
}
