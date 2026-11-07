package ui

import (
	"cmp"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
	palmodel "github.com/tbxark/gvim/internal/palette"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// The sources a command can start from, as the root names them. They are the values of
// the plugin command's ctx.source.
const (
	srcPalette = pluginapi.SrcPalette
	srcKey     = pluginapi.SrcKey
	srcMenu    = pluginapi.SrcMenu
	srcAPI     = pluginapi.SrcAPI
)

// maxDoCount bounds the repeat count of Host.Do, so a plugin cannot hold the update
// loop with one call.
const maxDoCount = 100000

// maxDoTime bounds the wall time of one Host.Do or DoAll. The count alone does not: an action
// costs from microseconds to milliseconds a repetition, and a repetition that opens a tab or
// adds a cursor costs more with each one before it. The editor cannot be interrupted inside
// a repetition, so the clock is read between them; the first repetition always runs.
const maxDoTime = 250 * time.Millisecond

// doDeadline is when the Do or DoAll that starts now has to stop repeating.
func (m *Model) doDeadline() time.Time {
	budget := cmp.Or(m.plug.doBudget, maxDoTime)
	return time.Now().Add(budget)
}

// reentrantActions are the actions that can reach the runtime: they end in Model.save,
// Model.quit, a macro replay (which runs plugin command ids) or a plugin operation of
// featureAction. Host.Do cannot run them inside a Lua call, because the runtime is on
// the stack, so it defers them: the pump runs them after the slice has returned. The set
// came from auditing the callers of Runtime.Run, Veto, Control and Stop, and
// TestNoActionReachesRuntimeInSlice checks that no other action reaches the runtime
// while a slice is running. The plugin command ids are reentrant too, and they are found
// through Runtime.Owns.
var reentrantActions = map[string]bool{
	"save": true, "save_as": true, "save_all": true,
	"quit": true, "quit_discard": true,
	"play_last_macro": true, "prompt_play_macro": true,
	"plugin_manager": true, "plugin_show_log": true, "plugin_reload": true, "plugin_reload_all": true,
	"load_plugin_from_buffer": true, "init_check": true, "init_edit": true, "init_reload": true,
	"prompt_save_macro_to_init": true, "prompt_promote_macro": true,
}

// uiActions are the actions uiAction handles itself, which the palette table does not
// all list. Host.Actions reports them with the editor's actions and the palette's.
var uiActions = []string{
	"toggle_file_explorer", "focus_file_explorer", "focus_editor", "new", "menu_activate", "menu_open",
	"toggle_line_numbers", "scroll_tabs_left", "scroll_tabs_right",
	"file_explorer_search_clear", "file_explorer_search_backspace",
	"open", "switch_project", "quick_open", "command_palette", "quick_open_files", "quick_open_buffers",
	"goto_line", "search", "find", "replace", "find_next", "find_previous", "close_tab", "save", "save_as",
	"save_all", "quit", "split_horizontal", "split_vertical", "next_split", "prev_split", "close_split",
	"next_pane", "prev_pane", "next_window", "prev_window", "increase_split_size", "decrease_split_size",
	"toggle_maximize_split", "switch_to_previous_tab", "compare_next_difference", "compare_prev_difference",
	"compare_switch_side",
}

// reentrantAction reports whether Host.Do must defer the action.
func (m *Model) reentrantAction(name string) bool {
	if reentrantActions[name] {
		return true
	}
	return m.plug.rt != nil && m.plug.rt.Owns(name)
}

// plugSource marks src as what started the command now running, unless an outer entry
// point already did, and returns the function that clears the mark. The palette runs
// through Run, so the palette is the outer one there.
func (m *Model) plugSource(src pluginapi.CommandSource) func() {
	if m.plug.src != "" {
		return func() {}
	}
	m.plug.src = src
	return func() { m.plug.src = "" }
}

// pluginAction runs the command when a plugin owns the action. It is the first thing
// uiAction does, so the key, menu and palette paths all reach it, and so does a macro
// replay. It reports whether a plugin took the action, and the command that results.
// A command whose when-clause fails does nothing and says so.
func (m *Model) pluginAction(action string, args map[string]any) (tea.Cmd, bool) {
	p := &m.plug
	if p.rt == nil || p.direct > 0 || !p.rt.Owns(action) {
		return nil, false
	}
	if info, ok := p.info(action); ok && !m.whenHolds(info.When) {
		m.notice = cmdTitle(info) + " is not available now"
		return nil, true
	}
	src := p.src
	if src == "" {
		src = srcKey
	}
	if p.rt.InSlice() {
		// No action that is not reentrant gets here. A call that does anyway must not
		// run Lua inside Lua, so it waits for the pump.
		p.later = append(p.later, func() tea.Cmd { return m.pluginRun(action, args, src) })
		return nil, true
	}
	return m.pluginRun(action, args, src), true
}

// pluginRun starts a plugin command. The runtime runs its first slice at once, so a
// macro replay keeps the order of its steps, and the pump handles what the slice
// queued. A refusal is the notice.
func (m *Model) pluginRun(action string, args map[string]any, src pluginapi.CommandSource) tea.Cmd {
	var err error
	cmd := m.pumpAfter(func() { err = m.plug.rt.Run(action, args, src) })
	if err != nil {
		m.notice = action + ": " + err.Error()
		m.plugFailed(err)
	}
	return cmd
}

// plugNoRecord reports whether the macro recorder must ignore action: a plugin is
// running it for a plugin command (C8), or the command asked not to be recorded.
func (m *Model) plugNoRecord(action string) bool {
	p := &m.plug
	if p.quiet > 0 {
		return true
	}
	info, ok := p.info(action)
	return ok && !info.Record
}

// info returns the registered plugin command with id.
func (p *pluginState) info(id string) (pluginapi.CommandInfo, bool) {
	plugin, _, ok := strings.Cut(id, ".")
	if !ok {
		return pluginapi.CommandInfo{}, false
	}
	for _, c := range p.commands[plugin] {
		if c.ID == id {
			return c, true
		}
	}
	return pluginapi.CommandInfo{}, false
}

// cmdTitle is the label of a plugin command: its title, or its id without the plugin.
func cmdTitle(c pluginapi.CommandInfo) string {
	if c.Title != "" {
		return c.Title
	}
	_, name, _ := strings.Cut(c.ID, ".")
	return cmp.Or(name, c.ID)
}

// whenHolds evaluates the when-clause of a plugin command: every name must hold, and a
// name with a leading "!" must not.
func (m *Model) whenHolds(when []string) bool {
	for _, w := range when {
		name, negated := strings.CutPrefix(w, "!")
		if m.Holds(name) == negated {
			return false
		}
	}
	return true
}

// keyLabeler is the optional part of Keymap that formats the primary binding of an
// action. The app adapter implements it once plugin bindings are part of the keymap.
type keyLabeler interface {
	Label(action string) string
}

// allCommands is the palette's command table: the built-in rows, then the commands the
// plugins registered, in the order the plugins registered them. A hidden command is not
// listed, and neither is one whose when-clause fails now.
func (m *Model) allCommands() []palmodel.Entry {
	p := &m.plug
	if len(p.order) == 0 {
		return m.commands
	}
	out := slices.Clone(m.commands)
	kl, _ := m.keys.(keyLabeler)
	for _, plugin := range p.order {
		for _, c := range p.commands[plugin] {
			if c.Hidden || !m.whenHolds(c.When) {
				continue
			}
			e := palmodel.PluginEntry(c.ID, cmdTitle(c), c.Description, c.Plugin, c.When)
			if kl != nil {
				e.Keybinding = kl.Label(c.ID)
			}
			out = append(out, e)
		}
	}
	return out
}

// doForPlugin runs an editor or ui action count times on behalf of a plugin, inside a
// Lua call. It is dispatch without the macro recording and without the plugin-command
// hook (C8), because what a plugin does through command.run is not the user's command.
// It returns the command the action started; Host.Do collects it into m.pending, so
// nothing is dropped.
func (m *Model) doForPlugin(action string, args map[string]any, count int) (tea.Cmd, error) {
	return m.doAction(action, args, count, false)
}

// doAction is doForPlugin; hook says that the plugin-command hook stays on, which a
// deferred action needs, because a macro replay runs plugin commands.
func (m *Model) doAction(action string, args map[string]any, count int, hook bool) (tea.Cmd, error) {
	return m.doActionUntil(action, args, count, hook, m.doDeadline())
}

// doActionUntil is doAction that stops repeating at deadline (see maxDoTime). A timeout
// says how many repetitions ran, because they are not undone.
func (m *Model) doActionUntil(action string, args map[string]any, count int, hook bool, deadline time.Time) (tea.Cmd, error) {
	p := &m.plug
	p.quiet++
	if !hook {
		p.direct++
	}
	defer func() {
		p.quiet--
		if !hook {
			p.direct--
		}
	}()
	var cmds []tea.Cmd
	n := max(count, 1)
	for i := 1; i <= n; i++ {
		cmd, err := m.actionOnce(action, args)
		cmds = append(cmds, cmd)
		if err != nil {
			return tea.Batch(cmds...), err
		}
		if i < n && time.Now().After(deadline) {
			return tea.Batch(cmds...), pluginapi.Errf(pluginapi.CodeTimeout,
				"action %q stopped after %d of %d repetitions: over the time limit", action, i, n)
		}
	}
	return tea.Batch(cmds...), nil
}

// actionOnce runs the action once, the way dispatch does, and returns the editor's error.
func (m *Model) actionOnce(action string, args map[string]any) (tea.Cmd, error) {
	if forced, _ := args["force"].(bool); action != "close_tab" || !forced {
		if c, ok := m.uiAction(action, args); ok {
			return c, nil
		}
	}
	if c, ok := m.explorerAction(action); ok {
		return c, nil
	}
	if m.explorerFocus && clipboardActions[action] {
		return nil, nil
	}
	if c, ok := m.editorPaste(action, args); ok {
		return c, nil
	}
	res := m.editorDo(editor.Action(action), args)
	m.syncViewport()
	if res.Quit {
		return m.quit(), nil
	}
	return m.clipboardOut(action, res), plugActionErr(action, res.Err)
}

// plugActionErr maps an editor error to the error code a plugin sees.
func plugActionErr(action string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, editor.ErrUnknownAction):
		return pluginapi.Errf(pluginapi.CodeUnsupported, "action %q is not supported", action)
	case errors.Is(err, editor.ErrReadOnly):
		return pluginapi.Errf(pluginapi.CodeReadOnly, "%v", err)
	case errors.Is(err, editor.ErrLargeFile), errors.Is(err, editor.ErrIndexing):
		return pluginapi.Errf(pluginapi.CodeTooLarge, "%v", err)
	case errors.Is(err, editor.ErrUnsaved), errors.Is(err, editor.ErrChangedOnDisk):
		return pluginapi.Errf(pluginapi.CodeConflict, "%v", err)
	case errors.Is(err, editor.ErrNoDocument), errors.Is(err, editor.ErrNoTab), errors.Is(err, editor.ErrNotOpen):
		return pluginapi.Errf(pluginapi.CodeNotFound, "%v", err)
	}
	return pluginapi.Errf(pluginapi.CodeInvalid, "%v", err)
}

