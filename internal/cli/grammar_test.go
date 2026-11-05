package cli

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
)

func TestGrammarListWithFakeNames(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		want  string
	}{
		{
			name:  "sorted and without repeats",
			names: []string{"go", "Go", "C", "Bash", "Go", "bash"},
			want:  "Bash\nC\nGo\nbash\ngo\n",
		},
		{
			name:  "already sorted",
			names: []string{"a", "b"},
			want:  "a\nb\n",
		},
		{
			name:  "no grammars prints nothing",
			names: nil,
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := slices.Clone(tt.names)
			d := grammarDeps{names: func() []string { return tt.names }}
			stdout, stderr, err := runTree(t, newGrammarCommandWith(d), "list")
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if stdout != tt.want {
				t.Fatalf("stdout = %q, want %q", stdout, tt.want)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if !slices.Equal(tt.names, before) {
				t.Fatalf("the injected list was changed: %q, want %q", tt.names, before)
			}
		})
	}
}

// TestGrammarListReal runs the real command against the chroma registry. The
// output must be sorted, have no repeats, and include the Go lexer.
func TestGrammarListReal(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"grammar", "list"}, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	text := strings.TrimSuffix(stdout.String(), "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 10 {
		t.Fatalf("got %d grammars, want many more", len(lines))
	}
	if !slices.IsSorted(lines) {
		t.Fatal("grammar names are not sorted")
	}
	if len(slices.Compact(slices.Clone(lines))) != len(lines) {
		t.Fatal("grammar names repeat")
	}
	if !slices.Contains(lines, "go") {
		t.Fatal("the go alias is missing")
	}
}
