//go:build linux || darwin

package scan

import (
	"runtime"
	"sort"
	"testing"
)

// The bulk lister (macOS) must agree with plain lstat on real directories.
func TestListDirMatchesGeneric(t *testing.T) {
	dirs := []string{fixture(t), runtime.GOROOT() + "/src/net/http", "/usr/lib"}
	for _, d := range dirs {
		var a, b []rawEntry
		if err := listDir(d, &a); err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		if err := listDirGeneric(d, &b); err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		sort.Slice(a, func(i, j int) bool { return a[i].name < a[j].name })
		sort.Slice(b, func(i, j int) bool { return b[i].name < b[j].name })
		if len(a) != len(b) {
			t.Fatalf("%s: %d vs %d entries", d, len(a), len(b))
		}
		for i := range a {
			x, y := a[i], b[i]
			if x.name != y.name || x.dir != y.dir || x.link != y.link || x.size != y.size || x.mtime != y.mtime || x.ino != y.ino || x.dev != y.dev || (!x.dir && x.nlink != y.nlink) {
				t.Fatalf("%s: mismatch\nbulk    %+v\ngeneric %+v", d, x, y)
			}
		}
	}
}
