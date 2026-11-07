package ui

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/picker"
	"github.com/tbxark/gvim/internal/ui/popup"
)

// The plugin management commands (FINAL X11): the manager, the reload picker, the log, the
// reload commands and Load Plugin from Buffer. The init.lua commands are in plugin_init.go.
//
// They are reentrant actions (see reentrantActions): a plugin's command.run defers them,
// so they run outside a Lua slice and reach the runtime through pluginControl, which
// pumps after the call. The manager is a picker over Runtime.Plugins(); Enter on a row
// opens a popup of the actions that row allows. Nothing here talks to the runtime from
// inside an overlay's Update: the overlays answer with messages, which the root routes to
// pluginManagerMessage like any other message, so the runtime is never entered from
// routeOverlay.

// pluginFiles is the optional part of the runtime that the manager and the init commands
// read: the path of init.lua and whether --no-plugins switched discovery off. The real
// runtime has both; a runtime without them has no init.lua to edit.
type pluginFiles interface {
	InitFile() string
	NoPlugins() bool
}

// pluginSourceMax is the largest buffer Load Plugin from Buffer sends. The runtime refuses
// a source over the same size (plugin.MaxLuaFile), and says so; checking here keeps a huge
// buffer from being copied only to be refused.
const pluginSourceMax = 256 << 10

// noPluginsNotice is the status text of a plugin command in a session without a plugin
// runtime.
const noPluginsNotice = "Plugins are not available in this session"

// pluginUIAction runs one of the plugin management actions. featureAction hands them
// over. It returns the command the action starts.
func (m *Model) pluginUIAction(action string) tea.Cmd {
	if m.plug.rt == nil {
		m.notice = noPluginsNotice
		return nil
	}
	switch action {
	case "plugin_manager":
		return m.openPluginManager(managerManage)
	case "plugin_reload":
		return m.openPluginManager(managerReload)
	case "plugin_show_log":
		return m.showPluginLog("")
	case "plugin_reload_all":
		return m.pluginControl(pluginapi.Control{Op: "reload_all"})
	case "load_plugin_from_buffer":
		return m.loadPluginFromBuffer()
	case "init_check":
		return m.pluginControl(pluginapi.Control{Op: "init_check"})
	case "init_reload":
		return m.pluginControl(pluginapi.Control{Op: "init_reload"})
	case "init_edit":
		return m.initEdit()
	case "prompt_save_macro_to_init":
		m.askMacroName(false)
	case "prompt_promote_macro":
		m.askMacroName(true)
	}
	return nil
}

// ---- the manager and the reload picker

// managerMode says what a choice in the picker does.
type managerMode int

const (
	managerManage managerMode = iota // Enter opens the actions of the plugin
	managerReload                    // Enter reloads the plugin
)

// managerOverlay is the picker of the manager or of Plugin: Reload.... It acts on
// nothing: the picker's events and ranking come back to the root as messages.
type managerOverlay struct {
	p     *picker.Picker
	mode  managerMode
	infos []pluginapi.PluginInfo // the rows; a picker item's ID indexes it
}

func (o *managerOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	cmd := o.cmdFor(o.p.Update(msg))
	if !o.p.Active() {
		return nil, cmd
	}
	return o, cmd
}

func (o *managerOverlay) Draw(g *grid.Grid) { o.p.Draw(g) }
func (o *managerOverlay) Active() bool      { return o.p.Active() }

// managerPickMsg carries a choice of the picker; managerRankMsg carries a ranking result
// back to the picker that asked for it.
type (
	managerPickMsg struct {
		mode  managerMode
		infos []pluginapi.PluginInfo
		id    int
	}
	managerRankMsg struct {
		ov  *managerOverlay
		msg tea.Msg
	}
)

