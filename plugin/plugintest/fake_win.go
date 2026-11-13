package plugintest

import (
	"slices"

	"github.com/tbxark/gvim/pluginapi"
)

// The window half of the fake host: a list of windows in reading order over the
// fake's buffers. It starts with one window that fills the screen under a three-row chrome
// and shows whatever buffer is active; a split cuts a window in two as the ui does (one
// border cell between them). Sizes are the text area; the fake has no gutter or scrollbar.
// What the real host measures from the painter (rows, wrapped lines) is set by tests with
// SetVisible.

// FakeWindow is one window of the fake.
type FakeWindow struct {
	ID                  pluginapi.WinID
	Buffer              pluginapi.BufferID
	X, Y, Width, Height int
	TopLine, LeftCol    int
	// Visible are the rows VisibleRows returns; nil lists one row for each line from
	// TopLine, as many as the window has rows.
	Visible []pluginapi.VisibleRow
}

// FakeWindows is the state behind the WindowHost methods of the fake.
type FakeWindows struct {
	// Wins are the windows in reading order.
	Wins []*FakeWindow
	// Current is the id of the focused window.
	Current pluginapi.WinID
	// Equalized counts the Equalize calls.
	Equalized int
	// ListCalls counts the Windows calls.
	ListCalls int

	next pluginapi.WinID
}

// Win returns the window state of the fake, creating the first window on first use.
func (f *FakeHost) Win() *FakeWindows {
	w, ok := f.Ext["win"].(*FakeWindows)
	if !ok {
		w = &FakeWindows{next: 1}
		f.Ext["win"] = w
		first := &FakeWindow{ID: 1, Y: 1, Width: f.Screen[0], Height: max(f.Screen[1]-3, 1)}
		w.next, w.Current = 2, 1
		w.Wins = []*FakeWindow{first}
	}
	return w
}

// Window returns the window with id, or nil. The focused window shows the active buffer.
func (f *FakeHost) Window(id pluginapi.WinID) *FakeWindow {
	w := f.Win()
	for _, x := range w.Wins {
		if x.ID == id {
			if id == w.Current {
				x.Buffer = f.active
			}
			return x
		}
	}
	return nil
}

// SetVisible sets the rows VisibleRows returns for window id.
func (f *FakeHost) SetVisible(id pluginapi.WinID, rows []pluginapi.VisibleRow) {
	if w := f.Window(id); w != nil {
		w.Visible = rows
	}
}

func winClosedErr(id pluginapi.WinID) error {
	return pluginapi.Errf(pluginapi.CodeClosed, "window %d is closed", id)
}

// Windows returns the windows in reading order.
func (f *FakeHost) Windows() []pluginapi.WindowInfo {
	ws := f.Win()
	ws.ListCalls++
	out := make([]pluginapi.WindowInfo, len(ws.Wins))
	for i, w := range ws.Wins {
		f.Window(w.ID) // the focused window shows the active buffer
		out[i] = pluginapi.WindowInfo{
			ID: w.ID, Buffer: w.Buffer, X: w.X, Y: w.Y, Width: w.Width, Height: w.Height,
			TopLine: w.TopLine, LeftCol: w.LeftCol, Active: w.ID == ws.Current,
		}
	}
	return out
}

// CurrentWindow returns the focused window.
func (f *FakeHost) CurrentWindow() pluginapi.WinID { return f.Win().Current }

// SetWindowBuffer shows the buffer in the window; in the focused one it activates it.
func (f *FakeHost) SetWindowBuffer(id pluginapi.WinID, b pluginapi.BufferID) error {
	f.rec("SetWindowBuffer", id, b)
	w := f.Window(id)
	switch {
	case w == nil:
		return winClosedErr(id)
	case f.find(b) == nil:
		return bufClosedErr(b)
	}
	w.Buffer, w.TopLine = b, 0
	if id == f.Win().Current {
		f.SetActive(b)
	}
	return nil
}

// FocusWindow focuses the window and activates its buffer.
func (f *FakeHost) FocusWindow(id pluginapi.WinID) error {
	f.rec("FocusWindow", id)
	w := f.Window(id)
	if w == nil {
		return winClosedErr(id)
	}
	f.Win().Current = id
	if w.Buffer != 0 && f.find(w.Buffer) != nil {
		f.SetActive(w.Buffer)
	}
	return nil
}

