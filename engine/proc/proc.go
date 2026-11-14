// Package proc starts helper programs (git, tasklist, ps) the way a GUI app should: on Windows
// a windowsgui binary would otherwise open a console window for each of them.
package proc

import (
	"context"
	"os/exec"
)

// Command is exec.CommandContext with the platform's quiet settings applied.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	hide(cmd)
	return cmd
}
