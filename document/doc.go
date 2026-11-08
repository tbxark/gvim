// Package document is the model of one open file in the editor: its text, the
// cursor and selections, the view settings, folds, the saved and on-disk state,
// its encoding and line ending, large-file mode, and the document-level editing
// primitives and load/save planning.
//
// A Document is a value in memory; it does no IO by itself, except the reads and
// writes that load and save it. The session around it — the tabs, the action
// registry, the save and autosave jobs, the shell and the plugin seam — is
// package editor, which owns document. The package must stay free of the TUI and
// of the session: it may import buffer, search, textlayout, fold, encoding, diff
// and atomicfile, and no internal/ package.
package document