// ---- pluginapi.CommandHost.

// Do runs an action count times on the active document. An action that could reach the
// runtime is deferred: Do validates and queues it, and the pump runs it after the Lua
// slice has returned. The command an action starts is collected, never dropped.
func (h *pluginHost) Do(action string, args map[string]any, count int) error {
	m := h.m
	if action == "" {
		return pluginapi.Errf(pluginapi.CodeInvalid, "empty action name")
	}
	if count > maxDoCount {
		return pluginapi.Errf(pluginapi.CodeLimit, "count %d is over %d", count, maxDoCount)
	}
	args = maps.Clone(args)
	// A validator runs inside routeOverlay, which writes the overlay stack back when the
	// Update returns: an action that pushes an overlay would be lost, so every action waits.
	if m.plug.ui.busy > 0 || m.reentrantAction(action) {
		m.plug.later = append(m.plug.later, func() tea.Cmd { return m.runDeferred(action, args, count) })
		return nil
	}
	cmd, err := m.doForPlugin(action, args, count)
	m.pending = tea.Batch(m.pending, cmd)
	return err
}

// runDeferred runs an action that Do deferred. The runtime is not in a slice now, so a
// plugin command starts for real and an action that reaches Model.save finds the
// runtime free. A failure shows on the status row: the plugin that asked is gone from the
// stack.
func (m *Model) runDeferred(action string, args map[string]any, count int) tea.Cmd {
	defer m.plugSource(srcAPI)()
	if rt := m.plug.rt; rt != nil && rt.Owns(action) {
		if count > 1 {
			args = withCount(args, count)
		}
		return m.pluginRun(action, args, srcAPI)
	}
	cmd, err := m.doAction(action, args, count, true)
	if err != nil {
		m.notice = "plugin: queued action " + action + " failed: " + err.Error()
	}
	return cmd
}

