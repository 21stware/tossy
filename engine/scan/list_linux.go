package scan

import (
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func listDir(path string, out *[]rawEntry) error { return listDirGeneric(path, out) }

// fstatat relative to the open directory: no repeated path resolution. syscall has Fstatat on
// some architectures only (not amd64); x/sys/unix has it on all, with a Stat_t of the same
// layout, as both are the kernel's struct stat.
func lstatAt(dirfd int, _ string, name string, st *syscall.Stat_t) error {
	return unix.Fstatat(dirfd, name, (*unix.Stat_t)(unsafe.Pointer(st)), unix.AT_SYMLINK_NOFOLLOW)
}

// the cast above needs the two to match: either array's length goes negative otherwise
var (
	_ [unsafe.Sizeof(syscall.Stat_t{}) - unsafe.Sizeof(unix.Stat_t{})]byte
	_ [unsafe.Sizeof(unix.Stat_t{}) - unsafe.Sizeof(syscall.Stat_t{})]byte
)

func fromStat(name string, st *syscall.Stat_t) rawEntry {
	return modeEntry(name, st.Mode, st.Blocks, int64(st.Mtim.Sec), uint64(st.Dev), st.Ino, uint32(st.Nlink))
}

func extraRootDevices(string) []string { return nil }

// pseudo / volatile filesystems never hold reclaimable user data
func defaultExcludes(root string) []string {
	if root != "/" {
		return nil
	}
	return []string{"/proc", "/sys", "/dev", "/run", "/snap"}
}
func statDev(st *syscall.Stat_t) uint64 { return uint64(st.Dev) }

func defaultWorkers() int { return min(max(runtime.NumCPU()*2, 4), 32) }
