// Package highlight caches the syntax spans of open documents for the TUI: it
// lexes only the window a frame needs, keeps a generation per document, and
// answers whether a position is inside a string. It is a host package used by
// internal/app and internal/uiadapter.
package highlight
