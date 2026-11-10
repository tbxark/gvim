package plugintest

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tbxark/gvim/pluginapi"
)

// The virtual-buffer half of the fake host (stage S18): CreateBuffer, SetContent, PropsAt,
// SetReadOnly, SetBufferMode and the per-buffer options. It follows the rules of the real
// host (internal/app/pluginvirtual.go): a virtual buffer is never modified and cannot be
// saved or reverted, SetContent replaces text, props and styles in one step and gives the
// buffer a new generation, props answer only for the content state they were set for, and
// the ui closes a plugin's virtual buffers when the plugin goes (OwnerGone).

const virtualExtKey = "virtual"

// FakeMaxVirtual is the number of virtual buffers the fake allows at once.
const FakeMaxVirtual = 256

// propSpan is the properties of one run of content, [from, to) in lines and byte columns.
type propSpan struct {
	fromLine, fromCol, toLine, toCol int
	props                            map[string]any
}

// StyledRun is a styled range of the content a plugin set, in lines and byte columns.
type StyledRun struct {
	FromLine, FromCol, ToLine, ToCol int
	Style                            pluginapi.Style
	Priority                         int
}

type virtualState struct {
	owner   string
	entries []pluginapi.ContentEntry
	runs    []StyledRun
	props   []propSpan
	propsAt pluginapi.Version
}

type virtualExt struct {
	st   map[pluginapi.BufferID]*virtualState  // the virtual buffers
	opts map[pluginapi.BufferID]map[string]any // per-buffer options of any buffer
}

func (f *FakeHost) vx() *virtualExt {
	if x, ok := f.Ext[virtualExtKey].(*virtualExt); ok {
		return x
	}
	x := &virtualExt{st: map[pluginapi.BufferID]*virtualState{}, opts: map[pluginapi.BufferID]map[string]any{}}
	f.Ext[virtualExtKey] = x
	return x
}

func init() {
	RegisterResourceKind(func(f *FakeHost) []string {
		x, ok := f.Ext[virtualExtKey].(*virtualExt)
		if !ok {
			return nil
		}
		var out []string
		for id, s := range x.st {
			if f.find(id) != nil {
				out = append(out, fmt.Sprintf("vbuf:%s/%d", s.owner, id))
			}
		}
		return out
	})
	OnOwnerGone(func(f *FakeHost, plugin string, _ uint64) {
		x, ok := f.Ext[virtualExtKey].(*virtualExt)
		if !ok {
			return
		}
		for id, s := range x.st {
			if s.owner != plugin {
				continue
			}
			if f.find(id) != nil {
				f.RemoveBuffer(id)
				delete(f.bx().st, id)
			}
			delete(x.st, id)
			delete(x.opts, id)
		}
	})
}

// VirtualContent returns the entries of the last SetContent of a virtual buffer, and the
// styled runs they gave.
func (f *FakeHost) VirtualContent(id pluginapi.BufferID) ([]pluginapi.ContentEntry, []StyledRun) {
	if s := f.vx().st[id]; s != nil {
		return s.entries, s.runs
	}
	return nil, nil
}

func (f *FakeHost) virtual(id pluginapi.BufferID) (*FakeBuffer, *virtualState, error) {
	b := f.find(id)
	if b == nil {
		return nil, nil, bufClosedErr(id)
	}
	s := f.vx().st[id]
	if s == nil || !b.Info.Virtual {
		return nil, nil, pluginapi.Errf(pluginapi.CodeInvalid, "buffer %d is not a virtual buffer", id)
	}
	return b, s, nil
}

