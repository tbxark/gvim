package tui

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/pluginapi"
)

// Plugin key layers (stage S14): the bindings of the plugins in the keymap, input modes,
// the key wait of gvim.input.next_key, plugin contexts, and the panic chords.
//
// The order a key press takes in handleKey (AMENDMENTS C17): the panic chords; the F6 focus
// cycle, the dock, the tab strip and the welcome screen (all existing); pluginKey, only
// while the editor has the focus: a pending key wait, then the editor's input mode, then the
// mode of the active buffer; then the keymap. Overlays are routed before handleKey, so a
// mode never sees a key meant for a prompt.

const (
	escWindow  = 600 * time.Millisecond // three Esc presses within it are a panic chord
	escPresses = 3
	seqTimeout = time.Second // a key sequence in a mode waits this long for its next key
)

// interactiveWaits are the kinds of waits that stand for a plugin prompt, picker, popup or
// key capture. The panic chord resolves them as cancelled. The prompt stage names its kinds
// in plugWait calls and lists them here.
var interactiveWaits = map[string]bool{
	"key": true, "input": true, "confirm": true, "choose": true, "select": true, "picker": true, "popup": true,
}

// plugOverlay marks the overlays that plugins open (pickers, prompts, popups). The panic
// chord closes them; Dismiss, when the overlay has it, runs first.
type plugOverlay interface{ pluginOverlay() }

// modeRef names an input mode: modes are scoped to their plugin.
type modeRef struct{ plugin, name string }

// keyNode is a node of the trie of the key sequences of a mode.
type keyNode struct {
	bound  bool
	action string
	args   map[string]any
	next   map[string]*keyNode
}

// uiMode is a mode of a plugin with its bindings parsed into a trie.
type uiMode struct {
	info pluginapi.ModeInfo
	root *keyNode
}

// seqState is a key sequence in progress in a mode.
type seqState struct {
	node *keyNode // the node reached; nil when no sequence is pending
	gen  int      // timers carry it; a change makes the pending ones stale
}

// pluginKeys is the key state the root keeps for the plugins.
type pluginKeys struct {
	// bindings are the normal-context bindings by plugin. A change schedules one rebuild of
	// the keymap, after the Lua slice, however many bindings the slice set.
	bindings map[string][]pluginapi.Binding
	border   []string // plugins in the order they first bound a key
	rebuild  bool     // a rebuild is queued
	applied  bool     // m.keys was replaced by the PluginKeys port
	preset   string   // the keymap preset a switch chose; "" is the startup one
	gated    map[string][]pluginapi.Binding

	modes   map[string][]*uiMode
	morder  []string
	editor  modeRef           // the active editor-wide input mode
	bufOff  map[uint64]string // buffers whose mode the panic chord switched off, with the mode name
	seq     seqState
	ctx     map[string]map[string]bool
	ctxSeen map[string]bool // contexts a plugin ever set, to either value
	warned  map[string]bool // typo warnings already shown (plugin_warn.go)
	wait    pluginapi.Token // the pending key capture, 0 when none
	esc     []time.Time     // the recent Esc presses that found the plugin input stuck

	status statusState
	idle   pluginIdle
}

// pluginKeyTimeoutMsg ends a key wait that got no key in time.
type pluginKeyTimeoutMsg struct{ t pluginapi.Token }

// pluginSeqMsg ends the wait for the next key of a sequence.
type pluginSeqMsg struct{ gen int }

// pluginKeysMessage handles the messages of this file and of the status and idle files.
func (m *Model) pluginKeysMessage(msg tea.Msg) (tea.Cmd, bool) {
	if m.plug.rt == nil {
		return nil, false
	}
	switch msg := msg.(type) {
	case pluginKeyTimeoutMsg:
		ks := &m.plug.keys
		if ks.wait != msg.t {
			return nil, true
		}
		ks.wait = 0
		return m.plugFinish(pluginapi.Completion{Token: msg.t, Err: string(pluginapi.CodeTimeout) + ": no key was pressed in time"}), true
	case pluginSeqMsg:
		ks := &m.plug.keys
		if ks.seq.gen != msg.gen || ks.seq.node == nil {
			return nil, true
		}
		node := ks.seq.node
		ks.seq = seqState{gen: ks.seq.gen + 1}
		if node.bound {
			return m.modeRun(node.action, node.args), true
		}
		return nil, true
	case pluginIdleMsg:
		return m.pluginIdleTick(msg), true
	}
	return nil, false
}