// CloseWindow closes the window; the last one stays. The focus moves to the window that
// takes its place in reading order.
func (f *FakeHost) CloseWindow(id pluginapi.WinID) error {
	f.rec("CloseWindow", id)
	ws := f.Win()
	i := slices.IndexFunc(ws.Wins, func(w *FakeWindow) bool { return w.ID == id })
	switch {
	case i < 0:
		return winClosedErr(id)
	case len(ws.Wins) == 1:
		return pluginapi.Errf(pluginapi.CodeConflict, "the last window cannot be closed")
	}
	gone := ws.Wins[i]
	ws.Wins = slices.Delete(ws.Wins, i, i+1)
	for _, o := range ws.Wins { // a neighbour of the same width or height takes the room back
		switch {
		case o.X == gone.X && o.Width == gone.Width && (o.Y+o.Height+1 == gone.Y || gone.Y+gone.Height+1 == o.Y):
			o.Y, o.Height = min(o.Y, gone.Y), o.Height+gone.Height+1
		case o.Y == gone.Y && o.Height == gone.Height && (o.X+o.Width+1 == gone.X || gone.X+gone.Width+1 == o.X):
			o.X, o.Width = min(o.X, gone.X), o.Width+gone.Width+1
		default:
			continue
		}
		break
	}
	if ws.Current == id {
		next := ws.Wins[min(i, len(ws.Wins)-1)]
		ws.Current = next.ID
		if next.Buffer != 0 && f.find(next.Buffer) != nil {
			f.SetActive(next.Buffer)
		}
	}
	return nil
}

// SplitWindow cuts the window in two. The new window shows o.Buffer (the window's own when
// zero) and is focused only with o.Focus.
func (f *FakeHost) SplitWindow(id pluginapi.WinID, o pluginapi.SplitOptions) (pluginapi.WinID, error) {
	f.rec("SplitWindow", id, o)
	ws := f.Win()
	src := f.Window(id)
	switch {
	case src == nil:
		return 0, winClosedErr(id)
	case o.Dir != "right" && o.Dir != "below":
		return 0, pluginapi.Errf(pluginapi.CodeInvalid, "split dir must be \"right\" or \"below\", got %q", o.Dir)
	case o.Ratio != 0 && !(o.Ratio > 0 && o.Ratio < 1):
		return 0, pluginapi.Errf(pluginapi.CodeInvalid, "split ratio must be between 0 and 1")
	case o.Buffer != 0 && f.find(o.Buffer) == nil:
		return 0, bufClosedErr(o.Buffer)
	}
	ratio := o.Ratio
	if ratio == 0 {
		ratio = 0.5
	}
	nw := &FakeWindow{ID: ws.next, Buffer: src.Buffer, X: src.X, Y: src.Y, Width: src.Width, Height: src.Height}
	if o.Buffer != 0 {
		nw.Buffer = o.Buffer
	}
	// cut divides extent between the new and the old window, with one cell for the border.
	cut := func(extent int) (newSize, oldSize int, ok bool) {
		if extent < 3 {
			return 0, 0, false
		}
		n := min(max(int(float64(extent-1)*ratio+0.5), 1), extent-2)
		return n, extent - 1 - n, true
	}
	if o.Dir == "right" {
		n, old, ok := cut(src.Width)
		if !ok {
			return 0, pluginapi.Errf(pluginapi.CodeLimit, "Not enough room to split")
		}
		nw.Width, src.Width = n, old
		if o.Before {
			nw.X, src.X = src.X, src.X+n+1
		} else {
			nw.X = src.X + old + 1
		}
	} else {
		n, old, ok := cut(src.Height)
		if !ok {
			return 0, pluginapi.Errf(pluginapi.CodeLimit, "Not enough room to split")
		}
		nw.Height, src.Height = n, old
		if o.Before {
			nw.Y, src.Y = src.Y, src.Y+n+1
		} else {
			nw.Y = src.Y + old + 1
		}
	}
	ws.next++
	i := slices.Index(ws.Wins, src)
	if !o.Before {
		i++
	}
	ws.Wins = slices.Insert(ws.Wins, i, nw)
	if o.Focus {
		src.Buffer = f.active // the old focused window keeps what it showed
		ws.Current = nw.ID
		if nw.Buffer != 0 {
			f.SetActive(nw.Buffer)
		}
	}
	return nw.ID, nil
}

// ScrollWindow sets the top line: the line itself, or with center the line in the middle of
// the rows. It is clamped to the buffer.
func (f *FakeHost) ScrollWindow(id pluginapi.WinID, line int, center bool) error {
	f.rec("ScrollWindow", id, line, center)
	w := f.Window(id)
	if w == nil {
		return winClosedErr(id)
	}
	top := line
	if center {
		top -= max(w.Height-1, 0) / 2
	}
	last := 0
	if b := f.find(w.Buffer); b != nil {
		last = max(len(b.Lines)-1, 0)
	}
	w.TopLine = min(max(top, 0), last)
	return nil
}

// Equalize counts the call.
func (f *FakeHost) Equalize() error {
	f.rec("Equalize")
	f.Win().Equalized++
	return nil
}

// VisibleRows returns the rows set with SetVisible, or one row for each line from the top.
func (f *FakeHost) VisibleRows(id pluginapi.WinID) ([]pluginapi.VisibleRow, error) {
	w := f.Window(id)
	if w == nil {
		return nil, winClosedErr(id)
	}
	if w.Visible != nil {
		return slices.Clone(w.Visible), nil
	}
	b := f.find(w.Buffer)
	if b == nil {
		return nil, nil
	}
	var rows []pluginapi.VisibleRow
	for l := w.TopLine; l < len(b.Lines) && len(rows) < w.Height; l++ {
		rows = append(rows, pluginapi.VisibleRow{Line: l, ColTo: len(b.Lines[l])})
	}
	return rows, nil
}
