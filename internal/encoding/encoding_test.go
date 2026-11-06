package encoding

import (
	"bytes"
	"slices"
	"testing"
)

func TestNames(t *testing.T) {
	t.Parallel()
	want := []string{"utf-8", "utf-8-bom", "utf-16le", "utf-16be", "windows-1252", "iso-8859-1", "shift_jis"}
	if got := Names(); !slices.Equal(got, want) {
		t.Errorf("Names() = %q, want %q", got, want)
	}
}

func TestDetect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"empty input is utf-8", nil, "utf-8"},
		{"ascii is utf-8", []byte("hello\n"), "utf-8"},
		{"multi-byte utf-8", []byte("日本語 😀"), "utf-8"},
		{"utf-8 bom", []byte("\xef\xbb\xbfa"), "utf-8-bom"},
		{"utf-8 bom alone", []byte{0xef, 0xbb, 0xbf}, "utf-8-bom"},
		{"utf-16le bom", []byte{0xff, 0xfe, 'a', 0}, "utf-16le"},
		{"utf-16le bom alone", []byte{0xff, 0xfe}, "utf-16le"},
		{"utf-16be bom", []byte{0xfe, 0xff, 0, 'a'}, "utf-16be"},
		{"invalid utf-8 is windows-1252", []byte("caf\xe9"), "windows-1252"},
		{"utf-16 without bom and non-ascii is windows-1252", []byte{0xe9, 0}, "windows-1252"},
		{"utf-16 ascii without bom is utf-8", []byte{'a', 0}, "utf-8"},
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

func TestDecode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		enc  string
		data []byte
		want string
	}{
		{"empty utf-16le", "utf-16le", nil, ""},
		{"utf-8 text", "utf-8", []byte("héllo 日本 😀"), "héllo 日本 😀"},
		{"utf-8 keeps a bom as text", "utf-8", []byte("\xef\xbb\xbfa"), "\ufeffa"},
		{"utf-8-bom strips bom", "utf-8-bom", []byte("\xef\xbb\xbfa"), "a"},
		{"utf-8-bom without bom", "utf-8-bom", []byte("a"), "a"},
		{"utf-16le strips bom", "utf-16le", []byte{0xff, 0xfe, 'A', 0}, "A"},
		{"utf-16le without bom", "utf-16le", []byte{'A', 0}, "A"},
		{"utf-16le surrogate pair", "utf-16le", []byte{0xff, 0xfe, 0x3d, 0xd8, 0x00, 0xde}, "😀"},
		{"utf-16be strips bom", "utf-16be", []byte{0xfe, 0xff, 0, 'A'}, "A"},
		{"utf-16be surrogate pair", "utf-16be", []byte{0xfe, 0xff, 0xd8, 0x3d, 0xde, 0x00}, "😀"},
		{"windows-1252 euro sign", "windows-1252", []byte{'c', 'a', 'f', 0xe9, ' ', 0x80}, "café €"},
		{"iso-8859-1 accepts every byte", "iso-8859-1", []byte{0x80, 0xff}, "\u0080ÿ"},
		{"shift_jis kanji", "shift_jis", []byte{0x93, 0xfa, 0x96, 0x7b}, "日本"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Decode(tc.enc, tc.data)
			if err != nil {
				t.Fatalf("Decode(%q, % x) error: %v", tc.enc, tc.data, err)
			}
			if got != tc.want {
				t.Errorf("Decode(%q, % x) = %q, want %q", tc.enc, tc.data, got, tc.want)
			}
		})
	}
}

func TestDecodeRejectsInvalidBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		enc  string
		data []byte
	}{
		{"utf-8 invalid byte", "utf-8", []byte("a\xff")},
		{"utf-8-bom invalid byte after bom", "utf-8-bom", []byte("\xef\xbb\xbf\xff")},
		{"utf-16le odd length", "utf-16le", []byte{'A', 0, 'B'}},
		{"utf-16le lone high surrogate", "utf-16le", []byte{0x00, 0xd8, 'A', 0}},
		{"utf-16le lone low surrogate", "utf-16le", []byte{0x00, 0xdc}},
		{"utf-16be odd length", "utf-16be", []byte{0, 'A', 0}},
		{"utf-16be high surrogate at end", "utf-16be", []byte{0xd8, 0x00}},
		{"windows-1252 undefined byte 0x81", "windows-1252", []byte{0x81}},
		{"windows-1252 undefined byte 0x9d", "windows-1252", []byte{'a', 0x9d}},
		{"shift_jis truncated lead byte", "shift_jis", []byte{0x81}},
		{"shift_jis invalid trail byte", "shift_jis", []byte{0x82, 0x20}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, err := Decode(tc.enc, tc.data); err == nil {
				t.Errorf("Decode(%q, % x) = %q, want error", tc.enc, tc.data, got)
			}
		})
	}
}

