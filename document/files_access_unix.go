//go:build unix

package document

import (
	"golang.org/x/sys/unix"
)

// writeAccess returns an error that wraps fs.ErrPermission when this process may not
// write the file at path. It asks the system and does not open the file, because an
// open for writing can block: on a FIFO without a reader, on a hung mount.
func writeAccess(path string) error {
	return unix.Access(path, unix.W_OK)
}
