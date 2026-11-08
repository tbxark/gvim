//go:build !unix

package atomicfile

import "os"

// inheritFile has nothing to keep on this system.
func inheritFile(*os.File, string, os.FileInfo) string { return "" }
