//go:build !darwin && !windows

package main

// systemLocales are the POSIX locale variables.
func systemLocales() []string { return envLocales() }
