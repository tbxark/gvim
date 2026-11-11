//go:build unix

package terminal

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// The foreground-process name is Fresh's other half of the terminal tab title:
// Fresh reads tcgetpgrp(master) and (on Linux) /proc/<pgid>/comm once a second and
// combines that command name with the program's OSC title. gvim does the same for
// a local Unix PTY, with bounded, non-blocking introspection: the syscall is a
// single ioctl, the name read is capped, and the fallback on platforms without
// /proc (macOS, the BSDs) spawns ps with a timeout. The dock calls this from a
// tea.Cmd, never from Update.

const (
	// foregroundProcessTimeout bounds the ps fallback (the ioctl and the /proc read
	// are not interruptible but are local and tiny).
	foregroundProcessTimeout = 500 * time.Millisecond
	// maxForegroundProcessBytes caps one process-name read.
	maxForegroundProcessBytes = 256
)

// foregroundProcessName reports the foreground process group's program name for a
// local Unix PTY. It returns false when the PTY is closed, the ioctl fails, or no
// name can be read.
func (p *unixPTY) foregroundProcessName() (string, bool) {
	p.fdMu.RLock()
	if p.closed {
		p.fdMu.RUnlock()
		return "", false
	}
	// The ioctl reads the kernel's foreground process group of the master. Keep the
	// read lock only for this call: Close must not race a reused fd, but the /proc
	// or ps lookup below uses the pgid alone.
	// SyscallConn, not Fd: Fd would put the master in blocking mode, and Close could
	// then no longer interrupt the output reader.
	pgid, err := -1, error(nil)
	if rc, rcErr := p.ptmx.SyscallConn(); rcErr != nil {
		err = rcErr
	} else if cerr := rc.Control(func(fd uintptr) {
		pgid, err = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP)
	}); cerr != nil {
		err = cerr
	}
	p.fdMu.RUnlock()
	if err != nil || pgid <= 0 {
		return "", false
	}
	name := sanitizeForegroundProcessName(foregroundNameOf(pgid))
	if name == "" {
		return "", false
	}
	return name, true
}

// foregroundNameOf resolves a process group id to a program name: /proc on Linux,
// a bounded ps elsewhere (and as a Linux fallback when /proc is unavailable).
func foregroundNameOf(pgid int) string {
	if name := readProcComm(pgid); name != "" {
		return name
	}
	return psComm(pgid)
}

// readProcComm reads /proc/<pgid>/comm, bounded and without spawning anything.
func readProcComm(pgid int) string {
	f, err := os.Open("/proc/" + strconv.Itoa(pgid) + "/comm")
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxForegroundProcessBytes))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// cappedWriter keeps the newest max bytes and always reports a full write, so a
// child writing more than that cannot grow the buffer or fail the command.
type cappedWriter struct {
	buf bytes.Buffer
	max int
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	n := len(p)
	if rem := w.max - w.buf.Len(); rem > 0 {
		if len(p) > rem {
			p = p[:rem]
		}
		_, _ = w.buf.Write(p)
	}
	return n, nil
}

// trustedPSCandidates are the only absolute system locations the ps fallback may
// execute, in priority order. The dock's ps fallback is an OS introspection of a
// local process group, so it must never resolve "ps" through PATH: a project
// environment or a malicious agent can add a directory to PATH, and a PATH-resolved
// ps would then run attacker-controlled code on every foreground poll. Only these
// fixed, root-owned system locations are considered; a relative or custom path is
// never used.
var trustedPSCandidates = []string{"/bin/ps", "/usr/bin/ps", "/sbin/ps", "/usr/sbin/ps"}

// resolvePSPath returns the first trusted absolute ps binary that is an existing,
// executable regular file, or "" when none is available. stat is injectable for
// tests; production always passes os.Stat. It never consults PATH, exec.LookPath
// or the process environment.
func resolvePSPath(stat func(string) (os.FileInfo, error)) string {
	for _, path := range trustedPSCandidates {
		info, err := stat(path)
		if err != nil || info == nil || info.IsDir() {
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		return path
	}
	return ""
}

// psComm asks the trusted ps for the process group leader's command name. The
// process id is an integer we read from the kernel, never user input; the command
// is bounded by ctx and the capped stdout, no shell is involved, and the binary is
// a fixed absolute system path chosen by resolvePSPath (never a PATH lookup).
func psComm(pgid int) string {
	psPath := resolvePSPath(os.Stat)
	if psPath == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), foregroundProcessTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, psPath, "-o", "comm=", "-p", strconv.Itoa(pgid))
	out := &cappedWriter{max: maxForegroundProcessBytes}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		return ""
	}
	line := strings.TrimSpace(out.buf.String())
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if line == "" {
		return ""
	}
	// ps -o comm= may print the full path (macOS); the tab title wants the program
	// name, like Fresh's /proc/<pid>/comm.
	return filepath.Base(line)
}

// sanitizeForegroundProcessName cleans an untrusted process name before it can
// reach a tab label or the i18n chrome: Cc controls, Cf format characters (bidi/
// zero-width spoofing) and invalid UTF-8 are dropped, outer space is trimmed and the
// name is bounded to 64 runes; combining marks are kept as readable text.
func sanitizeForegroundProcessName(name string) string {
	name = cleanLabelText(name)
	name = strings.TrimSpace(name)
	const maxRunes = 64
	if runes := []rune(name); len(runes) > maxRunes {
		name = strings.TrimSpace(string(runes[:maxRunes]))
	}
	return name
}
