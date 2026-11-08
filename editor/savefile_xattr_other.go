//go:build unix && !linux && !darwin

package editor

import "os"

// copyXattrs does nothing on this system.
func copyXattrs(string, *os.File) {}