// ---- Registrations (pluginapi.UIHost).

// SetBindings replaces the normal-context bindings of plugin. The keymap is rebuilt once
// after the slice (plugKeymapDirty), not for each call.
func (h *pluginHost) SetBindings(plugin string, b []pluginapi.Binding) {
	m := h.m
	ks := &m.plug.keys
	if len(b) == 0 {
		delete(ks.bindings, plugin)
		ks.border = slices.DeleteFunc(ks.border, func(s string) bool { return s == plugin })
	} else {
		if ks.bindings == nil {
			ks.bindings = make(map[string][]pluginapi.Binding)
		}
		if _, known := ks.bindings[plugin]; !known {
			ks.border = append(ks.border, plugin)
		}
		ks.bindings[plugin] = slices.Clone(b)
	}
	m.plugKeymapDirty()
}

// plugKeymapDirty queues one rebuild of the keymap. The rebuild runs in the pump, with no
// Lua on the stack, so a plugin that binds thousands of keys one call at a time costs one
// rebuild.
func (m *Model) plugKeymapDirty() {
	ks := &m.plug.keys
	if ks.rebuild {
		return
	}
	ks.rebuild = true
	m.plug.later = append(m.plug.later, func() tea.Cmd {
		m.plugRebuildKeymap()
		return nil
	})
}

// pluginBindingsForEditor are the plugin bindings the keymap layers over its preset, for the
// keybinding editor's preview. It is nil without a plugin runtime.
func (m *Model) pluginBindingsForEditor() []pluginapi.Binding {
	if m.plug.rt == nil {
		return nil
	}
	return m.plugBindingsInOrder()
}

// plugBindingsInOrder returns the bindings of every plugin in load order, so the first
// plugin to take a chord wins it in the layering.
func (m *Model) plugBindingsInOrder() []pluginapi.Binding {
	ks := &m.plug.keys
	rank := map[string]int{}
	for i, p := range m.plug.rt.Plugins() {
		rank[p.Name] = i
	}
	names := slices.Clone(ks.border)
	slices.SortStableFunc(names, func(a, b string) int {
		ra, oka := rank[a]
		rb, okb := rank[b]
		switch {
		case oka && okb:
			return ra - rb
		case oka:
			return -1
		case okb:
			return 1
		}
		return 0
	})
	var out []pluginapi.Binding
	for _, n := range names {
		out = append(out, ks.bindings[n]...)
	}
	return out
}

// plugRebuildKeymap layers the plugin bindings into the keymap through the PluginKeys port
// and installs the result. A key sequence is dropped here, with a notice: the root resolves
// single keys, and a sequence works only inside a mode.
func (m *Model) plugRebuildKeymap() {
	ks := &m.plug.keys
	ks.rebuild = false
	if m.plug.rt == nil {
		return
	}
	flat := m.plugBindingsInOrder()
	var dropped []string
	flat = slices.DeleteFunc(flat, func(b pluginapi.Binding) bool {
		if len(strings.Fields(b.Chord)) > 1 {
			dropped = append(dropped, fmt.Sprintf("plugin '%s': key sequence %q works in a mode only", b.Plugin, b.Chord))
			return true
		}
		return false
	})
	ks.gated = nil
	for _, b := range flat {
		if len(b.When) == 0 {
			continue
		}
		if c, err := keymap.NormalizeChord(b.Chord); err == nil {
			if ks.gated == nil {
				ks.gated = make(map[string][]pluginapi.Binding)
			}
			ks.gated[c] = append(ks.gated[c], b)
		}
	}
	var problems []string
	if pk := m.root.ports.PluginKeys; pk != nil && (len(flat) > 0 || ks.applied) {
		km, err := pk(ks.preset, flat)
		if km != nil {
			m.keys = km
			ks.applied = len(flat) > 0
		}
		if err != nil {
			problems = append(problems, err.Error())
		}
	}
	for _, msg := range append(problems, dropped...) {
		m.plug.host.Notify("", pluginapi.LevelWarn, msg)
	}
}

