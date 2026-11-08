//go:build windows

package pluginhost

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcAttr gives the child a process group of its own. Windows has no signals: every
// signal ends the process (this file is only compile-checked, see FINAL residual risk 8).
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

const openNoBlock = 0

// groupAlive: Windows has no group to probe once the process is gone.
func groupAlive(*os.Process) bool { return false }

func signalProc(p *os.Process, _ string) error { return p.Kill() }

func exitInfo(ps *os.ProcessState) (int, string) { return ps.ExitCode(), "" }
