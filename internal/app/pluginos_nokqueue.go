//go:build !(darwin || freebsd || netbsd || openbsd || dragonfly)

package app

// kqueueDirLimit is zero where watching a directory costs one watch descriptor (inotify,
// ReadDirectoryChangesW): there is no limit on its size.
const kqueueDirLimit = 0