// pluginSwitchKeymap is called by switch_keybinding_map with the new preset name and the
// keymap SwitchKeymap built for it. It layers the plugin bindings over that preset, so a
// switch keeps them, and remembers the preset for the next rebuild.
func (m *Model) pluginSwitchKeymap(name string, base Keymap) Keymap {
	ks := &m.plug.keys
	ks.preset = name
	pk := m.root.ports.PluginKeys
	if m.plug.rt == nil || pk == nil || len(ks.bindings) == 0 {
		ks.applied = false
		return base
	}
	flat := slices.DeleteFunc(m.plugBindingsInOrder(), func(b pluginapi.Binding) bool { return len(strings.Fields(b.Chord)) > 1 })
	km, err := pk(name, flat)
	if err != nil {
		m.plug.host.Notify("", pluginapi.LevelWarn, err.Error())
	}
	if km == nil {
		ks.applied = false
		return base
	}
	ks.applied = true
	return km
}

// pluginGate reports whether a resolved key belongs to a plugin binding whose when-clause
// fails now. The key then does nothing and the status row says so.
func (m *Model) pluginGate(kp KeyPress, ctx KeyContext, cmd Command) bool {
	ks := &m.plug.keys
	if len(ks.gated) == 0 || ctx != KeyContextNormal {
		return false
	}
	chord, err := keymap.Canonical(kp.Name, kp.Mods)
	if err != nil {
		return false
	}
	for _, b := range ks.gated[chord] {
		if b.Action == cmd.Action {
			m.plugCheckWhen(b.Plugin, b.Action, b.When)
		}
		if b.Action == cmd.Action && !m.whenHolds(b.When) {
			title := b.Action
			if info, ok := m.plug.info(b.Action); ok {
				title = cmdTitle(info)
			}
			m.notice = title + " is not available now"
			return true
		}
	}
	return false
}

// SetModes replaces the modes of plugin. A chord that is not a key is left out, with a
// notice.
func (h *pluginHost) SetModes(plugin string, infos []pluginapi.ModeInfo) {
	m := h.m
	ks := &m.plug.keys
	var bad []string
	modes := make([]*uiMode, 0, len(infos))
	for _, info := range infos {
		um, dropped := newUIMode(info)
		for _, d := range dropped {
			bad = append(bad, fmt.Sprintf("plugin '%s': mode '%s': key %s", plugin, info.Name, d))
		}
		modes = append(modes, um)
	}
	if len(modes) == 0 {
		delete(ks.modes, plugin)
		ks.morder = slices.DeleteFunc(ks.morder, func(s string) bool { return s == plugin })
	} else {
		if ks.modes == nil {
			ks.modes = make(map[string][]*uiMode)
		}
		if _, known := ks.modes[plugin]; !known {
			ks.morder = append(ks.morder, plugin)
		}
		ks.modes[plugin] = modes
	}
	if ks.editor.plugin == plugin && m.findMode(ks.editor) == nil {
		m.setEditorMode(modeRef{})
	}
	ks.seq = seqState{gen: ks.seq.gen + 1}
	for _, msg := range bad {
		h.Notify("", pluginapi.LevelWarn, msg)
	}
}

// newUIMode parses the bindings of a mode into a trie. It returns the chords it left out,
// each with the reason.
func newUIMode(info pluginapi.ModeInfo) (*uiMode, []string) {
	um := &uiMode{info: info, root: &keyNode{}}
	var bad []string
	for _, chord := range slices.Sorted(maps.Keys(info.Bindings)) {
		canon, err := keymap.NormalizeChord(chord)
		if err != nil {
			bad = append(bad, fmt.Sprintf("%q ignored: %v", chord, err))
			continue
		}
		node := um.root
		for _, step := range strings.Fields(canon) {
			if node.next == nil {
				node.next = make(map[string]*keyNode)
			}
			child := node.next[step]
			if child == nil {
				child = &keyNode{}
				node.next[step] = child
			}
			node = child
		}
		node.bound, node.action, node.args = true, info.Bindings[chord], info.Args[chord]
	}
	return um, bad
}

// findMode returns the mode ref names.
func (m *Model) findMode(ref modeRef) *uiMode {
	for _, um := range m.plug.keys.modes[ref.plugin] {
		if um.info.Name == ref.name {
			return um
		}
	}
	return nil
}

