package config

import (
	"encoding/json"
	"errors"
)

// ClipboardConfig is the clipboard section of the config. Fresh's clipboard keys
// tune the terminal's clipboard integration; gvim implements use_osc52 (child
// writes) and the gvim extension osc52_paste (child reads, opt-in and bounded).
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

// DefaultClipboard returns the clipboard defaults: OSC 52 writes and reads are
// off. Fresh
// defaults use_osc52 to true; gvim keeps it off because the program inside the
// terminal is untrusted output, and the host additionally requires a trusted
// workspace when it is turned on (docs/terminal.md).
func DefaultClipboard() ClipboardConfig { return ClipboardConfig{} }

// setClipboard applies one key of "clipboard". The other Fresh clipboard keys
// (osc52_paste, ...) are accepted and ignored: gvim's clipboard writes go through
// the editor's own copy path.
func (c *Config) setClipboard(key string, raw json.RawMessage) error {
	switch key {
	case "use_osc52":
		return setClipboardBool(raw, &c.Clipboard.UseOSC52)
	case "osc52_paste":
		return setClipboardBool(raw, &c.Clipboard.OSC52Paste)
	case "use_system_clipboard":
		// gvim always writes through the system clipboard tools when a copy asks
		// for it; there is no separate switch to turn that half off.
		return nil
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
