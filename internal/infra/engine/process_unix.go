//go:build !windows

package engine

import "os/exec"

// configureProcess is a no-op on non-Windows platforms.
func configureProcess(_ *exec.Cmd) {}
