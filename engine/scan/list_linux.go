package scan

import (
	"runtime"
	"syscall"
)

const atSymlinkNofollow = 0x100

func listDir(path string, out *[]rawEntry) error { return listDirGeneric(path, out) }

// fstatat relative to the open directory: no repeated path resolution.
func lstatAt(dirfd int, _ string, name string, st *syscall.Stat_t) error {
	return syscall.Fstatat(dirfd, name, st, atSymlinkNofollow)
}

func fromStat(name string, st *syscall.Stat_t) rawEntry {
	return modeEntry(name, st.Mode, st.Blocks, st.Mtim.Sec, uint64(st.Dev), st.Ino, uint32(st.Nlink))
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