func TestEncode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		enc  string
		text string
		want []byte
	}{
		{"empty utf-8", "utf-8", "", []byte{}},
		{"utf-8 text", "utf-8", "a日", []byte("a日")},
		{"utf-8-bom adds bom", "utf-8-bom", "a", []byte{0xef, 0xbb, 0xbf, 'a'}},
		{"utf-8-bom empty text is bom only", "utf-8-bom", "", []byte{0xef, 0xbb, 0xbf}},
		{"utf-16le adds bom", "utf-16le", "A", []byte{0xff, 0xfe, 'A', 0}},
		{"utf-16le surrogate pair", "utf-16le", "😀", []byte{0xff, 0xfe, 0x3d, 0xd8, 0x00, 0xde}},
		{"utf-16be adds bom", "utf-16be", "A", []byte{0xfe, 0xff, 0, 'A'}},
		{"windows-1252 euro sign", "windows-1252", "café €", []byte{'c', 'a', 'f', 0xe9, ' ', 0x80}},
		{"iso-8859-1 latin-1 letter", "iso-8859-1", "ÿ", []byte{0xff}},
		{"shift_jis kanji", "shift_jis", "日本", []byte{0x93, 0xfa, 0x96, 0x7b}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Encode(tc.enc, tc.text)
			if err != nil {
				t.Fatalf("Encode(%q, %q) error: %v", tc.enc, tc.text, err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("Encode(%q, %q) = % x, want % x", tc.enc, tc.text, got, tc.want)
			}
		})
	}
}

func TestEncodeRejectsUnrepresentableText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		enc  string
		text string
	}{
		{"utf-8 invalid utf-8 text", "utf-8", "a\xff"},
		{"utf-8-bom invalid utf-8 text", "utf-8-bom", "\xff"},
		{"utf-16le invalid utf-8 text", "utf-16le", "\xff"},
		{"utf-16be invalid utf-8 text", "utf-16be", "\xff"},
		{"windows-1252 cannot represent kanji", "windows-1252", "日"},
		{"iso-8859-1 cannot represent euro sign", "iso-8859-1", "€"},
		{"shift_jis cannot represent emoji", "shift_jis", "😀"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, err := Encode(tc.enc, tc.text); err == nil {
				t.Errorf("Encode(%q, %q) = % x, want error", tc.enc, tc.text, got)
			}
		})
	}
}

func TestUnknownName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"latin-9", ""} {
		if _, err := Decode(name, []byte("a")); err == nil {
			t.Errorf("Decode(%q) error = nil, want error", name)
		}
		if _, err := Encode(name, "a"); err == nil {
			t.Errorf("Encode(%q) error = nil, want error", name)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		enc  string
		text string
	}{
		{"utf-8", "héllo 日本 😀"},
		{"utf-8-bom", "héllo 日本 😀"},
		{"utf-8-bom", "\ufeffa"},
		{"utf-16le", "héllo 日本 😀"},
		{"utf-16le", ""},
		{"utf-16be", "héllo 日本 😀"},
		{"windows-1252", "café €"},
		{"iso-8859-1", "café ÿ"},
		{"shift_jis", "日本語 カタカナ abc"},
	}
	for _, tc := range tests {
		t.Run(tc.enc+"/"+tc.text, func(t *testing.T) {
			t.Parallel()
			data, err := Encode(tc.enc, tc.text)
			if err != nil {
				t.Fatalf("Encode(%q, %q) error: %v", tc.enc, tc.text, err)
			}
			got, err := Decode(tc.enc, data)
			if err != nil {
				t.Fatalf("Decode(%q, % x) error: %v", tc.enc, data, err)
			}
			if got != tc.text {
				t.Errorf("round trip through %q = %q, want %q", tc.enc, got, tc.text)
			}
		})
	}
}
