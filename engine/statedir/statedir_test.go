package statedir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMovesLegacy(t *testing.T) {
	base := t.TempDir()
	old := filepath.Join(base, legacy)
	os.MkdirAll(old, 0o700)
	os.WriteFile(filepath.Join(old, "trash.json"), []byte("[]"), 0o600)

	got := resolve(base)
	if want := filepath.Join(base, name); got != want {
		t.Fatalf("resolve = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(got, "trash.json")); err != nil {
		t.Fatalf("journal not carried over: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("legacy folder still there: %v", err)
	}
}

func TestResolveKeepsExisting(t *testing.T) {
	base := t.TempDir()
	os.MkdirAll(filepath.Join(base, name), 0o700)
	os.MkdirAll(filepath.Join(base, legacy), 0o700)
	if got := resolve(base); got != filepath.Join(base, name) {
		t.Fatalf("resolve = %q", got)
	}
	if _, err := os.Stat(filepath.Join(base, legacy)); err != nil {
		t.Fatalf("legacy folder touched when Tossy already exists: %v", err)
	}
}

func TestResolveFresh(t *testing.T) {
	base := t.TempDir()
	if got := resolve(base); got != filepath.Join(base, name) {
		t.Fatalf("resolve = %q", got)
	}
}