// cmdFor turns what the picker asks of its owner into commands.
func (o *managerOverlay) cmdFor(r picker.Result) tea.Cmd {
	var cmds []tea.Cmd
	if r.Cmd != nil {
		cmds = append(cmds, r.Cmd)
	}
	if r.Rank != nil {
		rank := r.Rank
		cmds = append(cmds, func() tea.Msg { return managerRankMsg{ov: o, msg: rank()} })
	}
	for _, e := range r.Events {
		if c, ok := e.(picker.Chosen); ok {
			pick := managerPickMsg{mode: o.mode, infos: o.infos, id: c.ID}
			cmds = append(cmds, func() tea.Msg { return pick })
		}
	}
	return tea.Batch(cmds...)
}

// reloadable reports whether Plugin: Reload... offers the plugin: it runs from a file, and
// the manager has not switched it off.
func reloadable(in pluginapi.PluginInfo) bool {
	return in.Status != "legacy" && in.Status != "disabled" && in.Source != "buffer"
}

// openPluginManager opens the picker of the plugins: the manager, or the reload picker.
func (m *Model) openPluginManager(mode managerMode) tea.Cmd {
	rt := m.plug.rt
	infos := slices.Clone(rt.Plugins())
	off := false
	if f, ok := rt.(pluginFiles); ok {
		off = f.NoPlugins()
	}
	if mode == managerReload {
		infos = slices.DeleteFunc(infos, func(in pluginapi.PluginInfo) bool { return !reloadable(in) })
	}
	switch {
	case len(infos) > 0:
	case mode == managerReload:
		m.notice = "No plugin can be reloaded"
		return nil
	case off:
		m.notice = "Plugins are disabled by --no-plugins"
		return nil
	default:
		m.notice = "No plugins found"
		return nil
	}
	title, footer := "Plugins", "Enter: actions    Esc: close"
	if mode == managerReload {
		title, footer = "Reload plugin", "Enter: reload    Esc: close"
	}
	if off {
		title += " (disabled by --no-plugins)"
	}
	p := picker.New(m.styles, picker.Options{
		Title: title, Footer: footer, Placeholder: "Filter plugins",
		Items: managerItems(infos), Fuzzy: true,
	})
	ov := &managerOverlay{p: p, mode: mode, infos: infos}
	m.pushOverlay(ov)
	return ov.cmdFor(p.Init())
}

// managerItems makes one picker row of each plugin: name, version, source and status in
// columns, then the counts, why the manager cannot toggle it, and the last error. The
// IDs index infos. The text comes from the plugin, so control characters are cleaned.
func managerItems(infos []pluginapi.PluginInfo) []picker.Item {
	var width [4]int
	cells := make([][4]string, len(infos))
	for i, in := range infos {
		cells[i] = [4]string{
			cleanText(in.Name), cleanText(cmp.Or(in.Version, "-")),
			cleanText(cmp.Or(in.Source, "-")), cleanText(cmp.Or(in.Status, "unknown")),
		}
		for j, c := range cells[i] {
			width[j] = max(width[j], ansi.StringWidth(c))
		}
	}
	items := make([]picker.Item, len(infos))
	for i, in := range infos {
		var b strings.Builder
		for j, c := range cells[i] {
			b.WriteString(c)
			if j < len(width)-1 {
				b.WriteString(strings.Repeat(" ", width[j]-ansi.StringWidth(c)+2))
			}
		}
		items[i] = picker.Item{ID: i, Text: b.String(), Detail: managerDetail(in)}
	}
	return items
}

// managerDetail is the part of a row after the columns. A running plugin shows how many
// commands and subscriptions it has. Locked says why the manager cannot toggle the
// plugin ("set in config"); for a legacy row it is the reason the file is not run.
func managerDetail(in pluginapi.PluginInfo) string {
	var parts []string
	switch in.Status {
	case "loaded", "loading", "suspended":
		parts = append(parts, fmt.Sprintf("(%d cmds, %d subs)", in.Commands, in.Subscriptions))
	}
	switch {
	case in.Locked == "":
	case in.Status == "legacy":
		parts = append(parts, cleanText(in.Locked))
	default:
		parts = append(parts, "locked: "+cleanText(in.Locked))
	}
	if in.Err != "" {
		first, _, _ := strings.Cut(in.Err, "\n")
		parts = append(parts, cleanText(first))
	}
	return strings.Join(parts, "  ")
}

