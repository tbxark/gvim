package plugintest

import (
	"errors"
	"slices"

	"github.com/tbxark/gvim/pluginapi"
)

// The compare/diff half of the fake host: DiffSource and ReplaceRange for
// gvim.diff.compare_buffers / replace_range. The fake's byte model is the editor's:
// offsets count one byte per line break.

// DiffSource returns one fake buffer's lines, byte starts, length and version.
func (f *FakeHost) DiffSource(id pluginapi.BufferID) (pluginapi.DiffSource, error) {
	b := f.find(id)
	if b == nil {
		return pluginapi.DiffSource{}, bufClosedErr(id)
	}
	if b.Info.Large {
		return pluginapi.DiffSource{}, pluginapi.Errf(pluginapi.CodeTooLarge, "buffer %d is in large-file mode", id)
	}
	src := pluginapi.DiffSource{
		Lines:      slices.Clone(b.Lines),
		Starts:     make([]int, len(b.Lines)),
		LineEnding: b.Info.LineEnding,
		Version:    b.Info.Version,
	}
	off := 0
	for i, line := range b.Lines {
		src.Starts[i] = off
		off += len(line)
		if i < len(b.Lines)-1 {
			off++
		}
	}
	src.Len = off
	return src, nil
}

// ReplaceRange replaces the fake buffer's bytes start..end with text as one edit,
// reporting false for the read-only, large and out-of-range refusals.
func (f *FakeHost) ReplaceRange(id pluginapi.BufferID, start, end int, text string, expect *pluginapi.Version) (bool, error) {
	b := f.find(id)
	if b == nil {
		return false, bufClosedErr(id)
	}
	if b.Info.ReadOnly || b.Info.Large {
		return false, nil
	}
	src, err := f.DiffSource(id)
	if err != nil {
		return false, nil
	}
	if start < 0 || end < start || end > src.Len {
		return false, nil
	}
	if expect != nil && b.Info.Version != *expect {
		return false, nil
	}
	from, ok := fakePosOf(src, start)
	if !ok {
		return false, nil
	}
	to, ok := fakePosOf(src, end)
	if !ok {
		return false, nil
	}
	err = f.Edit(id, []pluginapi.Edit{{Range: pluginapi.Range{From: from, To: to}, Text: text}},
		pluginapi.EditOptions{Expect: expect})
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, errStale):
		return false, nil
	}
	var pe *pluginapi.Error
	if errors.As(err, &pe) && (pe.Code == pluginapi.CodeReadOnly || pe.Code == pluginapi.CodeStale) {
		return false, nil
	}
	return false, err
}

// errStale is the fake editor's stale-version error, if it has one of its own; the
// host edit reports CodeStale in a pluginapi.Error, which ReplaceRange handles above.
var errStale = errors.New("stale")

// fakePosOf is the API position of a byte offset in a source.
func fakePosOf(src pluginapi.DiffSource, off int) (pluginapi.Pos, bool) {
	line := 0
	for line < len(src.Lines) {
		next := src.Starts[line] + len(src.Lines[line]) + 1
		if off < next || line == len(src.Lines)-1 {
			break
		}
		line++
	}
	if line >= len(src.Lines) {
		return pluginapi.Pos{Line: len(src.Lines)}, off == src.Len
	}
	return pluginapi.Pos{Line: line, Col: off - src.Starts[line]}, true
}
