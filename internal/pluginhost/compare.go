package pluginhost

import (
	"errors"
	"github.com/tbxark/gvim/document"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/pluginapi"
)

// Caps of one compare_buffers side (parity backlog #15). The text of both sides is
// read here and the diff runs in a Work, so the sides are bounded like
// gvim.diff.text: the compare_files plugin refuses rather than showing a partial
// diff, and a large-file document is refused before its lines are read.
const (
	compareMaxBytes = 16 << 20
	compareMaxLines = 1_000_000
)

// DiffSource returns the buffer in the plugin API's byte model: its lines, each
// line's byte start, the buffer's byte length and its content version. No text
// crosses into Lua. gvim's document bytes count one byte per line break (the
// buffer is LF-normalised; see buffer.ByteOffset), so the offsets line up with
// replace_range and the rest of the buffer API.
//
// A binary buffer is refused with CodeInvalid and one in large-file mode, or over
// the compare caps, with CodeTooLarge: a side-by-side view must not paint from
// partial text.
func (p *pluginEditor) DiffSource(id pluginapi.BufferID) (pluginapi.DiffSource, error) {
	d, err := p.doc(id)
	if err != nil {
		return pluginapi.DiffSource{}, err
	}
	if d.Binary() {
		return pluginapi.DiffSource{}, pluginapi.Errf(pluginapi.CodeInvalid, "buffer %d is binary", id)
	}
	b := d.Buffer
	if b.Large() {
		return pluginapi.DiffSource{}, pluginapi.Errf(pluginapi.CodeTooLarge, "buffer %d is in large-file mode", id)
	}
	n := b.Lines()
	if n > compareMaxLines {
		return pluginapi.DiffSource{}, pluginapi.Errf(pluginapi.CodeTooLarge, "buffer %d has more than %d lines", id, compareMaxLines)
	}
	src := pluginapi.DiffSource{
		Lines:      make([]string, n),
		Starts:     make([]int, n),
		LineEnding: d.LineEnding(),
		Version:    versionOf(d),
	}
	off := 0
	for i := range n {
		line := b.Line(i)
		src.Starts[i] = off
		src.Lines[i] = line
		off += len(line)
		if i < n-1 {
			off++ // the line break counts one byte in the document model
		}
		if off > compareMaxBytes {
			return pluginapi.DiffSource{}, pluginapi.Errf(pluginapi.CodeTooLarge, "buffer %d is larger than %d bytes", id, compareMaxBytes)
		}
	}
	src.Len = off
	return src, nil
}

// ReplaceRange replaces the bytes start..end of the buffer with text, as one edit
// and one undo step, through the same version-guarded atomic path every plugin
// edit uses. It returns false, changing nothing, when the buffer is read-only or
// binary, the range is outside the buffer, or expect names another content
// version; a closed buffer or another real failure is an error. The text is
// EOL-normalised for the document before it is inserted.
func (p *pluginEditor) ReplaceRange(id pluginapi.BufferID, start, end int, text string, expect *pluginapi.Version) (bool, error) {
	d, err := p.doc(id)
	if err != nil {
		return false, err
	}
	if d.Binary() || d.ReadOnly() {
		return false, nil
	}
	b := d.Buffer
	if b.Large() {
		// Byte offsets are not available on a large buffer, so a range names no text
		// that could be replaced atomically.
		return false, nil
	}
	total, err := b.ByteOffset(b.End())
	if err != nil {
		return false, nil
	}
	if start < 0 || end < start || end > total {
		return false, nil
	}
	if expect != nil && versionOf(d) != *expect {
		return false, nil
	}
	from, err := b.PosAtByteOffset(start)
	if err != nil {
		return false, nil
	}
	to, err := b.PosAtByteOffset(end)
	if err != nil {
		return false, nil
	}
	opts := document.EditOptions{}
	if expect != nil {
		opts.Expect = &document.Version{Gen: expect.Gen, Ver: expect.Ver}
	}
	edit := []document.LSPEdit{{From: from, To: to, Text: plugin.NormalizeEOL(text)}}
	err = p.ed.ApplyEdits(document.DocID(id), edit, opts)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, document.ErrStale), errors.Is(err, document.ErrReadOnly),
		errors.Is(err, document.ErrLargeFile), errors.Is(err, buffer.ErrLargeBuffer):
		return false, nil
	}
	return false, editorErr(err)
}