// managerChoice is the payload of a row of the actions popup: op is reload, enable,
// disable, log, source or copy.
type managerChoice struct {
	op   string
	info pluginapi.PluginInfo
}

// managerChoices lists the actions the plugin allows. A legacy file can only be opened,
// to read what it did. A plugin of the binary or of a buffer has no file to open. Reload
// needs a plugin that runs from a file and is on; the manager cannot switch off a plugin
// that --plugin or init.lua gave, and it says why when the configuration locks one.
func managerChoices(in pluginapi.PluginInfo) []popup.Item {
	var items []popup.Item
	add := func(op, label, detail string) {
		items = append(items, popup.Item{Label: label, Detail: cleanText(detail), Data: managerChoice{op: op, info: in}})
	}
	if in.Status == "legacy" {
		add("source", "Open Source", "")
		return items
	}
	if reloadable(in) {
		add("reload", "Reload", "")
	}
	if in.Source != "cli" && in.Source != "init" {
		if in.Status == "disabled" {
			add("enable", "Enable", in.Locked)
		} else {
			add("disable", "Disable", in.Locked)
		}
	}
	add("log", "Show Log", "")
	if in.Source != "bundled" && in.Source != "buffer" {
		add("source", "Open Source", "")
	}
	if in.Err != "" {
		add("copy", "Copy Error", "")
	}
	return items
}

// ---- messages

// managerSourceMsg is the answer of the command that looks for a plugin's source file.
type managerSourceMsg struct {
	name, path string
	err        error
}

// pluginManagerMessage handles the messages of this file, of plugin_init.go, and the two
// that carry a choice of a popup or a prompt that this group opened. It reports whether
// msg was one of them: any other popup choice or prompt answer is not.
func (m *Model) pluginManagerMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case managerRankMsg:
		if !msg.ov.p.Active() {
			return nil, true // the picker closed while the ranking ran
		}
		return msg.ov.cmdFor(msg.ov.p.Update(msg.msg)), true
	case managerPickMsg:
		if msg.id < 0 || msg.id >= len(msg.infos) {
			return nil, true
		}
		in := msg.infos[msg.id]
		if msg.mode == managerReload {
			return m.pluginControl(pluginapi.Control{Op: "reload", Name: in.Name}), true
		}
		if items := managerChoices(in); len(items) > 0 {
			title := cleanText(in.Name)
			m.ShowPopup(m.popupAnchor("center", title, items, 0), title, items)
		}
		return nil, true
	case popup.Chosen:
		c, ok := msg.Item.Data.(managerChoice)
		if !ok {
			return nil, false
		}
		return m.managerRun(c), true
	case managerSourceMsg:
		if msg.err != nil {
			m.notice = "Plugin '" + msg.name + "': " + msg.err.Error()
			return nil, true
		}
		m.openFile(msg.path, false)
		m.focusEditor()
		return nil, true
	}
	return m.pluginInitMessage(msg)
}

// managerRun runs the action chosen in the popup of a plugin.
func (m *Model) managerRun(c managerChoice) tea.Cmd {
	in := c.info
	switch c.op {
	case "reload", "enable", "disable":
		return m.pluginControl(pluginapi.Control{Op: c.op, Name: in.Name})
	case "log":
		return m.showPluginLog(in.Name)
	case "source":
		return pluginSourceCmd(in, m.initFilePath())
	case "copy":
		m.notice = "Copied the error of plugin '" + in.Name + "'"
		return m.clipboardOut("copy", editor.Result{Text: in.Err})
	}
	return nil
}

// ---- the log

