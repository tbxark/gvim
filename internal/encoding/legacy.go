package encoding

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// sampleSize is how much of a file the detectors look at, as Fresh does.
const sampleSize = 8 * 1024

// commonHanzi and commonHangul are very frequent characters of Chinese and Korean
// text. Chinese and Korean legacy encodings use overlapping byte ranges, so a
// sample that decodes in both is told apart by which decoding reads like the
// language. Fresh leans on chardetng's frequency tables for this.
const (
	commonHanzi  = "的一是不了人我在有他这中大来上国个到说们为子和你地出道也时年得就那要下以生会自着去之过家学对可她里后小么心多天而能好都然没日于起还发成事只作当想看文无开手十用主行方又如前所本见经头面公同三已老从动两长知民样现分等间因点实全表世界试测"
	commonKanji  = "日本人大学生会社国中年月時間者語文字京都東"
	commonHangul = "이다는의에은가를을한하고서로지도리어으시스기자나대인아정사수제안녕세요계습니있고와그것해여오"
)

// hasBOM reports whether data starts with a byte-order mark.
func hasBOM(data []byte) bool {
	return slices.ContainsFunc([][]byte{utf8BOM, utf16LEBOM, utf16BEBOM}, func(bom []byte) bool {
		return len(data) >= len(bom) && string(data[:len(bom)]) == string(bom)
	})
}

// isBinaryControl reports whether b is a control byte that text does not use:
// the C0 controls other than tab, line feed, vertical tab, form feed, carriage
// return and escape, and DEL.
func isBinaryControl(b byte) bool {
	switch {
	case b < 0x20:
		return !slices.Contains([]byte{'\t', '\n', 0x0b, 0x0c, '\r', 0x1b}, b)
	default:
		return b == 0x7f
	}
}

// IsBinary reports whether data is binary and not text in any of the encodings.
// It follows Fresh's detect_encoding_or_binary: only the first 8 KiB is looked at,
// a byte-order mark or text that looks like UTF-16 settles it as text, and
// otherwise a NUL or an unprintable control byte makes the data binary.
func IsBinary(data []byte) bool {
	sample := data[:min(len(data), sampleSize)]
	if hasBOM(sample) {
		return false
	}
	if validUTF8Sample(sample, len(sample) < len(data)) {
		return slices.ContainsFunc(sample, isBinaryControl)
	}
	if utf16WithoutBOM(sample) != "" {
		return false
	}
	return slices.ContainsFunc(sample, isBinaryControl)
}

// validUTF8Sample reports whether sample is valid UTF-8. A sample cut out of a
// longer file may end inside a character, so up to 3 trailing bytes are ignored
// then.
func validUTF8Sample(sample []byte, truncated bool) bool {
	if utf8.Valid(sample) {
		return true
	}
	if !truncated {
		return false
	}
	for cut := 1; cut <= 3 && cut < len(sample); cut++ {
		if utf8.Valid(sample[:len(sample)-cut]) {
			return true
		}
	}
	return false
}

// utf16WithoutBOM returns "utf-16le" or "utf-16be" when over half of the byte
// pairs of sample are a printable byte next to a NUL, and "" otherwise.
func utf16WithoutBOM(sample []byte) string {
	if len(sample) < 4 {
		return ""
	}
	printable := func(b byte) bool { return b >= 0x20 && b <= 0x7e || b >= 0x80 }
	var le, be int
	pairs := len(sample) / 2
	for i := range pairs {
		a, b := sample[2*i], sample[2*i+1]
		if b == 0 && printable(a) {
			le++
		}
		if a == 0 && printable(b) {
			be++
		}
	}
	switch {
	case le > pairs/2:
		return "utf-16le"
	case be > pairs/2:
		return "utf-16be"
	}
	return ""
}

// detectLegacy names the encoding of data that is not valid UTF-8 and has no
// byte-order mark. It is Fresh's detect_legacy_encoding without the statistics of
// chardetng: BOM-less UTF-16, then Shift-JIS, EUC-KR and GB18030 by how the text
// reads, then the Windows code pages by their byte patterns, and Windows-1252
// otherwise.
func detectLegacy(data []byte) string {
	sample := data[:min(len(data), sampleSize)]
	if enc := utf16WithoutBOM(sample); enc != "" {
		return enc
	}
	if enc := detectCJK(sample, len(sample) < len(data)); enc != "" {
		return enc
	}
	switch {
	case hasWindows1250Pattern(sample):
		return "windows-1250"
	case hasWindows1251Pattern(sample):
		return "windows-1251"
	}
	return "windows-1252"
}

