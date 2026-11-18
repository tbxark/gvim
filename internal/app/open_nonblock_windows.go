//go:build windows

package app

import (
	"errors"
	"os"
)

const openNoBlock = 0

func openRegularFile(path string) (*os.File, os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return f, info, nil
}