// findModeByName returns the first mode called name, plugins in the order they defined
// modes. A buffer's mode is a bare name, so this is how it is resolved.
func (m *Model) findModeByName(name string) *uiMode {
	ks := &m.plug.keys
	for _, p := range ks.morder {
		for _, um := range ks.modes[p] {
			if um.info.Name == name {
				return um
			}
		}
	}
	return nil
}

// SetInputMode makes the mode of plugin the editor's input mode, or with "" ends it when
// plugin owns it. The editor has one input mode: entering one replaces the other.
func (h *pluginHost) SetInputMode(plugin, mode string) error {
	m := h.m
	if mode == "" {
		if m.plug.keys.editor.plugin == plugin {
			m.setEditorMode(modeRef{})
		}
		return nil
	}
	ref := modeRef{plugin, mode}
	if m.findMode(ref) == nil {
		return pluginapi.Errf(pluginapi.CodeNotFound, "mode %q of plugin %q is not defined", mode, plugin)
	}
	m.setEditorMode(ref)
	return nil
}

// setEditorMode changes the editor's input mode and raises mode_changed.
func (m *Model) setEditorMode(ref modeRef) {
	ks := &m.plug.keys
	prev := ks.editor
	if prev == ref {
		return
	}
	ks.editor = ref
	ks.seq = seqState{gen: ks.seq.gen + 1}
	data := map[string]any{}
	if ref != (modeRef{}) {
		data["mode"], data["plugin"] = ref.name, ref.plugin
	}
	if prev != (modeRef{}) {
		data["previous"], data["previous_plugin"] = prev.name, prev.plugin
	}
	m.plugEmit("mode_changed", data)
}

// SetContext sets or clears a plugin context.
func (h *pluginHost) SetContext(plugin, name string, on bool) {
	ks := &h.m.plug.keys
	if ks.ctxSeen == nil {
		ks.ctxSeen = make(map[string]bool)
	}
	ks.ctxSeen[name] = true
	if !on {
		delete(ks.ctx[plugin], name)
		return
	}
	if ks.ctx == nil {
		ks.ctx = make(map[string]map[string]bool)
	}
	if ks.ctx[plugin] == nil {
		ks.ctx[plugin] = make(map[string]bool)
	}
	ks.ctx[plugin][name] = true
}

// pluginContext reports the value of a plugin-defined when-clause name, and whether some
// plugin defined it. Holds asks it for names it does not know. "mode:<plugin>:<name>" holds
// while that input mode is the editor's.
func (m *Model) pluginContext(name string) (on, defined bool) {
	ks := &m.plug.keys
	if rest, ok := strings.CutPrefix(name, "mode:"); ok {
		plugin, mode, ok := strings.Cut(rest, ":")
		return ok && ks.editor == modeRef{plugin, mode}, true
	}
	for _, ctx := range ks.ctx {
		if ctx[name] {
			return true, true
		}
	}
	return false, false
}

// plugKeysGone drops what the plugin (at generation gen) set: bindings, modes, contexts and
// the input mode. A key capture has no owner here; the runtime cancels it on unload.
func (m *Model) plugKeysGone(plugin string, gen uint64) {
	_ = gen
	ks := &m.plug.keys
	if _, ok := ks.bindings[plugin]; ok {
		delete(ks.bindings, plugin)
		ks.border = slices.DeleteFunc(ks.border, func(s string) bool { return s == plugin })
		m.plugKeymapDirty()
	}
	delete(ks.ctx, plugin)
	if _, ok := ks.modes[plugin]; ok {
		delete(ks.modes, plugin)
		ks.morder = slices.DeleteFunc(ks.morder, func(s string) bool { return s == plugin })
	}
	if ks.editor.plugin == plugin {
		m.setEditorMode(modeRef{})
	}
	ks.seq = seqState{gen: ks.seq.gen + 1}
}

// ---- The key capture of gvim.input.next_key.

// WaitKey captures the next key press for the wait t. Only one capture is pending.
func (h *pluginHost) WaitKey(t pluginapi.Token, timeoutMS int) error {
	m := h.m
	ks := &m.plug.keys
	if ks.wait != 0 {
		if _, live := m.plug.waits[ks.wait]; live {
			return pluginapi.Errf(pluginapi.CodeBusy, "another key wait is pending")
		}
	}
	if timeoutMS <= 0 {
		timeoutMS = 10_000
	}
	timeoutMS = min(timeoutMS, 60_000)
	if err := m.plugWait(t, "key"); err != nil {
		return err
	}
	ks.wait = t
	d := time.Duration(timeoutMS) * time.Millisecond
	m.plug.later = append(m.plug.later, func() tea.Cmd {
		return tea.Tick(d, func(time.Time) tea.Msg { return pluginKeyTimeoutMsg{t} })
	})
	return nil
}

