package engine

import (
	"os/exec"
	"syscall"
)

// configureProcess hides the console window for subprocess on Windows.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
}
