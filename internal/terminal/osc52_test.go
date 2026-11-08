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
		p.scan([]byte(c), emit, nil)
	}
	return got
}

// collectOSC52Queries returns the read requests the parser sees, as selections.
func collectOSC52Queries(t *testing.T, chunks ...string) []string {
	t.Helper()
	var got []string
	p := osc52Parser{}
	emit := func(selection, text string) { got = append(got, "WRITE:"+selection+"="+text) }
	query := func(selection string) { got = append(got, "QUERY:"+selection) }
	for _, c := range chunks {
		p.scan([]byte(c), emit, query)
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

func TestOSC52QueryParser(t *testing.T) {
	t.Parallel()
	esc := "\x1b"
	tests := []struct {
		name   string
		chunks []string
		want   []string
	}{
		{"clipboard query", []string{esc + "]52;c;?\x07"}, []string{"QUERY:c"}},
		{"empty selector query", []string{esc + "]52;;?\x07"}, []string{"QUERY:"}},
		{"split query", []string{esc + "]52;c", ";?\x07"}, []string{"QUERY:c"}},
		{"ST query", []string{esc + "]52;c;?" + esc + "\\"}, []string{"QUERY:c"}},
		{"primary query ignored", []string{esc + "]52;p;?\x07"}, nil},
		{"query then write", []string{esc + "]52;c;?\x07" + esc + "]52;c;aGVsbG8=\x07"}, []string{"QUERY:c", "WRITE:c=hello"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectOSC52Queries(t, tt.chunks...)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOSC52ResponseFormat(t *testing.T) {
	t.Parallel()
	resp := osc52Response("c", "hello")
	if want := "\x1b]52;c;aGVsbG8=\x07"; string(resp) != want {
		t.Fatalf("response = %q, want %q", resp, want)
	}
	// A bounded response stays below the parser's sequence bound.
	big := make([]byte, maxOSC52ResponseText)
	for i := range big {
		big[i] = 'a'
	}
	if got := len(osc52Response("", string(big))); got >= maxOSC52Sequence {
		t.Fatalf("response %d bytes >= sequence bound %d", got, maxOSC52Sequence)
	}
}
