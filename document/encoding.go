package document

import (
	"errors"
	"fmt"
	"github.com/tbxark/gvim/encoding"
	"strings"
	"unicode/utf8"
)

// ErrNotDecodable is returned by OpenFile, OpenPreview and reload_with_encoding
// when the bytes of a file do not decode in the encoding detected or named.
var ErrNotDecodable = errors.New("file cannot be decoded")

// ErrUnknownEncoding is returned by set_encoding and reload_with_encoding for a
// name that encoding does not list.
var ErrUnknownEncoding = errors.New("unknown encoding")

// defaultCodec is the encoding of text that was not read from a file in another
// encoding, and the one a Document with no codec set saves in.
const defaultCodec = "utf-8"

// labels are the Encoding labels of the encodings that are not utf-8. utf-8 has
// its own rule in encodingLabel.
var labels = map[string]string{
	"utf-8-bom": "UTF-8 BOM",
	"utf-16le":  "UTF-16 LE",
	"utf-16be":  "UTF-16 BE",

	"utf-16le-nobom": "UTF-16 LE",
	"utf-16be-nobom": "UTF-16 BE",
	"windows-1252":   "Windows-1252",
	"iso-8859-1":     "Latin-1",
	"shift_jis":      "Shift-JIS",
	"windows-1250":   "Windows-1250",
	"windows-1251":   "Windows-1251",
	"gbk":            "GBK",
	"gb18030":        "GB18030",
	"euc-kr":         "EUC-KR",
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
			if !hasUTF16BOM(data) {
				// The file has no mark, so the save must not add one.
				c = encoding.NoBOM(c)
			}
			return text, c, nil
		}
		if first == nil {
			first = err
		}
	}
	return "", "", fmt.Errorf("%w: %w", ErrNotDecodable, first)
}

// hasUTF16BOM reports whether data starts with a UTF-16 byte-order mark.
func hasUTF16BOM(data []byte) bool {
	return len(data) >= 2 && (data[0] == 0xff && data[1] == 0xfe || data[0] == 0xfe && data[1] == 0xff)
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

// CodecName is the encoding the document saves in.
func (d *Document) CodecName() string {
	if d.Codec == "" {
		return defaultCodec
	}
	return d.Codec
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

// TextSample is the text that the encoding label is judged by: all of the text of a
// document, and the head of a large document, as when the file was opened.
func (d *Document) TextSample() string {
	if d.large != nil {
		return d.Buffer.TextPrefix(largeSampleSize)
	}
	return d.Buffer.Text()
}
