package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/app"
)

// The stdin flags: --stdin and the - positional mark the same input; --stdin-name names the
// buffer; the app.Options validation checks the combinations.

func TestStdinFlagsReachOptions(t *testing.T) {
	isolateHome(t)
	tests := []struct {
		name string
		args []string
		want app.Options
	}{
		{"--stdin", []string{"--stdin"}, app.Options{ReadStdin: true}},
		{"dash positional", []string{"-"}, app.Options{ReadStdin: true}},
		{"dash among files", []string{"-", "a.txt"}, app.Options{ReadStdin: true, Files: []app.FileTarget{{Path: "a.txt"}}}},
		{"files before the dash", []string{"a.txt", "-"}, app.Options{ReadStdin: true, Files: []app.FileTarget{{Path: "a.txt"}}}},
		{"--stdin-name", []string{"--stdin", "--stdin-name", "notes.md"}, app.Options{ReadStdin: true, StdinName: "notes.md"}},
		{"--stdin-name with a dash", []string{"--stdin-name", "a.go", "-"}, app.Options{ReadStdin: true, StdinName: "a.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			code, _, stderr := execute(t, tt.args, rec)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if rec.calls != 1 {
				t.Fatalf("runner calls = %d", rec.calls)
			}
			want := tt.want
			got := rec.got
			if got.ReadStdin != want.ReadStdin || got.StdinName != want.StdinName || len(got.Stdin) != 0 {
				t.Errorf("options = %+v, want ReadStdin %v StdinName %q", got, want.ReadStdin, want.StdinName)
			}
			var gotFiles, wantFiles []string
			for _, f := range got.Files {
				gotFiles = append(gotFiles, f.Path)
			}
			for _, f := range want.Files {
				wantFiles = append(wantFiles, f.Path)
			}
			if !slices.Equal(gotFiles, wantFiles) {
				t.Errorf("files = %v, want %v", gotFiles, wantFiles)
			}
		})
	}
}

func TestStdinFlagConflicts(t *testing.T) {
	isolateHome(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"two dashes", []string{"-", "-"}, "only one - (standard input) may be given"},
		{"--stdin and a dash", []string{"--stdin", "-"}, "--stdin and - name the same standard input"},
		{"--stdin-name without stdin", []string{"--stdin-name", "notes.md"}, "--stdin-name needs --stdin or a - argument"},
		{"--diff with stdin", []string{"--diff", "-", "a.txt", "b.txt"}, "--stdin cannot be combined with --diff"},
		{"--restore with stdin", []string{"--restore", "-"}, "--restore cannot be combined with --stdin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			code, _, stderr := execute(t, tt.args, rec)
			if code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want %q", stderr, tt.want)
			}
			if rec.calls != 0 {
				t.Errorf("the runner ran despite the validation error")
			}
		})
	}
}

func TestHelpMentionsStdin(t *testing.T) {
	isolateHome(t)
	rec := &recorder{}
	code, stdout, _ := execute(t, []string{"--help"}, rec)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"--stdin", "--stdin-name", "Standard input becomes a scratch [stdin] buffer"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q:\n%s", want, stdout)
		}
	}
}
