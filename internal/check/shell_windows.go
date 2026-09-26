package check

import (
	"context"
	"os/exec"
	"syscall"
)

func shellCommand(ctx context.Context, line string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd")
	// La línea va tal cual a cmd.exe: sin esto Go re-escapa las comillas.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `/S /C "` + line + `"`}
	return cmd
}