// keyWaiting reports whether a key capture is pending. A wait the runtime cancelled is gone
// from the wait table, which ends the capture.
func (m *Model) keyWaiting() bool {
	ks := &m.plug.keys
	if ks.wait == 0 {
		return false
	}
	if _, live := m.plug.waits[ks.wait]; !live {
		ks.wait = 0
		return false
	}
	return true
}

// pluginExtraKeys names the special keys that translateKey has no name for and the keymap
// vocabulary has one. Every other special key (F13 and above, CapsLock, Menu, the rest of the
// keypad) has no chord name, so its Key.key is empty: a plugin sees the press but cannot match
// it, and never gets a private-use code point.
var pluginExtraKeys = map[rune]string{
	tea.KeyInsert:  "insert",
	tea.KeyKpEnter: "enter",
}

// pluginKeyOf converts a key press to the Key a plugin sees.
func pluginKeyOf(k tea.Key) pluginapi.Key {
	kp, text := translateKey(k)
	out := pluginapi.Key{
		Text: text,
		Ctrl: k.Mod&tea.ModCtrl != 0, Alt: k.Mod&tea.ModAlt != 0,
		Shift: k.Mod&tea.ModShift != 0, Super: k.Mod&tea.ModSuper != 0,
	}
	if kp.Name == "BackTab" {
		out.Key, out.Shift = "tab", true
		return out
	}
	if k.Code > unicode.MaxRune && kp.Name == string(unicode.ReplacementChar) {
		out.Key = pluginExtraKeys[k.Code]
		return out
	}
	name, err := keymap.Canonical(kp.Name, nil)
	if err != nil {
		name = strings.ToLower(kp.Name)
	}
	out.Key = name
	return out
}

// ---- Key presses.

// editorHasKeys reports whether the editor, not the explorer, the tab strip, the dock or
// the welcome screen, has the keyboard.
func (m *Model) editorHasKeys() bool {
	return !m.explorerFocus && !m.tabFocus && !m.dockKeyboard() && !m.welcomeVisible()
}

// activeMode returns the mode that takes keys now: the editor's input mode, else the mode of
// the active buffer.
func (m *Model) activeMode() *uiMode {
	ks := &m.plug.keys
	if ks.editor != (modeRef{}) {
		return m.findMode(ks.editor)
	}
	if len(ks.modes) == 0 || m.plug.pe == nil {
		return nil
	}
	id := m.plug.pe.ActiveBuffer()
	info, ok := m.plug.pe.Buffer(id)
	if !ok || info.Mode == "" || ks.bufOff[uint64(id)] == info.Mode {
		return nil
	}
	return m.findModeByName(info.Mode)
}

// pluginInputActive reports whether a plugin holds the keyboard in some way: an input mode,
// a key capture or an overlay.
func (m *Model) pluginInputActive() bool {
	if m.plug.keys.editor != (modeRef{}) || m.keyWaiting() || m.activeMode() != nil {
		return true
	}
	for _, o := range m.overlays {
		if _, ok := o.(plugOverlay); ok {
			return true
		}
	}
	return false
}

// pluginKey runs in handleKey after the focus cycle, the dock, the tab strip and the
// welcome screen, and before the keymap. It reports whether a plugin took the key: a
// pending key wait, an input mode or a buffer mode. It only acts when the editor has the
// focus.
func (m *Model) pluginKey(k tea.Key) (tea.Cmd, bool) {
	if m.plug.rt == nil || !m.editorHasKeys() {
		return nil, false
	}
	ks := &m.plug.keys
	if m.keyWaiting() {
		t := ks.wait
		ks.wait = 0
		return m.plugFinish(pluginapi.Completion{Token: t, Values: []any{pluginKeyOf(k)}}), true
	}
	um := m.activeMode()
	if um == nil {
		return nil, false
	}
	return m.modeKey(um, k)
}

