//go:build !windows

package monitor

import (
	"context"
	"os/exec"
)

func hiddenCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