// detectCJK returns "shift_jis", "euc-kr" or "gb18030" when sample is text in
// one of them, and "" otherwise. Latin-1 patterns weigh against it, as in Fresh,
// but a space before a high byte is also how Korean words are spaced, so text
// that reads clearly as Korean or Chinese still passes.
func detectCJK(sample []byte, truncated bool) string {
	cjkOnly := slices.ContainsFunc(sample, func(b byte) bool { return b >= 0x81 && b < 0xa0 })
	latin1 := !cjkOnly && hasLatin1Pattern(sample)
	decode := func(name string) string {
		// A sample cut out of a longer file may end inside a character.
		for cut := 0; cut <= 3 && cut < len(sample); cut++ {
			if text, err := Decode(name, sample[:len(sample)-cut]); err == nil {
				return text
			}
			if !truncated {
				break
			}
		}
		return ""
	}
	if text := decode("shift_jis"); !latin1 && sjisScore(text) {
		return "shift_jis"
	}
	var korean, chinese float64
	if text := decode("euc-kr"); text != "" {
		korean = commonShare(text, commonHangul, isHangul)
	}
	if text := decode("gb18030"); text != "" {
		chinese = commonShare(text, commonHanzi, isHanzi)
	}
	need := 0.2
	if latin1 {
		need = 0.5
	}
	switch {
	case korean > chinese && korean >= need:
		return "euc-kr"
	case chinese >= need:
		return "gb18030"
	}
	return ""
}

// sjisScore reports whether text, decoded as Shift-JIS, reads as Japanese: nearly
// all of its non-ASCII characters are kana, kanji or punctuation of CJK text, and
// it has kana or mostly common kanji. Half-width katakana is not counted, because
// single bytes of Latin-1 text decode to it.
func sjisScore(text string) bool {
	var kana, other, total int
	for _, r := range text {
		if r < utf8.RuneSelf {
			continue
		}
		total++
		switch {
		case r >= 0x3040 && r <= 0x30ff:
			kana++
		case isHanzi(r) || r >= 0x3000 && r <= 0x303f || r >= 0xff01 && r <= 0xff5e:
			other++
		}
	}
	if (kana+other)*10 < total*9 {
		return false
	}
	return kana > 0 || commonShare(text, commonKanji, isHanzi) >= 0.5
}

// commonShare is the share of the characters of text in script, as judged by
// inScript, that are in common. It is 0 when text has none.
func commonShare(text, common string, inScript func(rune) bool) float64 {
	var hit, total int
	for _, r := range text {
		if !inScript(r) {
			continue
		}
		total++
		if strings.ContainsRune(common, r) {
			hit++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(hit) / float64(total)
}

func isHangul(r rune) bool { return r >= 0xac00 && r <= 0xd7af }
func isHanzi(r rune) bool  { return r >= 0x4e00 && r <= 0x9fff }

// hasLatin1Pattern reports whether sample has high bytes that GBK cannot hold but
// Latin-1 text has: a high byte before a space or newline, or a high byte after a
// space. It is Fresh's has_latin1_high_byte_pattern.
func hasLatin1Pattern(sample []byte) bool {
	for i := 0; i < len(sample); i++ {
		b := sample[i]
		if b < 0x80 {
			if b == 0x20 && i+1 < len(sample) && sample[i+1] >= 0xa0 {
				return true
			}
			continue
		}
		if i+1 >= len(sample) {
			continue
		}
		next := sample[i+1]
		if b >= 0x81 && b <= 0xfe && (next >= 0x40 && next <= 0x7e || next >= 0x80 && next <= 0xfe) {
			i++
			continue
		}
		if b >= 0xa0 && next < 0x40 {
			return true
		}
	}
	return false
}

// hasWindows1250Pattern reports whether sample has bytes of Central European
// text: one that Windows-1252 leaves undefined (0x8d, 0x8f, 0x9d), or two of the
// Polish letters at 0x8c, 0x9c and 0x9f.
func hasWindows1250Pattern(sample []byte) bool {
	strong := 0
	for _, b := range sample {
		switch b {
		case 0x8d, 0x8f, 0x9d:
			return true
		case 0x8c, 0x9c, 0x9f:
			strong++
		}
	}
	return strong >= 2
}

// minCyrillicRun is how many Cyrillic letters in a row a sample needs to count as
// Windows-1251. Russian words are long, but Latin text keeps its accented letters
// apart, between ASCII letters, as in "Éric" and "allé".
const minCyrillicRun = 3

// hasWindows1251Pattern reports whether sample has Cyrillic upper and lower case
// letters, or Yo with a lower case letter, at the byte values of Windows-1251, and a
// word-shaped run of them. Fresh stops at the case mix, but it has chardetng to
// overrule it, and Latin text with one capital and one lower case accented letter
// has the case mix.
func hasWindows1251Pattern(sample []byte) bool {
	var upper, lower, yo bool
	var run, longest int
	for _, b := range sample {
		if b >= 0xc0 || b == 0xa8 || b == 0xb8 {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
		switch {
		case b >= 0xc0 && b <= 0xde:
			upper = true
		case b >= 0xe0:
			lower = true
		case b == 0xa8 || b == 0xb8:
			yo = true
		}
	}
	return (upper && lower || yo && lower) && longest >= minCyrillicRun
}
