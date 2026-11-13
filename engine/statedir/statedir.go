// Package statedir locates Tossy's state (trash journal, habits, settings): $TOSSY_STATE, or a
// "Tossy" folder in the user's config directory. The app was called FanFree before; the first
// call moves an existing "FanFree" folder there so the trash and learned habits carry over.
package statedir

import (
	"os"
	"path/filepath"
	"sync"
)

const (
	name   = "Tossy"
	legacy = "FanFree"
)

var (
	once sync.Once
	dir  string
)

// Dir is the state directory. It does not create it.
func Dir() string {
	if d := os.Getenv("TOSSY_STATE"); d != "" {
		return d
	}
	once.Do(func() {
		base, err := os.UserConfigDir()
		if err != nil {
			base = os.TempDir()
		}
		dir = resolve(base)
	})
	return dir
}

// resolve picks <base>/Tossy, first moving <base>/FanFree there when only the old one exists.
// If the move fails the old folder keeps being used, so nothing is lost.
func resolve(base string) string {
	d := filepath.Join(base, name)
	if _, err := os.Lstat(d); !os.IsNotExist(err) {
		return d
	}
	old := filepath.Join(base, legacy)
	if fi, err := os.Stat(old); err != nil || !fi.IsDir() {
		return d
	}
	if os.Rename(old, d) != nil {
		return old
	}
	return d
}
