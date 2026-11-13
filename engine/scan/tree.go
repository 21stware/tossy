// Package scan walks a directory tree in parallel and builds a compact, pointer-free tree
// suitable for millions of entries: nodes live in one slice, names in one byte arena,
// children are linked lists (First/Next) sorted by size descending after the scan.
package scan

import (
	"path/filepath"
	"sort"
	"strings"
)

// Flags on a Node.
const (
	FlagDir      uint8 = 1 << iota // directory
	FlagSymlink                    // symbolic link (never followed)
	FlagErr                        // could not be read (permission, vanished…)
	FlagMount                      // another device / excluded mount: not descended
	FlagHardlink                   // extra link to an inode already counted (size 0)
	FlagPartial                    // directory listing stopped early (cancelled)
	FlagCloud                      // cloud placeholder (iCloud/OneDrive): not on disk, never descended
)

// None marks a missing index.
const None int32 = -1

// Node is 40 bytes and contains no pointers, so the GC never scans the node slice.
type Node struct {
	nameOff uint32
	nameLen uint16
	Flags   uint8
	_       uint8
	Parent  int32
	First   int32  // first child (largest after Finish)
	Next    int32  // next sibling
	Count   int32  // entries in this subtree, including the node itself
	MTime   uint32 // unix seconds
	Size    int64  // bytes on disk (allocated), aggregated for directories
}

func (n *Node) IsDir() bool { return n.Flags&FlagDir != 0 }

// Tree is the scan result. Index 0 is the root.
type Tree struct {
	Root  string // absolute path of the root
	Nodes []Node
	names []byte
	Stats Stats
}

// Stats summarises a scan.
type Stats struct {
	Files, Dirs, Errors, Hardlinks int64
	Bytes                          int64
	Millis                         int64
}

func (t *Tree) Name(i int32) string {
	n := &t.Nodes[i]
	return string(t.names[n.nameOff : n.nameOff+uint32(n.nameLen)])
}

// nameEq compares without allocating.
func (t *Tree) nameEq(i int32, s string) bool {
	n := &t.Nodes[i]
	return int(n.nameLen) == len(s) && string(t.names[n.nameOff:n.nameOff+uint32(n.nameLen)]) == s
}

// Path returns the absolute path of node i.
func (t *Tree) Path(i int32) string {
	if i == 0 {
		return t.Root
	}
	var parts []string
	for j := i; j > 0; j = t.Nodes[j].Parent {
		parts = append(parts, t.Name(j))
	}
	var b strings.Builder
	b.WriteString(t.Root)
	for k := len(parts) - 1; k >= 0; k-- {
		if !strings.HasSuffix(b.String(), string(filepath.Separator)) {
			b.WriteByte(filepath.Separator)
		}
		b.WriteString(parts[k])
	}
	return b.String()
}

// Depth of node i (root = 0).
func (t *Tree) Depth(i int32) int {
	d := 0
	for j := i; j > 0; j = t.Nodes[j].Parent {
		d++
	}
	return d
}

// Children returns the child indices of i in order (largest first after Finish).
func (t *Tree) Children(i int32, dst []int32) []int32 {
	dst = dst[:0]
	for c := t.Nodes[i].First; c != None; c = t.Nodes[c].Next {
		dst = append(dst, c)
	}
	return dst
}

// Find resolves an absolute path to a node index, or None.
func (t *Tree) Find(path string) int32 {
	path = filepath.Clean(path)
	root := filepath.Clean(t.Root)
	if path == root {
		return 0
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return None
	}
	cur := int32(0)
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		next := None
		for c := t.Nodes[cur].First; c != None; c = t.Nodes[c].Next {
			if t.nameEq(c, part) {
				next = c
				break
			}
		}
		if next == None {
			return None
		}
		cur = next
	}
	return cur
}

