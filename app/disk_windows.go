package main

import "golang.org/x/sys/windows"

// diskUsage reports the volume holding path: total and free bytes for the user.
func diskUsage(path string) (total, free int64, ok bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, false
	}
	var avail, tot, all uint64
	if windows.GetDiskFreeSpaceEx(p, &avail, &tot, &all) != nil {
		return 0, 0, false
	}
	return int64(tot), int64(avail), true
}
