package cli

import "testing"

// TestDiffFlagPassesTwoFilesToTheRunner checks the --diff flag: two file arguments
// reach the runner with Diff set. Any other count of files, or a directory, is a
// usage error, and the runner is not called.
func TestDiffFlagPassesTwoFilesToTheRunner(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantRun  bool
		wantDiff bool
	}{
		{name: "two files", args: []string{"--diff", "a.txt", "b.txt"}, wantCode: 0, wantRun: true, wantDiff: true},
		{name: "one file", args: []string{"--diff", "a.txt"}, wantCode: 1, wantRun: false},
		{name: "three files", args: []string{"--diff", "a.txt", "b.txt", "c.txt"}, wantCode: 1, wantRun: false},
		{name: "a directory and a file", args: []string{"--diff", dir, "a.txt"}, wantCode: 1, wantRun: false},
		{name: "two files without the flag", args: []string{"a.txt", "b.txt"}, wantCode: 0, wantRun: true, wantDiff: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			code, _, _ := execute(t, tt.args, rec)
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d", code, tt.wantCode)
			}
			if got := rec.calls == 1; got != tt.wantRun {
				t.Fatalf("runner called = %v, want %v", got, tt.wantRun)
			}
			if tt.wantRun && rec.got.Diff != tt.wantDiff {
				t.Fatalf("Options.Diff = %v, want %v", rec.got.Diff, tt.wantDiff)
			}
		})
	}
}
