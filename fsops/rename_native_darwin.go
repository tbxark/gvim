package fsops

import "golang.org/x/sys/unix"

// renameNative renames src to dst in one step, failing when dst exists.
func renameNative(src, dst string) error {
	return unix.RenamexNp(src, dst, unix.RENAME_EXCL)
}
