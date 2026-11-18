//go:build !unix

package atomicfile

import "os"

// inheritFile has nothing to keep on this system beyond the file's mode.
func inheritFile(f *os.File, _ string, old os.FileInfo) string {
	if old != nil {
		_ = f.Chmod(old.Mode().Perm())
	}
	return ""
}

// umask is zero where the system has no file-creation mask.
const umask = 0

// syncDir has nothing to flush where a directory cannot be synced.
func syncDir(string) {}
