package main

import (
	"os"
	"testing"
	"time"

	"github.com/21stware/tossy/engine/scan"
)

// Putting something in the trash is a mark in Tossy only: the file stays on disk, a rescan
// keeps it hidden, restoring brings it back, and emptying deletes it in place.
func TestTrashIsAMark(t *testing.T) {
	a := loaded(t)
	n := a.t.Find(a.root + "/Downloads/Installer.dmg")
	if n == scan.None {
		t.Fatal("fixture file missing")
	}
	p, before := a.t.Path(n), a.t.Nodes[0].Size
	a.trashOne(n)
	for i := 0; i < 200 && a.trashMoving > 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Lstat(p); err != nil {
		t.Fatal("file left its place:", err)
	}
	if len(a.bin.List()) != 1 {
		t.Fatal("not marked")
	}
	rescan := func() {
		a.startScan(a.root)
		for i := 0; i < 1000 && a.scanning; i++ {
			time.Sleep(10 * time.Millisecond)
		}
	}
	rescan()
	m, ok := a.trashed[a.bin.List()[0].ID]
	if a.t.Find(p) != scan.None || !ok || a.t.Path(m) != p || a.t.Nodes[0].Size >= before {
		t.Fatal("rescan shows the marked file again")
	}
	a.restore([]string{a.bin.List()[0].ID})
	if a.t.Nodes[0].Size != before || !a.t.Attached(a.t.Find(p)) {
		t.Fatal("restore did not bring it back")
	}
	a.trashOne(a.t.Find(p))
	for i := 0; i < 200 && a.trashMoving > 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := a.bin.Empty(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatal("empty left the file")
	}
}
