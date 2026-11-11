package fsops

import "golang.org/x/sys/windows"

// renameNative renames src to dst in one step. Without MOVEFILE_REPLACE_EXISTING,
// MoveFileEx fails when dst exists, and without MOVEFILE_COPY_ALLOWED it fails across
// volumes, which the caller answers with the copy.
func renameNative(src, dst string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, 0)
}

// errnoCrossDevice is MoveFileEx's answer for a target on another volume.
const errnoCrossDevice = windows.ERROR_NOT_SAME_DEVICE