// withCount returns args with the repeat count a plugin command reads as ctx.count.
func withCount(args map[string]any, count int) map[string]any {
	out := maps.Clone(args)
	if out == nil {
		out = make(map[string]any, 1)
	}
	out["count"] = count
	return out
}

// DoAll runs the steps in order inside one undo group and stops at the first error. A
// list with a step that must be deferred is deferred as a whole, so the steps keep their
// order.
func (h *pluginHost) DoAll(steps []pluginapi.Step) error {
	m := h.m
	for _, s := range steps {
		if s.Action == "" {
			return pluginapi.Errf(pluginapi.CodeInvalid, "empty action name")
		}
		if s.Count > maxDoCount {
			return pluginapi.Errf(pluginapi.CodeLimit, "count %d is over %d", s.Count, maxDoCount)
		}
	}
	steps = slices.Clone(steps)
	if slices.ContainsFunc(steps, func(s pluginapi.Step) bool { return m.reentrantAction(s.Action) }) {
		m.plug.later = append(m.plug.later, func() tea.Cmd {
			defer m.plugSource(srcAPI)()
			cmd, err := m.doSteps(steps, true)
			if err != nil {
				m.notice = "plugin: queued actions failed: " + err.Error()
			}
			return cmd
		})
		return nil
	}
	cmd, err := m.doSteps(steps, false)
	m.pending = tea.Batch(m.pending, cmd)
	return err
}

