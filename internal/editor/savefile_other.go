//go:build !unix

package editor

import "os"

// inheritFile has nothing to keep on this system.
func inheritFile(*os.File, string, os.FileInfo) string { return "" }
