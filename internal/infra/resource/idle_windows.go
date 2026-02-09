//go:build windows

package resource

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procGetLastInputInfo = user32.NewProc("GetLastInputInfo")
	procGetTickCount     = kernel32.NewProc("GetTickCount")
)

type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

// osIdleDuration returns how long the user has been idle on Windows.
// Uses GetLastInputInfo (keyboard + mouse activity).
func osIdleDuration() time.Duration {
	var info lastInputInfo
	info.cbSize = uint32(unsafe.Sizeof(info))

	ret, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return 0 // API failed, assume active
	}

	tick, _, _ := procGetTickCount.Call()
	idle := uint32(tick) - info.dwTime
	return time.Duration(idle) * time.Millisecond
}

// hasDisplay returns true if there's a graphical display.
// On Windows, desktop machines always have a display.
func hasDisplay() bool {
	return true
}

// isScreenLocked checks if the Windows workstation is locked.
// Uses user32.dll OpenInputDesktop — if it fails, screen is locked.
func isScreenLocked() bool {
	procOpenInputDesktop := user32.NewProc("OpenInputDesktop")
	procCloseDesktop := user32.NewProc("CloseDesktop")

	// OpenInputDesktop(0, false, DESKTOP_READOBJECTS)
	hDesktop, _, _ := procOpenInputDesktop.Call(0, 0, 0x0001)
	if hDesktop == 0 {
		return true // Can't open desktop → locked
	}
	procCloseDesktop.Call(hDesktop)
	return false
}
