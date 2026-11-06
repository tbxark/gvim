//go:build !windows

package buffer

import "os"

func openFile(path string) (*os.File, error) { return os.Open(path) }
