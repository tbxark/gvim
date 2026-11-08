//go:build linux || darwin

package atomicfile

import (
	"bytes"
	"os"

	"golang.org/x/sys/unix"
)

// copyXattrs copies the extended attributes of the file at from to f. Some of them
// cannot be set by the user (security labels, for one), and a save does not fail
// for that: what cannot be copied is skipped.
func copyXattrs(from string, f *os.File) {
	size, err := unix.Listxattr(from, nil)
	if err != nil || size <= 0 {
		return
	}
	list := make([]byte, size)
	if size, err = unix.Listxattr(from, list); err != nil {
		return
	}
	for name := range bytes.SplitSeq(list[:size], []byte{0}) {
		if len(name) == 0 {
			continue
		}
		attr := string(name)
		n, err := unix.Getxattr(from, attr, nil)
		if err != nil {
			continue
		}
		value := make([]byte, n)
		if n, err = unix.Getxattr(from, attr, value); err != nil {
			continue
		}
		_ = unix.Fsetxattr(int(f.Fd()), attr, value[:n], 0)
	}
}
