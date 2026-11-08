package terminal

import (
	"strings"
	"testing"
)

func collectOSC52(t *testing.T, chunks ...string) []string {
	t.Helper()
	var got []string
	p := osc52Parser{}
	emit := func(selection, text string) { got = append(got, selection+"="+text) }
	for _, c := range chunks {
		p.scan([]byte(c), emit)
	}
	return got
}

func TestOSC52Parser(t *testing.T) {
	t.Parallel()
	esc := "\x1b"
	tests := []struct {
		name   string
		chunks []string
		want   []string
	}{
		{"clipboard with BEL", []string{esc + "]52;c;aGVsbG8=\x07"}, []string{"c=hello"}},
		{"empty selector", []string{esc + "]52;;aGVsbG8=\x07"}, []string{"=hello"}},
		{"ST terminator", []string{esc + "]52;c;aGVsbG8=" + esc + "\\"}, []string{"c=hello"}},
		{"split across reads", []string{esc + "]52;c;aGVs", "bG8=\x07"}, []string{"c=hello"}},
		{"primary ignored", []string{esc + "]52;p;aGVsbG8=\x07"}, nil},
		{"cut buffer ignored", []string{esc + "]52;0;aGVsbG8=\x07"}, nil},
		{"invalid base64", []string{esc + "]52;c;not-base64!!\x07"}, nil},
		{"other OSC ignored", []string{esc + "]0;title\x07" + esc + "]52;c;aGVsbG8=\x07"}, []string{"c=hello"}},
		{"empty payload", []string{esc + "]52;c;\x07"}, []string{"c="}},
		{"bare ESC then text", []string{esc + "[31m" + esc + "]52;c;aGVsbG8=\x07"}, []string{"c=hello"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectOSC52(t, tt.chunks...)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOSC52ParserIsBounded(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("\x1b]52;c;")
	for b.Len() < maxOSC52Sequence+1024 {
		b.WriteByte('A')
	}
	b.WriteString("\x07")
	if got := collectOSC52(t, b.String()); len(got) != 0 {
		t.Fatalf("an oversize sequence was emitted: %q", got)
	}
	// The parser recovers and a later sequence still works.
	got := collectOSC52(t, b.String()+"\x1b]52;c;aGVsbG8=\x07")
	if len(got) != 1 {
		t.Fatalf("recovery got %q", got)
	}
}
