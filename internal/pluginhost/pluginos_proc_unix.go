//go:build !windows

package pluginhost

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcAttr puts the child in a process group of its own, so signalProc reaches every
// process the child started.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// openNoBlock keeps os.OpenFile from blocking on a FIFO.
const openNoBlock = syscall.O_NONBLOCK

// groupAlive reports whether the process group led by p still has a member.
func groupAlive(p *os.Process) bool {
	err := syscall.Kill(-p.Pid, 0)
	return err == nil || err == syscall.EPERM
}

// signalProc sends a signal ("" or TERM, KILL, INT, HUP) to the process group of p.
func signalProc(p *os.Process, signal string) error {
	sig := syscall.SIGTERM
	switch signal {
	case "KILL":
		sig = syscall.SIGKILL
	case "INT":
		sig = syscall.SIGINT
	case "HUP":
		sig = syscall.SIGHUP
	}
	return syscall.Kill(-p.Pid, sig)
}

// exitInfo is the exit code of a finished child, or -1 and the signal name when a signal
// ended it.
func exitInfo(ps *os.ProcessState) (int, string) {
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		name := ws.Signal().String()
		switch ws.Signal() {
		case syscall.SIGKILL:
			name = "KILL"
		case syscall.SIGTERM:
			name = "TERM"
		case syscall.SIGINT:
			name = "INT"
		case syscall.SIGHUP:
			name = "HUP"
		}
		return -1, name
	}
	return ps.ExitCode(), ""
}
