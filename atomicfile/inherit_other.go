//go:build !unix

package atomicfile

import "os"

// inheritFile has nothing to keep on this system.
func inheritFile(*os.File, string, os.FileInfo) string { return "" }

// umask is zero where the system has no file-creation mask.
const umask = 0

// syncDir has nothing to flush where a directory cannot be synced.
func syncDir(string) {}
