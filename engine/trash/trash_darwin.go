package trash

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type osMover struct{}

func devOf(p string) (uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Lstat(p, &st); err != nil {
		return 0, err
	}
	return uint64(uint32(st.Dev)), nil
}

// mountPoint walks up from p while the device stays the same.
func mountPoint(p string) string {
	d, _ := devOf(p)
	for {
		up := filepath.Dir(p)
		if up == p {
			return p
		}
		if du, err := devOf(up); err != nil || du != d {
			return p
		}
		p = up
	}
}

// Finder's layout: ~/.Trash for the home volume, <volume>/.Trashes/<uid> elsewhere.
func trashDir(p string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dp, err := devOf(p)
	if err != nil {
		return "", err
	}
	if dh, _ := devOf(home); dh == dp {
		dir := filepath.Join(home, ".Trash")
		return dir, os.MkdirAll(dir, 0o700)
	}
	dir := filepath.Join(mountPoint(p), ".Trashes", fmt.Sprint(os.Getuid()))
	return dir, os.MkdirAll(dir, 0o700)
}

func (osMover) trash(p string) (string, string, error) {
	dir, err := trashDir(p)
	if err != nil {
		return "", "", err
	}
	dst := uniqueName(dir, filepath.Base(p))
	return dst, "", os.Rename(p, dst)
}

func (osMover) restore(it *Item) error { return os.Rename(it.Stored, it.Orig) }
func (osMover) purge(it *Item) error   { return removeAll(it.Stored) }
