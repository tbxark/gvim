package document

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
)

// libraryContains and libraryPrefixes are the folders of other people's code that
// open read-only: vendored dependencies, toolchains and system headers. They are
// Fresh's (is_library_path in buffer_meta.rs), in slash form, with the dotted
// rustup folder added.
var (
	libraryContains = []string{
		"/.cargo/registry/", "/.cargo/git/", "/rustup/toolchains/", "/.rustup/toolchains/",
		"/node_modules/", "/site-packages/", "/dist-packages/", "/pkg/mod/", "/gems/",
		"/.gradle/", "/.m2/", "/.nuget/", "/Xcode.app/Contents/Developer/", "/CommandLineTools/SDKs/",
	}
	libraryPrefixes = []string{
		"/usr/include/", "/usr/local/include/", "/nix/store/", "/opt/homebrew/Cellar/", "/usr/local/Cellar/",
	}
)

// libraryPath reports whether path is in a library folder.
func libraryPath(path string) bool {
	path = filepath.ToSlash(path)
	for _, p := range libraryContains {
		if strings.Contains(path, p) {
			return true
		}
	}
	for _, p := range libraryPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// unwritable reports whether the file at path exists and cannot be opened for
// writing by this process. It does not change the file.
func unwritable(path string) bool {
	return errors.Is(writeAccess(path), fs.ErrPermission)
}

// loadDocument reads path as readDocument does. With autoRO set (the
// editor.auto_read_only option) the document is read-only when the file cannot be
// written or is a library file. A library file is one whose own path and whose
// target after symbolic links are both in a library folder, so a dotfile linked to
// /nix/store stays editable. It reads the disk, so it may run in a command.
func loadDocument(path string, spec loadSpec) (*Document, error) {
	d, err := readDocument(path, spec)
	if err != nil || !spec.AutoRO || !d.Disk.Exists {
		return d, err
	}
	if target, err := filepath.EvalSymlinks(path); err == nil && libraryPath(path) && libraryPath(target) {
		d.readOnly = true
	}
	if unwritable(path) {
		d.readOnly = true
	}
	return d, nil
}

// openNote is the part of the "Opened" status that says why the document refuses
// edits, or "".
func (d *Document) openNote() string {
	switch {
	case d.binary:
		return " (binary file, read-only)"
	case d.readOnly:
		return " (read-only)"
	}
	return ""
}

// openStatus is the status row text after the document was opened: "Opened NAME" and
// the reason it refuses edits, if it does, or the notice of a large document.
func (d *Document) OpenStatus() string {
	if d.large != nil {
		return d.largeOpenStatus()
	}
	return "Opened " + d.Name() + d.openNote()
}