// modeKey resolves one key press in the mode um: a binding, a step of a sequence, or the
// mode's policy for a key it does not bind.
func (m *Model) modeKey(um *uiMode, k tea.Key) (tea.Cmd, bool) {
	ks := &m.plug.keys
	kp, text := translateKey(k)
	chord, err := keymap.Canonical(kp.Name, kp.Mods)
	cur := um.root
	if ks.seq.node != nil {
		cur = ks.seq.node
		ks.seq = seqState{gen: ks.seq.gen + 1}
	}
	if err == nil {
		if child := cur.next[chord]; child != nil {
			if len(child.next) == 0 {
				return m.modeRun(child.action, child.args), true
			}
			ks.seq = seqState{node: child, gen: ks.seq.gen + 1}
			gen := ks.seq.gen
			return tea.Tick(seqTimeout, func(time.Time) tea.Msg { return pluginSeqMsg{gen} }), true
		}
	}
	if cur != um.root {
		// The sequence broke off. A shorter binding at the node it reached runs first, then
		// the key is handled afresh.
		var first tea.Cmd
		if cur.bound {
			first = m.modeRun(cur.action, cur.args)
		}
		cmd, ok := m.modeKey(um, k)
		if ok {
			return tea.Batch(first, cmd), true
		}
		return first, first != nil
	}
	if text != "" {
		switch um.info.Text {
		case pluginapi.TextSwallow:
			return nil, true
		case pluginapi.TextHandler:
			return m.modeRun(um.info.TextAction, map[string]any{"text": text}), true
		}
		return nil, false
	}
	return nil, !um.info.Fallthrough
}

// modeRun runs the action of a mode binding the way a key binding runs: as a command, with
// source "mode" for a plugin command.
func (m *Model) modeRun(action string, args map[string]any) tea.Cmd {
	if action == "" {
		return nil
	}
	defer m.plugSource(pluginapi.SrcMode)()
	return m.dispatch(Command{Action: action, Args: args})
}

// pluginPaste is the paste hook of clipMessage. It reports whether the active mode took the
// text: a mode drops it or hands it to its text function with paste = true.
func (m *Model) pluginPaste(content string) (tea.Cmd, bool) {
	if m.plug.rt == nil || !m.editorHasKeys() {
		return nil, false
	}
	um := m.activeMode()
	if um == nil {
		return nil, false
	}
	switch um.info.Paste {
	case pluginapi.TextSwallow:
		return nil, true
	case pluginapi.TextHandler:
		return m.modeRun(um.info.TextAction, map[string]any{"text": content, "paste": true}), true
	}
	return nil, false
}

// ---- The panic chords (AMENDMENTS C27).

// panicChord reports whether k is one of the two chords that always clear the plugin input:
// ctrl+^, matched on what the terminal decoded so that it fires in the legacy encoding
// (byte 0x1e) and as the enhanced ctrl+^ key press, and ctrl+alt+shift+esc.
func panicChord(k tea.Key) bool {
	ctrl, alt, shift := k.Mod&tea.ModCtrl != 0, k.Mod&tea.ModAlt != 0, k.Mod&tea.ModShift != 0
	switch {
	case k.Text == "\x1e":
		return true
	case ctrl && !alt && k.Code == '^':
		return true
	case ctrl && alt && shift && k.Code == tea.KeyEscape:
		return true
	}
	return false
}

// panicAlias reports whether k is ctrl+shift+6, how an enhanced keyboard spells ctrl+^. Every
// keymap preset binds it to set_bookmark 6, so it only counts as a panic chord while a plugin
// holds the keyboard; the other spellings always fire.
func panicAlias(k tea.Key) bool {
	return k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModShift) == tea.ModCtrl|tea.ModShift && k.Code == '6'
}

