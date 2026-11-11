package fsops

import "golang.org/x/sys/unix"

// renameNative renames src to dst in one step, failing when dst exists.
func renameNative(src, dst string) error {
	return unix.Renameat2(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_NOREPLACE)
}
