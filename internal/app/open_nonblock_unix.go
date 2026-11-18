//go:build !windows

package app

import (
	"errors"
	"os"
	"syscall"
)

// openNoBlock prevents os.OpenFile from blocking on a FIFO or special device.
const openNoBlock = syscall.O_NONBLOCK

// openRegularFile opens path non-blockingly and verifies it refers to a regular file.
func openRegularFile(path string) (*os.File, os.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|openNoBlock, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, errors.New("not a regular file")
	}
	return f, info, nil
}
