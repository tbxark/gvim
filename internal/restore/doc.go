// Package restore saves and restores the editor session and the crash-recovery
// copies: the session file, its stash files for unsaved edits, and the recovery
// directory. It is the session policy of the executable; the engine package
// session owns the file format.
package restore
