package encoding

import (
	"bytes"
	"testing"
)

func TestLegacyEncodingsRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		enc  string
		text string
		want []byte
	}{
		{"windows-1250", "Zażółć", []byte{'Z', 'a', 0xbf, 0xf3, 0xb3, 0xe6}},
		{"windows-1251", "Привет", []byte{0xcf, 0xf0, 0xe8, 0xe2, 0xe5, 0xf2}},
		{"gbk", "你好", []byte{0xc4, 0xe3, 0xba, 0xc3}},
		{"gb18030", "你好😀", []byte{0xc4, 0xe3, 0xba, 0xc3, 0x94, 0x39, 0xfc, 0x36}},
		{"euc-kr", "안녕", []byte{0xbe, 0xc8, 0xb3, 0xe7}},
	}
	for _, tc := range tests {
		t.Run(tc.enc, func(t *testing.T) {
			t.Parallel()
			data, err := Encode(tc.enc, tc.text)
			if err != nil {
				t.Fatalf("Encode(%q, %q) error: %v", tc.enc, tc.text, err)
			}
			if !bytes.Equal(data, tc.want) {
				t.Errorf("Encode(%q, %q) = % x, want % x", tc.enc, tc.text, data, tc.want)
			}
			got, err := Decode(tc.enc, data)
			if err != nil || got != tc.text {
				t.Errorf("Decode(%q, % x) = %q, %v, want %q", tc.enc, data, got, err, tc.text)
			}
		})
	}
}

func TestDecodeRejectsInvalidLegacyBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		enc  string
		data []byte
	}{
		{"windows-1250", []byte{'a', 0x81}},
		{"windows-1251", []byte{'a', 0x98}},
		{"gbk", []byte{0x81}},
		{"gb18030", []byte{0xc4, 0x20}},
		{"euc-kr", []byte{0xbe, 0x20}},
	}
	for _, tc := range tests {
		if got, err := Decode(tc.enc, tc.data); err == nil {
			t.Errorf("Decode(%q, % x) = %q, want error", tc.enc, tc.data, got)
		}
	}
}

// legacy encodes text in enc, failing the test when it cannot.
func legacy(t *testing.T, enc, text string) []byte {
	t.Helper()
	data, err := Encode(enc, text)
	if err != nil {
		t.Fatalf("Encode(%q, %q): %v", enc, text, err)
	}
	return data
}

func TestDetectLegacy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"latin text stays windows-1252", legacy(t, "windows-1252", "Café résumé, naïve façade\n"), "windows-1252"},
		{"czech t caron is windows-1250", []byte("m\x9dsto\n"), "windows-1250"},
		{"two polish letters are windows-1250", []byte("\x9cwiat \x8cwiat\n"), "windows-1250"},
		{"mixed case cyrillic is windows-1251", legacy(t, "windows-1251", "Привет, Мир!\n"), "windows-1251"},
		{"yo with lowercase is windows-1251", legacy(t, "windows-1251", "ёжик\n"), "windows-1251"},
		{"french capital and lowercase accents", legacy(t, "windows-1252", "Éric est allé à la mer.\n"), "windows-1252"},
		{"spanish capital and lowercase accents", legacy(t, "windows-1252", "Año nuevo en Ávila, señor.\n"), "windows-1252"},
		{"german capital and lowercase umlauts", legacy(t, "windows-1252", "Über die Brücke, Öl für Ärzte.\n"), "windows-1252"},
		{"shift_jis with kana", legacy(t, "shift_jis", "こんにちは世界\n"), "shift_jis"},
		{"chinese text", legacy(t, "gb18030", "你好，世界。这是一个测试。\n"), "gb18030"},
		{"korean text", legacy(t, "euc-kr", "안녕하세요 세계\n"), "euc-kr"},
		{"utf-8 wins over legacy", []byte("héllo\n"), "utf-8"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Detect(tc.data); got != tc.want {
				t.Errorf("Detect(% x) = %q, want %q", tc.data, got, tc.want)
			}
		})
	}
}

// TestDetectShortCJKSamples checks that one-line CJK samples are told apart from Latin text.
// Without chardetng's frequency tables, rarer words of a script can still be missed; the
// user then picks the encoding with reload_with_encoding.
func TestDetectShortCJKSamples(t *testing.T) {
	t.Parallel()
	tests := []struct{ enc, text string }{
		{"gb18030", "你好\n"},
		{"gb18030", "中文\n"},
		{"gb18030", "// 这是一个测试\n"},
		{"euc-kr", "안녕\n"},
		{"euc-kr", "// 한국어\n"},
		{"euc-kr", "설정\n"},
		{"shift_jis", "日本語\n"},
		{"shift_jis", "ひらがな\n"},
		{"shift_jis", "データ\n"},
		{"shift_jis", "// これは日本語です\n"},
	}
	for _, tc := range tests {
		t.Run(tc.enc+" "+tc.text, func(t *testing.T) {
			t.Parallel()
			if got := Detect(legacy(t, tc.enc, tc.text)); got != tc.enc {
				t.Errorf("Detect(%q in %s) = %q", tc.text, tc.enc, got)
			}
		})
	}
}

func TestIsBinary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"empty", nil, false},
		{"plain text", []byte("hello\tworld\r\n\f\x1b[0m"), false},
		{"nul in valid utf-8", []byte("ab\x00cd"), true},
		{"control byte", []byte("ab\x01cd"), true},
		{"del byte", []byte("ab\x7fcd"), true},
		{"legacy text with nul", []byte("caf\xe9\x00"), true},
		{"utf-16le bom", []byte{0xff, 0xfe, 'a', 0, 'b', 0}, false},
		{"utf-16be bom", []byte{0xfe, 0xff, 0, 'a'}, false},
		{"utf-8 bom text", []byte("\xef\xbb\xbfabc"), false},
		{"utf-16le without bom", []byte{'a', 0, 'b', 0, 0xe9, 0, 'c', 0}, false},
		{"control byte past the sample", append(bytes.Repeat([]byte("a"), sampleSize), 0), false},
		{"png header", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsBinary(tc.data); got != tc.want {
				t.Errorf("IsBinary(% x) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}