// showPluginLog opens the plugin log, newest line last, in a read-only virtual buffer, so
// that it is never modified and never asks to be saved. A plugin name limits it to that
// plugin's lines. A session without an editor port for plugins has no virtual buffers; the
// log then opens in an untitled tab.
func (m *Model) showPluginLog(plugin string) tea.Cmd {
	text := pluginLogText(m.plug.rt.Log(), plugin)
	if m.plug.pe == nil {
		return m.openScratch(text)
	}
	if _, err := m.plug.host.CreateBuffer(pluginapi.CreateBuffer{
		Name: "Plugin Log", Text: text, ReadOnly: true, Focus: true,
	}); err != nil {
		m.notice = "Plugin log: " + err.Error()
	}
	return nil
}

// pluginLogText formats the log entries of plugin, or of every plugin when it is empty, one
// line each: time, level, source and message. The loader's own lines are from "gvim". The
// continuation lines of a message are indented. Entries that plugins wrote can hold
// control characters, which become spaces.
func pluginLogText(entries []pluginapi.LogEntry, plugin string) string {
	var b strings.Builder
	for _, e := range entries {
		if plugin != "" && e.Plugin != plugin {
			continue
		}
		msg := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) && r != '\n' && r != '\t' {
				return ' '
			}
			return r
		}, e.Msg)
		msg = strings.ReplaceAll(msg, "\n", "\n    ")
		fmt.Fprintf(&b, "%s %-5s %s: %s\n", e.Time.Format("15:04:05"), e.Level, cmp.Or(e.Plugin, "gvim"), msg)
	}
	switch {
	case b.Len() > 0:
		return b.String()
	case plugin != "":
		return "No log entries for plugin '" + plugin + "'.\n"
	}
	return "No plugin log entries yet.\n"
}

// ---- the source of a plugin

// pluginSourceCmd returns the command that finds the file to open for a plugin. The look
// at the disk is a command, never Update's.
func pluginSourceCmd(in pluginapi.PluginInfo, initFile string) tea.Cmd {
	return func() tea.Msg {
		path, err := pluginSourcePath(in, initFile)
		return managerSourceMsg{name: in.Name, path: path, err: err}
	}
}

// pluginSourcePath finds the file a plugin runs from. A legacy row is the JavaScript file
// itself. init.lua is where the runtime reads it. Any other plugin reports its entry file
// (the runtime resolves it the way it loads it); a plugin of the binary or of a buffer has
// none.
func pluginSourcePath(in pluginapi.PluginInfo, initFile string) (string, error) {
	switch {
	case in.Status == "legacy":
		return regularFile(filepath.Join(in.Dir, in.Name))
	case in.Source == "init":
		return regularFile(initFile)
	case in.Source == "bundled":
		return "", errors.New("is part of the gvim binary and has no file on disk")
	case in.Source == "buffer":
		return "", errors.New("was loaded from a buffer and has no file")
	case in.File == "":
		return "", errors.New("has no source file")
	}
	return regularFile(in.File)
}

// regularFile returns path when it is a regular file.
func regularFile(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	return path, nil
}

// ---- Load Plugin from Buffer

// loadPluginFromBuffer sends the text of the active buffer to the runtime as a plugin
// named after the file. The runtime compiles it in Work and says what happened.
func (m *Model) loadPluginFromBuffer() tea.Cmd {
	doc := m.ed.Active()
	switch {
	case doc == nil:
		m.notice = noDocumentNotice
		return nil
	case isLargeDoc(doc):
		m.notice = "Load Plugin from Buffer: a large file cannot be loaded as a plugin"
		return nil
	}
	var b strings.Builder
	for i := range doc.LineCount() {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(doc.Line(i))
		if b.Len() > pluginSourceMax {
			m.notice = fmt.Sprintf("Load Plugin from Buffer: the buffer is larger than %d KiB", pluginSourceMax>>10)
			return nil
		}
	}
	name := filepath.Base(doc.Name())
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	return m.pluginControl(pluginapi.Control{Op: "load_text", Name: stem, Text: b.String()})
}
