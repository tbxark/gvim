package config

import (
	"encoding/json"
	"fmt"
)

// Native defaults and bounds for the Contents (Markdown outline) sidebar section.
const (
	// DefaultContentsRows is the rows default for the Contents section.
	DefaultContentsRows = 10
	// MinContentsRows and MaxContentsRows bound contents.rows (1..200).
	MinContentsRows = 1
	MaxContentsRows = 200
	// DefaultContentsMaxDepth is the default deepest heading level listed (1..6).
	DefaultContentsMaxDepth = 6
)

// The allowed contents.follow values.
const (
	ContentsFollowCursor = "cursor"
	ContentsFollowScroll = "scroll"
)

// ContentsConfig is the contents section: settings for the native Contents
// (Markdown outline) sidebar section. Only the user layer is read: a repository
// cannot decide how the user reads an outline.
type ContentsConfig struct {
	// Enabled is contents.enabled: whether the Contents section is active.
	// Default true.
	Enabled *bool
	// FoldBuffer is contents.fold_buffer: folding a heading in the Contents
	// section also folds its section in the buffer, and unfolding it opens the
	// fold. Default false.
	FoldBuffer *bool
	// Follow is contents.follow: "cursor" or "scroll". Default "cursor".
	Follow *string
	// AutoOpen is contents.auto_open: show the section when a Markdown buffer
	// becomes active and hide it otherwise. Default true.
	AutoOpen *bool
	// Rows is contents.rows: body rows the section asks for (1..200). Default
	// DefaultContentsRows.
	Rows *int
	// MaxDepth is contents.max_depth: deepest heading level listed (1..6).
	// Default DefaultContentsMaxDepth.
	MaxDepth *int
}

// setContents applies the contents object of one config layer. Only the user
// layer is read: a project file's contents object is accepted and ignored.
func (c *Config) setContents(raw json.RawMessage, l layer, path string) (diags []Diagnostic) {
	if l == layerProject {
		return nil
	}
	diag := func(key, message string) {
		diags = append(diags, Diagnostic{Path: path, Key: "contents." + key, Message: message})
	}
	entries, err := decodeObject(raw)
	if err != nil {
		return []Diagnostic{{Path: path, Key: "contents", Message: err.Error()}}
	}
	for _, k := range sortedKeys(entries) {
		v := entries[k]
		if isNull(v) {
			diag(k, "null is not a valid value")
			continue
		}
		switch k {
		case "enabled":
			var b bool
			if err := json.Unmarshal(v, &b); err != nil {
				diag(k, "must be true or false")
				continue
			}
			c.Contents.Enabled = &b
		case "fold_buffer":
			var b bool
			if err := json.Unmarshal(v, &b); err != nil {
				diag(k, "must be true or false")
				continue
			}
			c.Contents.FoldBuffer = &b
		case "follow":
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				diag(k, "must be "+quoteChoice(ContentsFollowCursor)+" or "+quoteChoice(ContentsFollowScroll))
				continue
			}
			if s != ContentsFollowCursor && s != ContentsFollowScroll {
				diag(k, "must be "+quoteChoice(ContentsFollowCursor)+" or "+quoteChoice(ContentsFollowScroll))
				continue
			}
			c.Contents.Follow = &s
		case "auto_open":
			var b bool
			if err := json.Unmarshal(v, &b); err != nil {
				diag(k, "must be true or false")
				continue
			}
			c.Contents.AutoOpen = &b
		case "rows":
			var n int
			if err := json.Unmarshal(v, &n); err != nil {
				diag(k, "must be an integer")
				continue
			}
			if n < MinContentsRows || n > MaxContentsRows {
				diag(k, fmt.Sprintf("must be between %d and %d, got %d", MinContentsRows, MaxContentsRows, n))
				continue
			}
			c.Contents.Rows = &n
		case "max_depth":
			var n int
			if err := json.Unmarshal(v, &n); err != nil {
				diag(k, "must be an integer")
				continue
			}
			if n < 1 || n > DefaultContentsMaxDepth {
				diag(k, fmt.Sprintf("must be between 1 and %d, got %d", DefaultContentsMaxDepth, n))
				continue
			}
			c.Contents.MaxDepth = &n
		default:
			diag(k, "unknown setting")
		}
	}
	return diags
}

// quoteChoice renders one follow choice for a diagnostic.
func quoteChoice(s string) string { return fmt.Sprintf("%q", s) }

// ContentsEnabled returns whether the native Contents section is enabled (default true).
func (c Config) ContentsEnabled() bool {
	if c.Contents.Enabled != nil {
		return *c.Contents.Enabled
	}
	return true
}

// ContentsFoldBuffer returns the effective contents.fold_buffer (default false).
func (c Config) ContentsFoldBuffer() bool {
	if c.Contents.FoldBuffer != nil {
		return *c.Contents.FoldBuffer
	}
	return false
}

// ContentsFollow returns the effective contents.follow (default "cursor").
func (c Config) ContentsFollow() string {
	if c.Contents.Follow != nil {
		return *c.Contents.Follow
	}
	return ContentsFollowCursor
}

// ContentsAutoOpen returns the effective contents.auto_open (default true).
func (c Config) ContentsAutoOpen() bool {
	if c.Contents.AutoOpen != nil {
		return *c.Contents.AutoOpen
	}
	return true
}

// ContentsRows returns the effective contents.rows (default DefaultContentsRows).
func (c Config) ContentsRows() int {
	if c.Contents.Rows != nil {
		return clampContentsRows(*c.Contents.Rows)
	}
	return DefaultContentsRows
}

// ContentsMaxDepth returns the effective contents.max_depth (default DefaultContentsMaxDepth).
func (c Config) ContentsMaxDepth() int {
	if c.Contents.MaxDepth != nil {
		return min(max(*c.Contents.MaxDepth, 1), DefaultContentsMaxDepth)
	}
	return DefaultContentsMaxDepth
}

// clampContentsRows bounds a rows value to the section's accepted range.
func clampContentsRows(n int) int {
	if n < MinContentsRows {
		return MinContentsRows
	}
	if n > MaxContentsRows {
		return MaxContentsRows
	}
	return n
}
