package editor

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/encoding"
)

// ErrNotDecodable is returned by OpenFile, OpenPreview and reload_with_encoding
// when the bytes of a file do not decode in the encoding detected or named.
var ErrNotDecodable = errors.New("file cannot be decoded")

// ErrUnknownEncoding is returned by set_encoding and reload_with_encoding for a
// name that internal/encoding does not list.
var ErrUnknownEncoding = errors.New("unknown encoding")

// defaultCodec is the encoding of text that was not read from a file in another
// encoding, and the one a Document with no codec set saves in.
const defaultCodec = "utf-8"

// labels are the Encoding labels of the encodings that are not utf-8. utf-8 has
// its own rule in encodingLabel.
var labels = map[string]string{
	"utf-8-bom":    "UTF-8 BOM",
	"utf-16le":     "UTF-16 LE",
	"utf-16be":     "UTF-16 BE",
	"windows-1252": "Windows-1252",
	"iso-8859-1":   "Latin-1",
	"shift_jis":    "Shift-JIS",
	"windows-1250": "Windows-1250",
	"windows-1251": "Windows-1251",
	"gbk":          "GBK",
	"gb18030":      "GB18030",
	"euc-kr":       "EUC-KR",
}

// byteOrderMark is the U+FEFF that a byte-order mark decodes to. The text of a
// document never starts with one that came from a mark: decodeFile removes it,
// and the codec keeps the mark for the save.
const byteOrderMark = "\ufeff"

// bomCodecs are the codecs whose Encode writes a byte-order mark.
var bomCodecs = map[string]bool{"utf-8-bom": true, "utf-16le": true, "utf-16be": true}

// decodeFile returns the text of the file data and the name of the encoding it
// is read with. Valid UTF-8 is decoded as utf-8. A leading byte-order mark is
// removed from the text and the codec is utf-8-bom, so that the save writes the
// mark back. Other data gets the encoding that encoding.Detect picks, and is
// decoded with it. When that fails and the detected encoding is windows-1252,
// shift_jis is tried next, because Shift_JIS text can hold bytes that windows-1252
// leaves undefined. Detection looks at the first 8 KiB only, so when the detected
// encoding is another legacy one (a Windows code page or a CJK encoding) a later
// byte can be undefined in it; then windows-1252 and iso-8859-1 are tried, and
// iso-8859-1 defines every byte. Data that does not decode returns an error
// wrapping ErrNotDecodable. A byte-order mark or a windows-1252 verdict that fails is
// not retried in iso-8859-1, so broken UTF-16 and undefined windows-1252 bytes stay
// refused.
func decodeFile(data []byte) (text, codec string, err error) {
	if utf8.Valid(data) {
		if rest, ok := strings.CutPrefix(string(data), byteOrderMark); ok {
			return rest, "utf-8-bom", nil
		}
		return string(data), defaultCodec, nil
	}
	codec = encoding.Detect(data)
	candidates := []string{codec}
	switch codec {
	case "windows-1252":
		candidates = append(candidates, "shift_jis")
	case "windows-1250", "windows-1251", "gbk", "gb18030", "euc-kr", "shift_jis":
		candidates = append(candidates, "windows-1252", "iso-8859-1")
	}
	var first error
	for _, c := range candidates {
		text, err = encoding.Decode(c, data)
		if err == nil {
			return text, c, nil
		}
		if first == nil {
			first = err
		}
	}
	return "", "", fmt.Errorf("%w: %w", ErrNotDecodable, first)
}

// encodeText returns text in codec, with the byte-order mark the codec has. The
// codec writes the mark itself, so a leading U+FEFF in text is taken as that mark
// and not written twice. It fails when codec cannot represent text.
func encodeText(codec, text string) ([]byte, error) {
	if bomCodecs[codec] {
		text = strings.TrimPrefix(text, byteOrderMark)
	}
	return encoding.Encode(codec, text)
}

// codecName is the encoding the document saves in.
func (d *Document) codecName() string {
	if d.codec == "" {
		return defaultCodec
	}
	return d.codec
}

// encodingLabel is the label Encoding reports for text in codec. Utf-8 text with
// no byte above 0x7f is "ASCII", as it has always been reported. Utf-8 text with
// any other byte is "UTF-8".
func encodingLabel(codec, text string) string {
	if codec == "" || codec == defaultCodec {
		if isASCII(text) {
			return "ASCII"
		}
		return "UTF-8"
	}
	return labels[codec]
}

// isASCII reports whether s has no byte above 0x7f.
func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

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

// textSample is the text that the encoding label is judged by: all of the text of a
// document, and the head of a large document, as when the file was opened.
func (d *Document) textSample() string {
	if d.large != nil {
		return d.Buffer.TextPrefix(largeSampleSize)
	}
	return d.Buffer.Text()
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
	if d.large != nil && name != defaultCodec && name != "utf-8-bom" {
		// A large document is read and written as UTF-8 text, with or without a
		// byte-order mark. Any other encoding would transcode the whole file.
		return fmt.Errorf("set_encoding: %w", ErrLargeFile)
	}
	d.markMetaChanged(name != d.codecName())
	d.codec = name
	d.encoding = encodingLabel(name, d.textSample())
	e.setStatus("Encoding " + d.encoding)
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
	if d.large != nil || e.isLargePath(d.Path) {
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
	d.setBuffer(buffer.New(text))
	d.Cursor = d.Buffer.Clamp(d.Cursor)
	d.Sel = nil
	d.want = -1
	d.folded = nil
	d.setSaved(text)
	d.eol = eol
	d.ends = lineEnds(decoded)
	d.codec = name
	d.metaDirty = false
	d.encoding = encodingLabel(name, text)
	d.disk = disk
	e.setStatus("Reloaded as " + d.encoding)
	return nil
}
