//go:build unix && !linux && !darwin

package atomicfile

import "os"

// copyXattrs does nothing on this system.
func copyXattrs(string, *os.File) {}
