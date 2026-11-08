package tui

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/pluginapi"
)

// Line targets (tier-1c remainder): the plugin host storage and the editor behaviour of
// Fresh's setLineTargets. A target set is per buffer; a later SetLineTargets call replaces
// it (owner recorded so a plugin unload does not remove another plugin's set), the set is
// dropped when the buffer closes. Enter on a target line (Normal, count 1, no selection and
// no extra carets) or a single left click opens the target: the split labelled Into when
// live, else beside the pane that holds the targets, so a result list never replaces
// itself. Counts 2/3, extend-selection clicks and multi-caret mode keep their normal
// behaviour.

// lineTargetSet is one buffer's clickable lines, owned by the plugin that set them last.
type lineTargetSet struct {
	owner  string
	byLine map[int]pluginapi.LineTarget
}

// SetLineTargets replaces the buffer's line targets.
func (h *pluginHost) SetLineTargets(plugin string, id pluginapi.BufferID, targets []pluginapi.LineTarget) error {
	info, ok := h.ed().Buffer(id)
	if !ok {
		return pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", id)
	}
	if len(targets) > pluginapi.MaxLineTargets {
		return pluginapi.Errf(pluginapi.CodeLimit, "a buffer takes at most %d line targets", pluginapi.MaxLineTargets)
	}
	last := max(info.LineCount-1, 0)
	set := &lineTargetSet{owner: plugin, byLine: make(map[int]pluginapi.LineTarget, len(targets))}
	for i, t := range targets {
		if strings.TrimSpace(t.Path) == "" {
			return pluginapi.Errf(pluginapi.CodeInvalid, "target %d: an empty path", i+1)
		}
		if len(t.Path) > pluginapi.MaxLineTargetLen {
			return pluginapi.Errf(pluginapi.CodeLimit, "target %d: path longer than %d bytes", i+1, pluginapi.MaxLineTargetLen)
		}
		if len(t.Into) > pluginapi.MaxLineTargetInto {
			return pluginapi.Errf(pluginapi.CodeLimit, "target %d: split label longer than %d bytes", i+1, pluginapi.MaxLineTargetInto)
		}
		if t.Line < 0 || t.Line > last {
			return pluginapi.Errf(pluginapi.CodeInvalid, "target %d: line %d is outside the buffer (0..%d)", i+1, t.Line, last)
		}
		if t.Target < 0 {
			t.Target = 0
		}
		t.Into = cleanText(t.Into)
		set.byLine[t.Line] = t
	}
	m := h.m
	if m.plug.lineTargets == nil {
		m.plug.lineTargets = map[pluginapi.BufferID]*lineTargetSet{}
	}
	if len(set.byLine) == 0 {
		delete(m.plug.lineTargets, id)
		return nil
	}
	m.plug.lineTargets[id] = set
	return nil
}

// ClearLineTargets removes the buffer's targets when the caller owns them.
func (h *pluginHost) ClearLineTargets(plugin string, id pluginapi.BufferID) {
	set := h.m.plug.lineTargets[id]
	if set == nil || set.owner != plugin {
		return
	}
	delete(h.m.plug.lineTargets, id)
}

// lineTargetAt returns the target on a line of the document, when it is a plugin buffer
// with targets.
func (m *Model) lineTargetAt(doc Document, line int) (pluginapi.LineTarget, bool) {
	bd, ok := doc.(bufferIdentified)
	if !ok {
		return pluginapi.LineTarget{}, false
	}
	set := m.plug.lineTargets[pluginapi.BufferID(bd.BufferID())]
	if set == nil {
		return pluginapi.LineTarget{}, false
	}
	t, ok := set.byLine[line]
	return t, ok
}

// plugLineTargetsDrop forgets a closed buffer's targets.
func (m *Model) plugLineTargetsDrop(id pluginapi.BufferID) { delete(m.plug.lineTargets, id) }

// plugLineTargetsDropOwner forgets the targets a plugin set (another plugin's later set
// stays).
func (m *Model) plugLineTargetsDropOwner(plugin string) {
	for id, set := range m.plug.lineTargets {
		if set.owner == plugin {
			delete(m.plug.lineTargets, id)
		}
	}
}

// openLineTarget opens what a line target points at: the labelled split when it is live,
// else beside the pane that holds the source buffer; the caret lands on Target.
func (m *Model) openLineTarget(t pluginapi.LineTarget, source Document) tea.Cmd {
	path := t.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(m.rootDir(), path)
	}
	// A live labelled split takes the target; otherwise it opens beside the pane that
	// holds the source buffer, never replacing the result list itself.
	placed := false
	if t.Into != "" {
		if id, ok := m.plug.host.SplitByLabel(t.Into); ok && id != 0 {
			if err := m.plug.host.FocusWindow(id); err == nil {
				placed = true
			}
		}
	}
	if !placed && m.ed.Active() == source {
		m.trySplit(sideBySide)
	}
	line := t.Target
	if line < 0 {
		line = 0
	}
	cmd, err := m.openFileCmdErr(path, false, line, 0)
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	return cmd
}

// lineTargetAction runs the line target under the caret for the current action, reporting
// whether it took it. Only the plain Normal insert_newline (Enter) is claimed: counts above
// one, a selection and extra carets keep their editing behaviour.
func (m *Model) lineTargetAction(action string, args map[string]any) (tea.Cmd, bool) {
	if action != "insert_newline" || m.ed.Active() == nil {
		return nil, false
	}
	if c, _ := args["count"].(int); c > 1 {
		return nil, false
	}
	if _, _, ok := m.ed.Active().Selection(); ok {
		return nil, false
	}
	if cl, ok := m.ed.Active().(editorpane.CaretLister); ok && len(cl.Cursors()) > 0 {
		return nil, false
	}
	line := m.ed.Active().Cursor().Line
	t, ok := m.lineTargetAt(m.ed.Active(), line)
	if !ok {
		return nil, false
	}
	return m.openLineTarget(t, m.ed.Active()), true
}
