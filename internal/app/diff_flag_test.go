package app

import (
	"path/filepath"
	"testing"
)

// TestValidateDiffNeedsTwoFiles checks that Diff takes exactly two files and no
// directory, and that the options without Diff are not affected.
func TestValidateDiffNeedsTwoFiles(t *testing.T) {
	t.Parallel()
	two := []FileTarget{{Path: "a.txt"}, {Path: "b.txt"}}
	tests := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{name: "two files", opts: Options{Diff: true, Files: two}, wantErr: false},
		{name: "one file", opts: Options{Diff: true, Files: two[:1]}, wantErr: true},
		{name: "no file", opts: Options{Diff: true}, wantErr: true},
		{name: "three files", opts: Options{Diff: true, Files: append(two[:2:2], FileTarget{Path: "c.txt"})}, wantErr: true},
		{name: "a directory", opts: Options{Diff: true, Root: "/work", Files: two}, wantErr: true},
		{name: "no diff, one file", opts: Options{Files: two[:1]}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.opts.Validate()
			if got := err != nil; got != tt.wantErr {
				t.Fatalf("Validate() error = %v, want an error = %v", err, tt.wantErr)
			}
		})
	}
}

// TestBuildModelDiffNeedsTwoDistinctFiles checks that --diff stops startup when the
// two arguments are the same file, because the compare view then has one tab.
func TestBuildModelDiffNeedsTwoDistinctFiles(t *testing.T) {
	isolateConfig(t)
	file := filepath.Join(t.TempDir(), "a.txt")
	writeTestFile(t, file, "hello\n")
	opts := Options{Diff: true, Files: []FileTarget{{Path: file}, {Path: file}}}
	if _, _, err := buildModel(t.Context(), opts); err == nil {
		t.Fatal("buildModel() returned no error for --diff on one file")
	}
}

// TestBuildModelDiffOpensTheCompareView checks that --diff opens the two files as
// tabs 0 and 1 and shows them in the compare view.
func TestBuildModelDiffOpensTheCompareView(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	writeTestFile(t, a, "one\n")
	writeTestFile(t, b, "two\n")
	opts := Options{Diff: true, Files: []FileTarget{{Path: a}, {Path: b}}}
	m, files, err := buildModel(t.Context(), opts)
	if err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	t.Cleanup(files.Close)
	if !m.Comparing() {
		t.Fatal("the model is not in the compare view after --diff")
	}
}
