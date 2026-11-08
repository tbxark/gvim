package encoding

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"
	"unicode/utf16"
	"unicode/utf8"

	textenc "golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

var (
	names      = []string{"utf-8", "utf-8-bom", "utf-16le", "utf-16be", "windows-1252", "iso-8859-1", "shift_jis", "windows-1250", "windows-1251", "gbk", "gb18030", "euc-kr"}
	utf8BOM    = []byte{0xef, 0xbb, 0xbf}
	utf16LEBOM = []byte{0xff, 0xfe}
	utf16BEBOM = []byte{0xfe, 0xff}
)

// byteOrder is a byte order that reads and appends 16-bit units.
type byteOrder interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

// utf16Kind is a UTF-16 byte order and whether its files carry a byte-order mark.
// The "-nobom" names are not in names: they are what a file that was read without a
// mark is saved as, so that the save does not add one. They are not offered to the user.
type utf16Kind struct {
	order byteOrder
	bom   []byte
}

var utf16Kinds = map[string]utf16Kind{
	"utf-16le":       {binary.LittleEndian, utf16LEBOM},
	"utf-16be":       {binary.BigEndian, utf16BEBOM},
	"utf-16le-nobom": {binary.LittleEndian, nil},
	"utf-16be-nobom": {binary.BigEndian, nil},
}

// NoBOM returns the name of the UTF-16 encoding with the byte order of name that
// writes no byte-order mark, or name when it is not "utf-16le" or "utf-16be".
func NoBOM(name string) string {
	switch name {
	case "utf-16le", "utf-16be":
		return name + "-nobom"
	}
	return name
}

// Names returns the encoding names that Decode and Encode accept, without the
// "-nobom" UTF-16 names (see NoBOM).
func Names() []string {
	return slices.Clone(names)
}

// Detect returns the name of the encoding of data. A byte-order mark decides
// first. Otherwise valid UTF-8 is "utf-8". Any other data goes to detectLegacy,
// which guesses UTF-16 without a mark, a CJK encoding or a Windows code page, and
// answers "windows-1252" when nothing else fits.
func Detect(data []byte) string {
	switch {
	case bytes.HasPrefix(data, utf8BOM):
		return "utf-8-bom"
	case bytes.HasPrefix(data, utf16LEBOM):
		return "utf-16le"
	case bytes.HasPrefix(data, utf16BEBOM):
		return "utf-16be"
	case utf8.Valid(data):
		return "utf-8"
	default:
		return detectLegacy(data)
	}
}

// Decode converts data in the named encoding to a string. The byte-order mark
// of the named encoding is consumed when present. Bytes the encoding does not
// define are an error, not replaced with U+FFFD.
func Decode(name string, data []byte) (string, error) {
	switch name {
	case "utf-8":
		return decodeUTF8(name, data)
	case "utf-8-bom":
		return decodeUTF8(name, bytes.TrimPrefix(data, utf8BOM))
	}
	if k, ok := utf16Kinds[name]; ok {
		return decodeUTF16(name, bytes.TrimPrefix(data, k.bom), k.order)
	}
	codec := table(name)
	if codec == nil {
		return "", unknownName(name)
	}
	out, _, err := transform.Bytes(codec.NewDecoder(), data)
	if err != nil {
		return "", fmt.Errorf("encoding: decode %s: %w", name, err)
	}
	// The x/text decoders write U+FFFD for bytes they do not define. None of the
	// tables maps a byte sequence to U+FFFD, so its presence means the input
	// was invalid.
	if bytes.ContainsRune(out, utf8.RuneError) {
		return "", invalidBytes(name)
	}
	return string(out), nil
}

// Encode converts text to bytes in the named encoding. "utf-8-bom" and the
// UTF-16 names, but not their "-nobom" forms, write their byte-order mark first. Text the encoding cannot
// represent, including invalid UTF-8, is an error.
func Encode(name string, text string) ([]byte, error) {
	switch name {
	case "utf-8":
		return encodeUTF8(name, text, nil)
	case "utf-8-bom":
		return encodeUTF8(name, text, utf8BOM)
	}
	if k, ok := utf16Kinds[name]; ok {
		return encodeUTF16(name, text, k.order, k.bom != nil)
	}
	codec := table(name)
	if codec == nil {
		return nil, unknownName(name)
	}
	out, _, err := transform.Bytes(codec.NewEncoder(), []byte(text))
	if err != nil {
		return nil, fmt.Errorf("encoding: encode %s: %w", name, err)
	}
	return out, nil
}

// table returns the x/text codec for a legacy (non-Unicode) name, or nil.
func table(name string) textenc.Encoding {
	switch name {
	case "windows-1252":
		return charmap.Windows1252
	case "iso-8859-1":
		return charmap.ISO8859_1
	case "shift_jis":
		return japanese.ShiftJIS
	case "windows-1250":
		return charmap.Windows1250
	case "windows-1251":
		return charmap.Windows1251
	case "gbk":
		return simplifiedchinese.GBK
	case "gb18030":
		return simplifiedchinese.GB18030
	case "euc-kr":
		return korean.EUCKR
	}
	return nil
}

func decodeUTF8(name string, data []byte) (string, error) {
	if !utf8.Valid(data) {
		return "", invalidBytes(name)
	}
	return string(data), nil
}

// decodeUTF16 decodes data whose code units are in order. A lone surrogate is an
// error. A trailing odd byte is an error.
func decodeUTF16(name string, data []byte, order binary.ByteOrder) (string, error) {
	if len(data)%2 != 0 {
		return "", invalidBytes(name)
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = order.Uint16(data[2*i:])
	}
	runes := make([]rune, 0, len(units))
	for i := 0; i < len(units); i++ {
		u := rune(units[i])
		if !utf16.IsSurrogate(u) {
			runes = append(runes, u)
			continue
		}
		if i+1 >= len(units) {
			return "", invalidBytes(name)
		}
		r := utf16.DecodeRune(u, rune(units[i+1]))
		if r == utf8.RuneError {
			return "", invalidBytes(name)
		}
		runes = append(runes, r)
		i++
	}
	return string(runes), nil
}

func encodeUTF8(name, text string, bom []byte) ([]byte, error) {
	if !utf8.ValidString(text) {
		return nil, invalidText(name)
	}
	return append(slices.Clone(bom), text...), nil
}

// encodeUTF16 writes the byte-order mark, when bom is set, and then the code units
// of text.
func encodeUTF16(name, text string, order byteOrder, bom bool) ([]byte, error) {
	if !utf8.ValidString(text) {
		return nil, invalidText(name)
	}
	units := utf16.Encode([]rune(text))
	var out []byte
	if bom {
		out = order.AppendUint16(out, 0xfeff)
	}
	for _, u := range units {
		out = order.AppendUint16(out, u)
	}
	if out == nil {
		out = []byte{}
	}
	return out, nil
}

func unknownName(name string) error {
	return fmt.Errorf("encoding: unknown encoding %q", name)
}

func invalidBytes(name string) error {
	return fmt.Errorf("encoding: invalid %s byte sequence", name)
}

func invalidText(name string) error {
	return fmt.Errorf("encoding: text is not valid for %s", name)
}
