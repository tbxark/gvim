package config

import (
	"encoding/json"
)

// ContentsConfig is the contents section: settings for the native Contents
// (Markdown outline) sidebar section. Fresh ships the section as the bundled
// markdown_toc plugin; its config path plugins.markdown_toc.settings is still read
// for compatibility (see ContentsFoldBuffer), and these are gvim's native keys for
// the same behavior now that the section is built into the TUI.
type ContentsConfig struct {
	// FoldBuffer is contents.fold_buffer: folding a heading in the Contents
	// section also folds its section in the buffer, and unfolding it opens the
	// fold. A nil value defers to the retired plugin's
	// plugins.markdown_toc.settings.fold_buffer, and then Fresh's default, false.
	FoldBuffer *bool
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
		default:
			diag(k, "unknown setting")
		}
	}
	return diags
}

// ContentsFoldBuffer returns the effective contents.fold_buffer: the native key
// when the user file sets it, otherwise the retired bundled plugin's
// plugins.markdown_toc.settings.fold_buffer (Fresh's own config path, kept so a
// Fresh config works), and false when neither sets it.
func (c Config) ContentsFoldBuffer() bool {
	if c.Contents.FoldBuffer != nil {
		return *c.Contents.FoldBuffer
	}
	if pc, ok := c.Plugins["markdown_toc"]; ok {
		if v, ok := pc.Settings["fold_buffer"].(bool); ok {
			return v
		}
	}
	return false
}
