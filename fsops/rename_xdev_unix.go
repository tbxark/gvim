//go:build !windows

package fsops

import "syscall"

// errnoCrossDevice is rename's answer for a target on another device.
const errnoCrossDevice = syscall.EXDEV
