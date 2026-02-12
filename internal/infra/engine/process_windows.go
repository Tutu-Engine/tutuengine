package engine

import (
	"os/exec"
	"syscall"
)

// configureProcess hides the console window for subprocess on Windows
// and creates a new process group so we can kill the entire process tree.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
