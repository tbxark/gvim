package terminal

import (
	"strings"
	"testing"
)

// collectOSC52 feeds the chunks as child output and returns the clipboard
// writes the session's hook sees, as "selection=text".
func collectOSC52(t *testing.T, s *Session, chunks ...string) []string {
	t.Helper()
	var got []string
	s.SetOSC52Handler(func(selection, text string) bool {
		got = append(got, selection+"="+text)
		return true
	})
	for _, c := range chunks {
		s.feed([]byte(c))
	}
	return got
}

// collectOSC52Queries feeds the chunks and returns the clipboard writes and
// read requests the session sees, as "WRITE:selection=text" / "QUERY:selection".
func collectOSC52Queries(t *testing.T, s *Session, chunks ...string) []string {
	t.Helper()
	var got []string
	s.SetOSC52Handler(func(selection, text string) bool {
		got = append(got, "WRITE:"+selection+"="+text)
		return true
	})
	s.osc52.setQuery(func(selection string) {
		got = append(got, "QUERY:"+selection)
	})
	for _, c := range chunks {
		s.feed([]byte(c))
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
		{"combined selection pc", []string{esc + "]52;pc;aGVsbG8=\x07"}, []string{"pc=hello"}},
		{"combined selection cp", []string{esc + "]52;cp;aGVsbG8=\x07"}, []string{"cp=hello"}},
		{"split ST across reads", []string{esc + "]52;c;aGVsbG8=" + esc, "\\"}, []string{"c=hello"}},
		// x/vt's parser delivers the partial body of an OSC aborted by another
		// escape; the old scanner dropped it. The aborted write still fires.
		{"aborted body still delivered", []string{esc + "]52;c;aGVsbG8=" + esc + "[31m" + esc + "]52;c;d29ybGQ=\x07"}, []string{"c=hello", "c=world"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := startFeedSession(t)
			got := collectOSC52(t, s, tt.chunks...)
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
	s, _ := startFeedSession(t)
	if got := collectOSC52(t, s, b.String()); len(got) != 0 {
		t.Fatalf("an oversize sequence was emitted: %q", got)
	}
	// The parser recovers and a later sequence still works.
	s2, _ := startFeedSession(t)
	got := collectOSC52(t, s2, b.String()+"\x1b]52;c;aGVsbG8=\x07")
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
		{"combined selection query", []string{esc + "]52;pc;?\x07"}, []string{"QUERY:pc"}},
		{"query then write", []string{esc + "]52;c;?\x07" + esc + "]52;c;aGVsbG8=\x07"}, []string{"QUERY:c", "WRITE:c=hello"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := startFeedSession(t)
			got := collectOSC52Queries(t, s, tt.chunks...)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
	t.Run("query works when emit is nil", func(t *testing.T) {
		s, _ := startFeedSession(t)
		var queries []string
		s.osc52.setQuery(func(sel string) {
			queries = append(queries, sel)
		})
		s.feed([]byte(esc + "]52;c;?\x07"))
		if len(queries) != 1 || queries[0] != "c" {
			t.Fatalf("queries = %q, want [c]", queries)
		}
	})
}

// TestOSC52QueryResponseIsQueued checks that a query handler's text is sent
// back to the child as a base64 OSC 52 response on its input.
func TestOSC52QueryResponseIsQueued(t *testing.T) {
	t.Parallel()
	s, p := startFeedSession(t)
	s.SetOSC52QueryHandler(func(selection string) (string, bool) {
		return "hello", true
	})
	s.feed([]byte("\x1b]52;c;?\x07"))
	waitWritten(t, p, "\x1b]52;c;aGVsbG8=\x07")
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
