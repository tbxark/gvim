package app

// Remote workspace session restore helpers: the saved files of a daemon-hosted remote
// workspace live on the remote host, so their existence is probed through the authority
// (bounded, no local stat) before the editor opens them through the installed remote file
// access.

import (
	"context"
	"time"

	"github.com/tbxark/gvim/authority"
)

// remoteRestoreTimeout bounds one existence probe during a remote session restore.
const remoteRestoreTimeout = 5 * time.Second

// remoteRestoreExists returns the existence probe restore.RestoreSessionAccess uses for a
// remote workspace. A stat error (missing path, dropped carrier) reports the path missing:
// a clean file is then skipped, while a file with unsaved text is still opened (and fails
// with its stash kept and a notice when the carrier is down). It never touches the local
// file system.
func remoteRestoreExists(slot authority.Slot) func(path string) bool {
	if slot == nil {
		return func(string) bool { return false }
	}
	return func(path string) bool {
		files := slot.Files()
		if files == nil {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), remoteRestoreTimeout)
		defer cancel()
		_, err := files.Stat(ctx, path)
		return err == nil
	}
}