// pluginPanicKey runs first in handleKey, and in update before the key goes to an open
// overlay (a plugin picker, popup or prompt is an overlay, and handleKey never sees its
// keys). It reports whether k is a panic chord, which clears every plugin mode, key wait
// and overlay, and which no layer may intercept, and the command that results. Each key
// press reaches it once. The third trigger is three Esc presses within escWindow that each
// find the plugin input stuck (pluginEscStuck).
func (m *Model) pluginPanicKey(k tea.Key) (tea.Cmd, bool) {
	if f := m.plug.panicKey; f != nil && f(k) {
		return nil, true
	}
	if m.plug.rt == nil {
		return nil, false
	}
	ks := &m.plug.keys
	if panicChord(k) || (panicAlias(k) && m.pluginInputActive()) {
		ks.esc = nil
		return m.plugPanic(), true
	}
	if k.Code != tea.KeyEscape || k.Mod != 0 {
		ks.esc = nil
		return nil, false
	}
	now := m.now()
	ks.esc = slices.DeleteFunc(ks.esc, func(t time.Time) bool { return now.Sub(t) > escWindow })
	if !m.pluginEscStuck() {
		// Esc does what the plugin made it do: pressing it again and again is a habit, not a plea.
		ks.esc = nil
		return nil, false
	}
	ks.esc = append(ks.esc, now)
	if len(ks.esc) >= escPresses {
		ks.esc = nil
		return m.plugPanic(), true
	}
	return nil, false
}

// pluginCancelKey reports whether k is ctrl+c on an overlay a plugin opened (a prompt, picker or
// popup): it cancels like Esc does, as it does in a terminal and in vi's `:` and `/` prompts.
func (m *Model) pluginCancelKey(k tea.Key) bool {
	if k.Mod != tea.ModCtrl || k.Code != 'c' || len(m.overlays) == 0 {
		return false
	}
	_, ok := m.overlays[len(m.overlays)-1].(plugOverlay)
	return ok
}

// overlayCancels reports whether k closes the top overlay: Esc does, unless the overlay asks for
// Esc itself (takesEscape), and ctrl+c does for a plugin overlay (pluginCancelKey).
func (m *Model) overlayCancels(k tea.Key) bool {
	if k.Code == tea.KeyEscape {
		return len(m.overlays) > 0 && !takesEscape(m.overlays[len(m.overlays)-1])
	}
	return m.pluginCancelKey(k)
}

// pluginEscStuck reports whether an Esc press finds the plugin input stuck: a plugin waits for
// a key (Esc is only a key to it), has a prompt, picker or popup open, or the active mode does
// not bind Esc, so that the key does nothing and the user may have no other way out. A mode
// that binds Esc itself, like vi's, answers it: the press is not a panic, however often it is
// repeated.
func (m *Model) pluginEscStuck() bool {
	if m.keyWaiting() {
		return true
	}
	for _, o := range m.overlays {
		if _, ok := o.(plugOverlay); ok {
			return true
		}
	}
	um := m.activeMode()
	if um == nil {
		return false
	}
	n := um.root.next["escape"]
	return n == nil || !n.bound
}

// plugPanic clears the plugin input: the input mode, the key capture, the mode of the active
// buffer and the overlays plugins opened; the waits of those end as cancelled.
func (m *Model) plugPanic() tea.Cmd {
	ks := &m.plug.keys
	m.setEditorMode(modeRef{})
	ks.seq = seqState{gen: ks.seq.gen + 1}
	if um := m.activeMode(); um != nil && m.plug.pe != nil {
		id := uint64(m.plug.pe.ActiveBuffer())
		if ks.bufOff == nil {
			ks.bufOff = make(map[uint64]string)
		}
		ks.bufOff[id] = um.info.Name
	}
	// The prompts, pickers and popups of the prompt stage close through plugOverlaysCancel,
	// which resolves their waits; they must not see the Dismiss/delete below.
	cmds := []tea.Cmd{m.plugOverlaysCancel("plugin input cleared")}
	for i := len(m.overlays) - 1; i >= 0; i-- {
		if _, ok := m.overlays[i].(pluginOverlay); ok {
			continue
		}
		if _, ok := m.overlays[i].(plugOverlay); !ok {
			continue
		}
		if d, ok := m.overlays[i].(interface{ Dismiss() }); ok {
			d.Dismiss()
		}
		m.overlays = slices.Delete(m.overlays, i, i+1)
	}
	toks := make([]pluginapi.Token, 0, len(m.plug.waits))
	for t, kind := range m.plug.waits {
		if interactiveWaits[kind] {
			toks = append(toks, t)
		}
	}
	slices.Sort(toks)
	for _, t := range toks {
		cmds = append(cmds, m.plugFinish(pluginapi.Completion{Token: t, Err: string(pluginapi.CodeCancelled) + ": plugin input cleared"}))
	}
	ks.wait = 0
	m.notice = "plugin input cleared"
	return tea.Batch(cmds...)
}
