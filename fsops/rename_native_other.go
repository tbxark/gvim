//go:build !linux && !darwin && !windows

package fsops

// renameNative has no no-replace rename primitive here; the caller copies instead.
func renameNative(src, dst string) error { return errNoNativeRename }
