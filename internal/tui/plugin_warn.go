package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/pluginapi"
)

// Typos in what plugins hand to the editor are said once, naming the bad key: a `when` that
// no condition answers, and a key bound to an action nothing runs. Both used to do nothing at all.

// knownWhen are the conditions Holds answers besides language:<id>, mode:<plugin>:<name> and the
// contexts plugins set with gvim.context.set.
var knownWhen = map[string]bool{
	"has_buffer": true, "has_text_buffer": true, "can_edit": true, "can_paste": true,
	"line_numbers": true, "line_wrap": true, "vertical_scrollbar": true, "horizontal_scrollbar": true,
	"mouse_capture": true, "mouse_hover": true, "scroll_sync": true,
	"keymap_default": true, "keymap_emacs": true, "keymap_vscode": true, "keymap_macos_gui": true,
	"can_save": true, "can_revert": true, "can_save_all": true, "read_only": true, "modified": true,
	"large": true, "formatter_available": true, "has_selection": true, "can_cut": true, "can_copy": true,
	"file_explorer": true, "file_explorer_focused": true, "file_explorer_show_hidden": true,
	"file_explorer_show_gitignored": true,
}

// plugWarnOnce shows a warning of a plugin once per key.
func (m *Model) plugWarnOnce(key, plugin, msg string) {
	ks := &m.plug.keys
	if ks.warned[key] {
		return
	}
	if ks.warned == nil {
		ks.warned = make(map[string]bool)
	}
	ks.warned[key] = true
	m.plug.host.Notify(plugin, pluginapi.LevelWarn, msg)
}

// plugCheckWhen warns about a name in a when-clause that no condition answers. A context counts
// once a plugin has set it, to either value, so a plugin declares one by setting it false at load
// time. owner is the command or action the clause belongs to.
func (m *Model) plugCheckWhen(plugin, owner string, when []string) {
	ks := &m.plug.keys
	for _, w := range when {
		name := strings.TrimPrefix(w, "!")
		if knownWhen[name] || ks.ctxSeen[name] || strings.HasPrefix(name, "language:") || strings.HasPrefix(name, "mode:") {
			continue
		}
		m.plugWarnOnce("when\x00"+name, plugin, fmt.Sprintf("%s: when '%s' is not a known condition (a typo? a plugin context counts once gvim.context.set has been called for it)", owner, name))
	}
}

// plugUnknownAction warns when a key a plugin bound ran an action that does not exist.
func (m *Model) plugUnknownAction(action string, err error) {
	if err == nil || !errors.Is(err, editor.ErrUnknownAction) {
		return
	}
	for plugin, bs := range m.plug.keys.bindings {
		for _, b := range bs {
			if b.Action == action {
				m.plugWarnOnce("action\x00"+action, plugin, fmt.Sprintf("key %s is bound to '%s', which is not an action or a command", b.Chord, action))
				return
			}
		}
	}
	for plugin, modes := range m.plug.keys.modes {
		for _, um := range modes {
			for chord, a := range um.info.Bindings {
				if a == action {
					m.plugWarnOnce("action\x00"+action, plugin, fmt.Sprintf("key %s of mode '%s' is bound to '%s', which is not an action or a command", chord, um.info.Name, action))
					return
				}
			}
		}
	}
}
