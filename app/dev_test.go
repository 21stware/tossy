package main

import "testing"

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{"20GB": 20 << 30, "500 mb": 500 << 20, "1.5TB": 3 << 39, "300k": 300 << 10, "4096": 4096, "": 0, "x": 0, "-3G": 0} {
		if got := parseSize(in); got != want {
			t.Errorf("parseSize(%q) = %d, want %d", in, got, want)
		}
	}
}
