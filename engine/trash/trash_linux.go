package trash

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// freedesktop.org Trash specification 1.0.

type osMover struct{}

func devOf(p string) (uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Lstat(p, &st); err != nil {
		return 0, err
	}
	return uint64(st.Dev), nil
}

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

// trashDir returns the trash root and the directory paths in .trashinfo are relative to ("" = absolute).
func trashDir(p string) (root, top string, err error) {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return "", "", e
		}
		data = filepath.Join(home, ".local", "share")
	}
	home := filepath.Join(data, "Trash")
	os.MkdirAll(data, 0o700)
	dh, _ := devOf(data)
	dp, err := devOf(p)
	if err != nil {
		return "", "", err
	}
	if dh == dp {
		return home, "", nil
	}
	top = mountPoint(p)
	return filepath.Join(top, fmt.Sprintf(".Trash-%d", os.Getuid())), top, nil
}

func (osMover) trash(p string) (string, string, error) {
	root, top, err := trashDir(p)
	if err != nil {
		return "", "", err
	}
	files, infos := filepath.Join(root, "files"), filepath.Join(root, "info")
	if err := os.MkdirAll(files, 0o700); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(infos, 0o700); err != nil {
		return "", "", err
	}
	rel := p
	if top != "" {
		if r, e := filepath.Rel(top, p); e == nil {
			rel = r
		}
	}
	esc := (&url.URL{Path: rel}).EscapedPath()
	base := filepath.Base(p)
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			ext := filepath.Ext(base)
			name = fmt.Sprintf("%s.%d%s", strings.TrimSuffix(base, ext), i, ext)
		}
		info := filepath.Join(infos, name+".trashinfo")
		f, err := os.OpenFile(info, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // O_EXCL reserves the name
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		fmt.Fprintf(f, "[Trash Info]\nPath=%s\nDeletionDate=%s\n", esc, time.Now().Format("2006-01-02T15:04:05"))
		f.Close()
		dst := filepath.Join(files, name)
		if _, e := os.Lstat(dst); e == nil {
			os.Remove(info)
			continue
		}
		if err := os.Rename(p, dst); err != nil {
			os.Remove(info)
			return "", "", err
		}
		return dst, info, nil
	}
}

func (osMover) restore(it *Item) error {
	if err := os.Rename(it.Stored, it.Orig); err != nil {
		return err
	}
	os.Remove(it.Info)
	return nil
}

func (osMover) purge(it *Item) error {
	if err := removeAll(it.Stored); err != nil {
		return err
	}
	os.Remove(it.Info)
	return nil
}