func fakeCleanText(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// CreateBuffer opens a virtual buffer as the last tab.
func (f *FakeHost) CreateBuffer(o pluginapi.CreateBuffer) (pluginapi.BufferID, error) {
	f.rec("CreateBuffer", o)
	if strings.TrimSpace(o.Name) == "" {
		return 0, pluginapi.Errf(pluginapi.CodeInvalid, "a virtual buffer needs a name")
	}
	x := f.vx()
	live := 0
	for id := range x.st {
		if f.find(id) != nil {
			live++
		}
	}
	if live >= FakeMaxVirtual {
		return 0, pluginapi.Errf(pluginapi.CodeLimit, "too many virtual buffers (max %d)", FakeMaxVirtual)
	}
	id := f.AddBuffer("", fakeCleanText(o.Text))
	b := f.find(id)
	b.Info.Name = "*" + strings.TrimSpace(o.Name) + "*"
	b.Info.Virtual, b.Info.ReadOnly = true, o.ReadOnly
	b.Info.Language, b.Info.Mode = o.Language, o.Mode
	x.st[id] = &virtualState{owner: o.Plugin}
	if o.Focus {
		f.SetActive(id)
	}
	return id, nil
}

// SetReadOnly sets the read-only flag of a virtual buffer.
func (f *FakeHost) SetReadOnly(id pluginapi.BufferID, ro bool) error {
	f.rec("SetReadOnly", id, ro)
	b, _, err := f.virtual(id)
	if err != nil {
		return err
	}
	b.Info.ReadOnly = ro
	return nil
}

// SetBufferMode sets the buffer-local mode of any buffer.
func (f *FakeHost) SetBufferMode(id pluginapi.BufferID, mode string) error {
	f.rec("SetBufferMode", id, mode)
	b := f.find(id)
	if b == nil {
		return bufClosedErr(id)
	}
	b.Info.Mode = mode
	return nil
}

// SetOption sets a per-buffer option; nil clears it.
func (f *FakeHost) SetOption(id pluginapi.BufferID, name string, v any) error {
	f.rec("SetOption", id, name, v)
	if f.find(id) == nil {
		return bufClosedErr(id)
	}
	switch name {
	case "wrap", "line_numbers":
		if _, ok := v.(bool); v != nil && !ok {
			return pluginapi.Errf(pluginapi.CodeInvalid, "option %q takes a boolean", name)
		}
	case "tab_size":
		if n, ok := v.(float64); v != nil && (!ok || n < 1 || n > 16 || n != float64(int(n))) {
			return pluginapi.Errf(pluginapi.CodeInvalid, "option %q takes a whole number from 1 to 16", name)
		}
	default:
		return pluginapi.Errf(pluginapi.CodeInvalid, "unknown option %q (wrap, line_numbers, tab_size)", name)
	}
	opts := f.optionsOf(id)
	if v == nil {
		delete(opts, name)
	} else {
		opts[name] = v
	}
	return nil
}

// GetOption returns the override a buffer has for an option, or nil.
func (f *FakeHost) GetOption(id pluginapi.BufferID, name string) (any, error) {
	f.rec("GetOption", id, name)
	if f.find(id) == nil {
		return nil, bufClosedErr(id)
	}
	switch name {
	case "wrap", "line_numbers", "tab_size":
		return f.optionsOf(id)[name], nil
	}
	return nil, pluginapi.Errf(pluginapi.CodeInvalid, "unknown option %q (wrap, line_numbers, tab_size)", name)
}

// optionsOf returns the option map of any buffer (virtual or not).
func (f *FakeHost) optionsOf(id pluginapi.BufferID) map[string]any {
	x := f.vx()
	if x.opts[id] == nil {
		x.opts[id] = map[string]any{}
	}
	return x.opts[id]
}

type fakePos struct{ line, col int }

func (p fakePos) after(s string) fakePos {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return fakePos{p.line + strings.Count(s, "\n"), len(s) - i - 1}
	}
	return fakePos{p.line, p.col + len(s)}
}

// SetContent replaces the text of a virtual buffer with the entries, concatenated, and the
// styles and props that go with them.
func (f *FakeHost) SetContent(id pluginapi.BufferID, entries []pluginapi.ContentEntry) error {
	f.rec("SetContent", id, len(entries))
	if f.SetContentErr != nil {
		return f.SetContentErr
	}
	b, s, err := f.virtual(id)
	if err != nil {
		return err
	}
	var text strings.Builder
	var runs []StyledRun
	var props []propSpan
	at := fakePos{}
	style := func(start fakePos, piece string, st pluginapi.Style, prio int) {
		if st == (pluginapi.Style{}) {
			return
		}
		end := start.after(strings.TrimSuffix(piece, "\n"))
		if end != start {
			runs = append(runs, StyledRun{start.line, start.col, end.line, end.col, st, prio})
		}
	}
	for _, e := range entries {
		start, off := at, text.Len()
		if len(e.Segments) == 0 {
			t := fakeCleanText(e.Text)
			text.WriteString(t)
			at = at.after(t)
		}
		for _, seg := range e.Segments {
			t, segAt := fakeCleanText(seg.Text), at
			text.WriteString(t)
			at = at.after(t)
			style(segAt, t, seg.Style, 1)
		}
		style(start, text.String()[off:], e.Style, 0)
		if len(e.Props) > 0 && at != start {
			props = append(props, propSpan{start.line, start.col, at.line, at.col, e.Props})
		}
	}
	if text.Len() > 16<<20 {
		return pluginapi.Errf(pluginapi.CodeTooLarge, "content is over %d bytes", 16<<20)
	}
	old := f.bstate(b)
	oldCaret := old.carets[0].Pos
	b.Lines = strings.Split(text.String(), "\n")
	old.undo, old.redo = nil, nil
	old.saved = slices.Clone(b.Lines)
	b.Info.Version = pluginapi.Version{Gen: b.Info.Version.Gen + 1}
	f.refreshInfo(b)
	old.carets = []pluginapi.Caret{{Pos: f.clampPos(b, oldCaret)}}
	s.entries, s.runs, s.props, s.propsAt = slices.Clone(entries), runs, props, b.Info.Version
	return nil
}

// PropsAt returns the props of the content run that covers p; a buffer that is not
// virtual, or was edited since SetContent, has none.
func (f *FakeHost) PropsAt(id pluginapi.BufferID, p pluginapi.Pos) (map[string]any, error) {
	f.rec("PropsAt", id, p)
	b := f.find(id)
	if b == nil {
		return nil, bufClosedErr(id)
	}
	s := f.vx().st[id]
	if s == nil || !b.Info.Virtual || s.propsAt != b.Info.Version {
		return nil, nil
	}
	last := len(b.Lines) - 1
	line, col := max(p.Line, 0), max(p.Col, 0)
	if line > last {
		line, col = last, len(b.Lines[last])
	}
	atEnd := line == last && col == len(b.Lines[last])
	for _, sp := range s.props {
		afterFrom := line > sp.fromLine || (line == sp.fromLine && col >= sp.fromCol)
		beforeTo := line < sp.toLine || (line == sp.toLine && col < sp.toCol)
		onTo := line == sp.toLine && col == sp.toCol
		if afterFrom && (beforeTo || (onTo && atEnd)) {
			return sp.props, nil
		}
	}
	return nil, nil
}
