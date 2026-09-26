//go:build !windows

package check

import (
	"context"
	"os/exec"
)

func shellCommand(ctx context.Context, line string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", line)
}
