package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// FindFirstFile/FindNextFile already return size and times, so DirEntry.Info costs no extra syscall.
func listDir(path string, out *[]rawEntry) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	des, err := f.ReadDir(-1)
	for _, de := range des {
		fi, e := de.Info()
		if e != nil {
			*out = append(*out, rawEntry{name: de.Name(), err: true})
			continue
		}
		m := fi.Mode()
		r := rawEntry{name: de.Name(), mtime: uint32(fi.ModTime().Unix())}
		switch {
		case m&fs.ModeSymlink != 0 || m&fs.ModeIrregular != 0: // symlinks, junctions, mount points: never followed
			r.link = true
		case m.IsDir():
			r.dir = true
		default:
			r.size = fi.Size()
		}
		*out = append(*out, r)
	}
	return err
}

func rootDevices(string) []uint64 { return []uint64{0} }

func defaultExcludes(root string) []string {
	vol := filepath.VolumeName(root) + `\`
	if filepath.Clean(root) != filepath.Clean(vol) {
		return nil
	}
	return []string{vol + `System Volume Information`}
}

func defaultWorkers() int { return min(max(runtime.NumCPU()*2, 4), 32) }
