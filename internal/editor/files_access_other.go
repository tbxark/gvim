//go:build !unix

package editor

import "os"

// writeAccess returns an error that wraps fs.ErrPermission when this process may not
// write the file at path, found by opening it for writing, which does not change it.
func writeAccess(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return f.Close()
}
