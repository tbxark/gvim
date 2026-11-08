//go:build !windows

package restore

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with the id pid is running. A process that this user
// may not signal is running too.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