// finish aggregates sizes/counts bottom-up and sorts every child list by size desc.
// Children are always appended after their parent, so a reverse sweep is a post-order.
func (t *Tree) finish() {
	ns := t.Nodes
	for i := range ns {
		ns[i].Count = 1
	}
	for i := len(ns) - 1; i > 0; i-- {
		p := ns[i].Parent
		ns[p].Size += ns[i].Size
		ns[p].Count += ns[i].Count
	}
	var buf []int32
	for i := range ns {
		if ns[i].First == None || ns[ns[i].First].Next == None {
			continue
		}
		buf = buf[:0]
		for c := ns[i].First; c != None; c = ns[c].Next {
			buf = append(buf, c)
		}
		sort.Slice(buf, func(a, b int) bool {
			if ns[buf[a]].Size != ns[buf[b]].Size {
				return ns[buf[a]].Size > ns[buf[b]].Size
			}
			return buf[a] < buf[b]
		})
		ns[i].First = buf[0]
		for k := 0; k < len(buf)-1; k++ {
			ns[buf[k]].Next = buf[k+1]
		}
		ns[buf[len(buf)-1]].Next = None
	}
}

// Remove detaches node i from its parent and subtracts its size/count from all ancestors, which
// move to keep their siblings sorted by size. The node's storage stays in the slice (indices remain
// stable), so Reattach can put it back.
func (t *Tree) Remove(i int32) {
	if i <= 0 {
		return
	}
	ns := t.Nodes
	p := ns[i].Parent
	t.unlink(i)
	sz, cnt := ns[i].Size, ns[i].Count
	for a := p; a != None; a = ns[a].Parent {
		ns[a].Size -= sz
		ns[a].Count -= cnt
		if a == 0 {
			break
		}
		t.resort(a)
	}
}

// Reattach puts a node removed by Remove back under its parent (as after restoring it from the trash).
func (t *Tree) Reattach(i int32) {
	if i <= 0 {
		return
	}
	ns := t.Nodes
	p := ns[i].Parent
	t.insert(i)
	sz, cnt := ns[i].Size, ns[i].Count
	for a := p; a != None; a = ns[a].Parent {
		ns[a].Size += sz
		ns[a].Count += cnt
		if a == 0 {
			break
		}
		t.resort(a)
	}
}

// Attached reports whether i is still reachable from the root.
func (t *Tree) Attached(i int32) bool {
	for i > 0 {
		p := t.Nodes[i].Parent
		found := false
		for c := t.Nodes[p].First; c != None; c = t.Nodes[c].Next {
			if c == i {
				found = true
				break
			}
		}
		if !found {
			return false
		}
		i = p
	}
	return true
}

func (t *Tree) unlink(i int32) {
	ns := t.Nodes
	p := ns[i].Parent
	if ns[p].First == i {
		ns[p].First = ns[i].Next
	} else {
		for c := ns[p].First; c != None; c = ns[c].Next {
			if ns[c].Next == i {
				ns[c].Next = ns[i].Next
				break
			}
		}
	}
	ns[i].Next = None
}

// insert links i into its parent's child list at its place by size (descending).
func (t *Tree) insert(i int32) {
	ns := t.Nodes
	p := ns[i].Parent
	if f := ns[p].First; f == None || ns[f].Size <= ns[i].Size {
		ns[i].Next = f
		ns[p].First = i
		return
	}
	c := ns[p].First
	for ns[c].Next != None && ns[ns[c].Next].Size > ns[i].Size {
		c = ns[c].Next
	}
	ns[i].Next = ns[c].Next
	ns[c].Next = i
}

func (t *Tree) resort(i int32) {
	ns := t.Nodes
	p := ns[i].Parent
	prev := None
	for c := ns[p].First; c != None && c != i; c = ns[c].Next {
		prev = c
	}
	next := ns[i].Next
	if (prev == None || ns[prev].Size >= ns[i].Size) && (next == None || ns[next].Size <= ns[i].Size) {
		return
	}
	t.unlink(i)
	t.insert(i)
}
