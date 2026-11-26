package config

import (
	"encoding/json"
	"errors"
)

// ClipboardConfig is the clipboard section of the config. It is user-layer
// only: a project .gvim/config.json cannot enable terminal clipboard access.
type ClipboardConfig struct {
	// UseOSC52 lets a program inside the terminal dock write the system clipboard
	// with OSC 52 (base64 text). It is off by default: output is untrusted data,
	// and the feature is additionally gated on a trusted workspace when it is on.
	UseOSC52 bool

	// OSC52Paste lets a program inside the terminal dock read the system
	// clipboard with an OSC 52 read request (ESC ] 52 ; c ; ? BEL). It is off by
	// default: a child reading the clipboard pastes host data into untrusted
	// output. The read is bounded (64 KiB), only for the clipboard selection, and
	// needs a trusted workspace when on.
	OSC52Paste bool
}

// DefaultClipboard returns the clipboard defaults: OSC 52 writes and reads are off.
func DefaultClipboard() ClipboardConfig { return ClipboardConfig{} }

// setClipboard applies one key of "clipboard".
func (c *Config) setClipboard(key string, raw json.RawMessage) error {
	switch key {
	case "use_osc52":
		return setClipboardBool(raw, &c.Clipboard.UseOSC52)
	case "osc52_paste":
		return setClipboardBool(raw, &c.Clipboard.OSC52Paste)
	}
	return errors.New("unknown setting")
}

// setClipboardBool decodes a boolean setting of the clipboard section.
func setClipboardBool(raw json.RawMessage, field *bool) error {
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return errors.New("must be true or false")
	}
	*field = v
	return nil
}
