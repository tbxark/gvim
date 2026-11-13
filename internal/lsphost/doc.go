// Package lsphost is the language-server host of the app: it implements the TUI's
// LSPSession over the multi-server manager. It builds the effective server table
// from the built-in catalogue and the config, keeps one manager per workspace
// root, converts the protocol's UTF-16 positions to the editor's rune positions,
// polls diagnostics, queues server-initiated workspace edits for the root to apply,
// and lists the configured servers for the status popup. It is a host package used
// by internal/app.
package lsphost
