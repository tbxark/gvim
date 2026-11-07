package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// macroState is the macro recorder. recording is set between a start and a stop,
// and cmds holds the steps recorded so far. last is the macro the play action
// replays. playing is set while a playback runs, so its steps are not recorded
// again, and a play that a step starts is ignored.
type macroState struct {
	recording bool
	playing   bool
	cmds      []macroStep
	last      []macroStep
}

// macroCtx is the focus a command was issued in: whether an overlay is on top, and
// which of the explorer and the tab strip has the keyboard.
type macroCtx struct {
	overlay  bool
	explorer bool
	tabs     bool
}

// macroStep is one recorded command and the focus it was issued in.
type macroStep struct {
	cmd Command
	ctx macroCtx
}

// canRun reports whether the step can run with the focus now. Nothing runs under an
// overlay, which takes every key. The explorer keeps the keys it binds (file_explorer_*,
// focus_editor and the clipboard) and the application-wide actions, and it takes the
// rest as search text. A copy, cut or paste means a file in the explorer and text in
// the editor, so it runs only on the side it was recorded on. The tab strip takes typed
// text, so insert_char does not run while it has focus.
func (s macroStep) canRun(now macroCtx) bool {
	a := s.cmd.Action
	switch {
	case now.overlay:
		return false
	case strings.HasPrefix(a, "file_explorer_"):
		return now.explorer
	case clipboardActions[a]:
		return s.ctx.explorer == now.explorer
	case a == "focus_editor" || explorerFallthrough[a]:
		return true
	case now.explorer:
		return false
	case now.tabs && a == "insert_char":
		return false
	}
	return true
}

// macroControl are the macro actions themselves. They are never recorded, so a
// playback does not replay its own start, stop or play.
var macroControl = map[string]bool{
	"prompt_record_macro":  true,
	"stop_macro_recording": true,
	"play_last_macro":      true,
	// They act on the macro that was recorded, so a recording that contained one would
	// open the name prompt again when played, and save itself into init.lua.
	"prompt_save_macro_to_init": true,
	"prompt_promote_macro":      true,
}

// macroAction runs one of the macro actions: prompt_record_macro starts a recording,
// or stops it when one is on, stop_macro_recording stops it, and play_last_macro
// replays the last macro. It returns the command the replay started, if any. The
// recorder has one slot, so there is no register prompt.
func (m *Model) macroAction(action string) tea.Cmd {
	switch action {
	case "prompt_record_macro":
		if m.root.macro.recording {
			m.stopMacro()
			return nil
		}
		m.root.macro.recording = true
		m.root.macro.cmds = nil
		m.notice = "Recording macro"
	case "stop_macro_recording":
		m.stopMacro()
	case "play_last_macro":
		return m.playMacro()
	}
	return nil
}

// stopMacro ends the recording and keeps its commands as the last macro. An empty
// recording keeps the previous macro. With no recording on it says so and changes
// nothing.
func (m *Model) stopMacro() {
	mc := &m.root.macro
	if !mc.recording {
		m.notice = "Not recording a macro"
		return
	}
	mc.recording = false
	recorded := mc.cmds
	mc.cmds = nil
	if len(recorded) == 0 {
		// A recording with no commands keeps the macro that was there before, so a
		// start and stop by mistake does not lose it.
		m.notice = "Macro is empty; the last macro is kept"
		return
	}
	mc.last = recorded
	m.notice = fmt.Sprintf("Macro recorded: %d actions", len(mc.last))
}

// playMacro runs the last macro through dispatch, so each command does what it does
// when typed. Each step is checked against the focus at its turn, so a step that the
// focus cannot run is skipped, and a notice counts the skipped steps. A play that a
// step starts is ignored, so a replay never runs itself. It returns the commands the
// replays started. With no macro it says so.
func (m *Model) playMacro() tea.Cmd {
	mc := &m.root.macro
	if mc.playing {
		return nil
	}
	if len(mc.last) == 0 {
		m.notice = "No macro recorded"
		return nil
	}
	mc.playing = true
	defer func() { mc.playing = false }()
	steps := slices.Clone(mc.last)
	var cmds []tea.Cmd
	skipped := 0
	for _, s := range steps {
		if !s.canRun(m.macroContext()) {
			skipped++
			continue
		}
		cmds = append(cmds, m.dispatch(s.cmd))
	}
	if skipped > 0 {
		m.notice = fmt.Sprintf("Macro skipped %d of %d actions that cannot run here", skipped, len(steps))
	}
	return tea.Batch(cmds...)
}

// macroContext is the focus now: whether an overlay is on top, and which of the
// explorer and the tab strip has the keyboard.
func (m *Model) macroContext() macroCtx {
	return macroCtx{overlay: len(m.overlays) > 0, explorer: m.explorerFocus, tabs: m.tabFocus}
}

// recordCommand adds cmd to the recording, with the focus it is issued in, when a
// recording is on, a playback is not running and the command is not a macro action. A
// command a plugin runs through command.run is not recorded (only the plugin command the
// user issued is), and neither is a plugin command that asked not to be.
func (m *Model) recordCommand(cmd Command) {
	mc := &m.root.macro
	if !mc.recording || mc.playing || macroControl[cmd.Action] || m.plugNoRecord(cmd.Action) {
		return
	}
	mc.cmds = append(mc.cmds, macroStep{cmd: cmd, ctx: m.macroContext()})
}
