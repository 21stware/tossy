package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/fanfree/fanfree/engine/trash"
)

// Emptying looks again at the checkouts in the trash: work done after the mark is not lost silently.
func TestEmptyRisks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "old", "proj")
	os.MkdirAll(repo, 0o755)
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("new work"), 0o644)
	cache := filepath.Join(root, "cache")
	os.MkdirAll(cache, 0o755)

	items := []trash.Item{
		{ID: "a", Orig: filepath.Join(root, "old"), Dir: true},
		{ID: "b", Orig: cache, Dir: true},
	}
	risky := emptyRisks(context.Background(), items)
	if len(risky) != 1 || risky[0].id != "a" || risky[0].name != "old/proj" {
		t.Fatalf("risky %+v", risky)
	}
	chk := &emptyCheck{ids: []string{"a", "b"}, risky: risky, done: true}
	if ids := chk.safeIDs(); len(ids) != 1 || ids[0] != "b" {
		t.Fatalf("safe ids %v", ids)
	}
}
