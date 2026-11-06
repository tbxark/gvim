//go:build windows

package app

import "syscall"

// processAlive reports whether a process with the id pid is running. A process that this user
// may not open is running too.
func processAlive(pid int) bool {
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return err == syscall.ERROR_ACCESS_DENIED
	}
	_ = syscall.CloseHandle(h)
	return true
}
