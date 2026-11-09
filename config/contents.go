package config

import (
	"encoding/json"
	"fmt"
)

// The Contents section's native defaults and bounds. Fresh's markdown_toc plugin
// defines the same defaults under plugins.markdown_toc.settings; gvim's native
// contents.* keys are user-layer only and win when both are set.
const (
	// DefaultContentsRows is the rows default for the Contents section (Fresh's
	// markdown_toc rows default).
	DefaultContentsRows = 10
	// MinContentsRows and MaxContentsRows bound contents.rows (Fresh's 1..200).
	MinContentsRows = 1
	MaxContentsRows = 200
)

// The allowed contents.follow values.
const (
	ContentsFollowCursor = "cursor"
	ContentsFollowScroll = "scroll"
)

// ContentsConfig is the contents section: settings for the native Contents
// (Markdown outline) sidebar section. Fresh ships the section as the bundled
// markdown_toc plugin; its config path plugins.markdown_toc.settings is still read
// for compatibility (see the effective getters), and these are gvim's native keys
// for the same behavior now that the section is built into the TUI. Only the user
// layer is read: a repository cannot decide how the user reads an outline.
type ContentsConfig struct {
	// FoldBuffer is contents.fold_buffer: folding a heading in the Contents
	// section also folds its section in the buffer, and unfolding it opens the
	// fold. A nil value defers to the retired plugin's
	// plugins.markdown_toc.settings.fold_buffer, and then Fresh's default, false.
	FoldBuffer *bool
	// Follow is contents.follow: "cursor" or "scroll". A nil value defers to the
	// plugin's follow setting, and then "cursor".
	Follow *string
	// AutoOpen is contents.auto_open: show the section when a Markdown buffer
	// becomes active and hide it otherwise. A nil value defers to the plugin's
	// auto_open setting, and then true.
	AutoOpen *bool
	// Rows is contents.rows: body rows the section asks for (1..200). A nil value
	// defers to the plugin's rows setting, and then DefaultContentsRows.
	Rows *int
}

// setContents applies the contents object of one config layer. The section is the
// retired markdown_toc plugin's, so like a plugin's settings only the user layer is
// read: a project file's contents object is accepted and ignored, and a repository
// cannot decide how the user reads an outline.
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
		default:
			diag(k, "unknown setting")
		}
	}
	return diags
}

// quoteChoice renders one follow choice for a diagnostic.
func quoteChoice(s string) string { return fmt.Sprintf("%q", s) }

// ContentsEnabled returns whether the native Contents section is enabled. Only the
// USER layer may turn it off: markdown_toc is a retired bundled plugin whose section
// is now built into the TUI, and a project file's plugins.markdown_toc.enabled=false
// must not disable the reader's outline (docs/sidebar.md: user layer only). A missing
// user setting means enabled, as the bundled plugin shipped.
func (c Config) ContentsEnabled() bool {
	if v, ok := c.pluginUserEnabled["markdown_toc"]; ok {
		return v
	}
	return true
}

// ContentsFoldBuffer returns the effective contents.fold_buffer: the native key
// when the user file sets it, otherwise the retired bundled plugin's
// plugins.markdown_toc.settings fold_buffer/foldBuffer (Fresh's own config path, kept
// so a Fresh config works), and false when neither sets it.
func (c Config) ContentsFoldBuffer() bool {
	if c.Contents.FoldBuffer != nil {
		return *c.Contents.FoldBuffer
	}
	if v, ok := pluginSetting(c, "markdown_toc", "fold_buffer", "foldBuffer"); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// ContentsFollow returns the effective contents.follow: the native key when set,
// otherwise the plugin's follow setting, and then Fresh's default "cursor".
func (c Config) ContentsFollow() string {
	if c.Contents.Follow != nil {
		return *c.Contents.Follow
	}
	if v, ok := pluginSetting(c, "markdown_toc", "follow"); ok {
		if s, ok := v.(string); ok && (s == ContentsFollowCursor || s == ContentsFollowScroll) {
			return s
		}
	}
	return ContentsFollowCursor
}

// ContentsAutoOpen returns the effective contents.auto_open: the native key when
// set, otherwise the plugin's auto_open/autoOpen setting, and then true.
func (c Config) ContentsAutoOpen() bool {
	if c.Contents.AutoOpen != nil {
		return *c.Contents.AutoOpen
	}
	if v, ok := pluginSetting(c, "markdown_toc", "auto_open", "autoOpen"); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return true
}

// ContentsRows returns the effective contents.rows: the native key when set,
// otherwise the plugin's rows setting, and then DefaultContentsRows, clamped to
// 1..MaxContentsRows for a hand-edited/legacy plugin value.
func (c Config) ContentsRows() int {
	if c.Contents.Rows != nil {
		return clampContentsRows(*c.Contents.Rows)
	}
	if v, ok := pluginSetting(c, "markdown_toc", "rows"); ok {
		if n, ok := pluginWhole(v); ok {
			return clampContentsRows(n)
		}
	}
	return DefaultContentsRows
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

// pluginSetting reads one setting from the retired plugin's config object, trying the
// given key spellings (gvim's historical snake_case and Fresh's camelCase) in order.
func pluginSetting(c Config, plugin string, keys ...string) (any, bool) {
	pc, ok := c.Plugins[plugin]
	if !ok || pc.Settings == nil {
		return nil, false
	}
	for _, key := range keys {
		if v, ok := pc.Settings[key]; ok {
			return v, true
		}
	}
	return nil, false
}

// pluginWhole reads an integer from a decoded plugin setting.
func pluginWhole(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}
