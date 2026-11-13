//go:build linux || darwin

package scan

import (
	"errors"
	"os"
	"syscall"
)

// listDirGeneric: readdir + one lstat per entry. Portable fallback.
func listDirGeneric(path string, out *[]rawEntry) error {
	var f *os.File
	var err error
	for try := 0; try < 8; try++ {
		if f, err = os.Open(path); !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		return err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	base := path
	if base != "/" {
		base += "/"
	}
	for _, nm := range names {
		var st syscall.Stat_t
		if e := lstatAt(int(f.Fd()), base, nm, &st); e != nil {
			*out = append(*out, rawEntry{name: nm, err: true})
			continue
		}
		*out = append(*out, fromStat(nm, &st))
	}
	return err
}

func modeEntry(name string, mode uint32, blocks int64, mtime int64, dev, ino uint64, nlink uint32) rawEntry {
	t := mode & syscall.S_IFMT
	return rawEntry{name: name, dir: t == syscall.S_IFDIR, link: t == syscall.S_IFLNK, size: blocks * 512, mtime: uint32(mtime), dev: dev, ino: ino, nlink: nlink}
}

func rootDevices(root string) []uint64 {
	var st syscall.Stat_t
	if syscall.Stat(root, &st) != nil {
		return nil
	}
	out := []uint64{statDev(&st)}
	for _, extra := range extraRootDevices(root) {
		if syscall.Stat(extra, &st) == nil {
			out = append(out, statDev(&st))
		}
	}
	return out
}