// doSteps runs the steps as one undo group of the active document, when the editor has
// the port for it.
func (m *Model) doSteps(steps []pluginapi.Step, hook bool) (tea.Cmd, error) {
	var cmds []tea.Cmd
	deadline := m.doDeadline()
	run := func() error {
		for i, s := range steps {
			var cmd tea.Cmd
			var err error
			if i > 0 && time.Now().After(deadline) {
				return pluginapi.Errf(pluginapi.CodeTimeout, "run_all stopped after %d of %d steps: over the time limit", i, len(steps))
			}
			if rt := m.plug.rt; hook && rt != nil && rt.Owns(s.Action) {
				cmd = m.pluginRun(s.Action, withCount(s.Args, max(s.Count, 1)), srcAPI)
			} else {
				cmd, err = m.doActionUntil(s.Action, s.Args, s.Count, hook, deadline)
			}
			cmds = append(cmds, cmd)
			if err != nil {
				return err
			}
		}
		return nil
	}
	var err error
	if pe := m.plug.pe; pe != nil && pe.ActiveBuffer() != 0 {
		err = pe.UndoGroup(pe.ActiveBuffer(), run)
	} else {
		err = run()
	}
	return tea.Batch(cmds...), err
}

// Holds evaluates a when-clause name against the current state.
func (h *pluginHost) Holds(name string) bool { return h.m.Holds(name) }

// KeyLabel formats the primary binding of action, or "" when there is none.
func (h *pluginHost) KeyLabel(action string) string {
	if kl, ok := h.m.keys.(keyLabeler); ok {
		return kl.Label(action)
	}
	return ""
}

// OpenPalette opens the command palette with query. The palette is deferred with the
// other things that open ui, so it opens after the slice.
func (h *pluginHost) OpenPalette(query string) {
	m := h.m
	m.plug.later = append(m.plug.later, func() tea.Cmd { return m.openPalette(query) })
}

// Actions lists the names Do accepts: the editor actions, the ui actions and the built-in
// commands of the palette. The runtime adds the plugin command ids.
func (h *pluginHost) Actions() []string {
	seen := make(map[string]struct{})
	for _, a := range editor.Actions() {
		seen[string(a)] = struct{}{}
	}
	for _, e := range h.m.commands {
		if e.Source == palmodel.SourceBuiltin {
			seen[e.Action] = struct{}{}
		}
	}
	for _, a := range uiActions {
		seen[a] = struct{}{}
	}
	for a := range reentrantActions {
		seen[a] = struct{}{}
	}
	return slices.Sorted(maps.Keys(seen))
}

// ---- Registration.

// SetCommands replaces the commands of plugin; an empty list removes them. The palette
// reads them when it opens.
func (h *pluginHost) SetCommands(plugin string, cmds []pluginapi.CommandInfo) {
	p := &h.m.plug
	if len(cmds) == 0 {
		h.m.dropCommands(plugin)
		return
	}
	if p.commands == nil {
		p.commands = make(map[string][]pluginapi.CommandInfo)
	}
	if _, known := p.commands[plugin]; !known {
		p.order = append(p.order, plugin)
	}
	p.commands[plugin] = slices.Clone(cmds)
}

// dropCommands removes the commands of plugin.
func (m *Model) dropCommands(plugin string) {
	p := &m.plug
	if _, ok := p.commands[plugin]; !ok {
		return
	}
	delete(p.commands, plugin)
	p.order = slices.DeleteFunc(p.order, func(s string) bool { return s == plugin })
}
