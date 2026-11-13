package trash

import (
	"errors"
	"os"
	"path/filepath"
)

// removeAll deletes p recursively. Unlike os.RemoveAll it never opens p's parent directory:
// on macOS ~/.Trash is privacy-protected (listing it needs Full Disk Access) while opening and
// unlinking entries inside it does not. It also grants the owner rwx on read-only directories
// (Go's module cache, some package managers) so their contents can be removed.
func removeAll(p string) error {
	err := os.Remove(p)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	fi, lerr := os.Lstat(p)
	if lerr != nil {
		if os.IsNotExist(lerr) {
			return nil
		}
		return lerr
	}
	if !fi.IsDir() {
		return err
	}
	if fi.Mode().Perm()&0o700 != 0o700 {
		os.Chmod(p, fi.Mode().Perm()|0o700)
	}
	var errs []error
	for {
		ents, rerr := os.ReadDir(p)
		if rerr != nil && len(ents) == 0 {
			return rerr
		}
		for _, e := range ents {
			if e := removeAll(filepath.Join(p, e.Name())); e != nil {
				errs = append(errs, e)
			}
		}
		if len(errs) > 0 || len(ents) == 0 {
			break
		}
		// re-list: ReadDir has no batch limit, but a concurrent writer may have added entries
		if err = os.Remove(p); err == nil || os.IsNotExist(err) {
			return nil
		}
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
