package main

import (
	"syscall"
	"unsafe"
)

// systemLocales is the user's display locale ("zh-TW", "ja-JP"), then the environment.
func systemLocales() []string {
	var out []string
	var buf [85]uint16 // LOCALE_NAME_MAX_LENGTH
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")
	if n, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); n > 0 {
		out = append(out, syscall.UTF16ToString(buf[:]))
	}
	return append(out, envLocales()...)
}
