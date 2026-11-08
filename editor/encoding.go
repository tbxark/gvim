package editor

import (
	"fmt"
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/encoding"
	"os"
	"slices"
)

// codecArg reads the "name" argument of an encoding action. The name must be one
// that internal/encoding lists.
func codecArg(action Action, args map[string]any) (string, error) {
	name, ok := args["name"].(string)
	if !ok {
		return "", fmt.Errorf("%s: missing string argument \"name\"", action)
	}
	if !slices.Contains(encoding.Names(), name) {
		return "", fmt.Errorf("%s: %w %q", action, ErrUnknownEncoding, name)
	}
	return name, nil
}

// setEncoding changes the encoding that the next save uses. The text is kept, and
// a new encoding marks the document modified. A save that the new encoding cannot
// hold fails, and the buffer stays modified. A large document takes only utf-8 and
// utf-8-bom, and any other name returns ErrLargeFile.
func setEncoding(e *Editor, d *Document, args map[string]any) error {
	name, err := codecArg("set_encoding", args)
	if err != nil {
		return err
	}
	if d.LargeState() != nil && name != defaultCodec && name != "utf-8-bom" {
		// A large document is read and written as UTF-8 text, with or without a
		// byte-order mark. Any other encoding would transcode the whole file.
		return fmt.Errorf("set_encoding: %w", ErrLargeFile)
	}
	d.MarkMetaChanged(name != d.CodecName())
	d.Codec = name
	d.SetEncodingLabel(encodingLabel(name, d.TextSample()))
	e.setStatus("Encoding " + d.EncodingLabel())
	return nil
}

// reloadWithEncoding reads the file again from disk and decodes it with the named
// encoding. The buffer takes the new text, and its undo history starts again. An
// untitled buffer returns ErrNoPath. A buffer with unsaved edits returns an error
// wrapping ErrUnsaved, unless force is set, because the edits would be lost.
func reloadWithEncoding(e *Editor, d *Document, args map[string]any) error {
	name, err := codecArg("reload_with_encoding", args)
	if err != nil {
		return err
	}
	if d.Path == "" {
		return fmt.Errorf("reload: %w", ErrNoPath)
	}
	// The reload reads the whole file. That is not done for a document in large file
	// mode, nor for a file that has grown into one since it was opened.
	if d.LargeState() != nil || e.isLargePath(d.Path) {
		return fmt.Errorf("reload_with_encoding: %w", ErrLargeFile)
	}
	// A changed encoding or line ending alone is given up by the reload.
	if d.Buffer.Modified() && !forced(args) {
		return fmt.Errorf("%s: %w", d.Name(), ErrUnsaved)
	}
	// The disk state is taken before the read, as readDocument does.
	disk := diskStateOf(d.Path)
	data, err := os.ReadFile(d.Path)
	if err != nil {
		return fmt.Errorf("reload %s: %w", d.Path, err)
	}
	decoded, err := encoding.Decode(name, data)
	if err != nil {
		return fmt.Errorf("reload %s: %w: %w", d.Path, ErrNotDecodable, err)
	}
	text, eol := splitLineEnding(decoded)
	_ = d.Buffer.Close()
	d.SetBuffer(buffer.New(text))
	d.Cursor = d.Buffer.Clamp(d.Cursor)
	d.Sel = nil
	d.Want = -1
	d.SetFoldedLines(nil)
	d.SetSaved(text)
	d.Eol = eol
	d.Ends = lineEnds(decoded)
	d.Codec = name
	d.MetaDirty = false
	d.SetEncodingLabel(encodingLabel(name, text))
	d.Disk = disk
	e.setStatus("Reloaded as " + d.EncodingLabel())
	return nil
}
