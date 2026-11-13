package fsops

// RenameNoReplace renames oldPath to newPath without replacing an existing target. A
// rename to a name that differs only in case (the same file on a case-insensitive
// filesystem) is allowed. A failure leaves the source and reports it.
func RenameNoReplace(oldPath, newPath string) error {
	_, err := RenameNoReplaceResult(oldPath, newPath)
	return err
}

// MoveNoReplace moves src into dstDir keeping its name, without replacing an existing
// destination. A move within the source's own directory is a no-op. It renames, or
// copies and removes, as RenameNoReplace does.
func MoveNoReplace(src, dstDir string) error {
	_, err := MoveNoReplaceResult(src, dstDir)
	return err
}
