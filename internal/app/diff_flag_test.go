package app

import (
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/restore"
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

// TestBuildRuntimeDiffSurvivesRecoveredTabs checks that crash recovery, which adds tabs
// after the files of the command line, does not make --diff refuse to start: the compare
// view opens on the two files that were asked for, and the recovered tab stays after them.
func TestBuildRuntimeDiffSurvivesRecoveredTabs(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	a, b, c := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt"), filepath.Join(dir, "c.txt")
	writeTestFile(t, a, "one\n")
	writeTestFile(t, b, "two\n")
	writeTestFile(t, c, "three\n")
	sessDir := filepath.Join(dir, "session")

	crashed := apptest.NewSessionEditor()
	must(t, crashed.OpenFile(c))
	crashed.Do("insert_char", map[string]any{"text": "Z"})
	must(t, restore.NewRecoveryWriter(crashed, restore.RecoveryPath(sessDir)).Plan()())

	opts := projectRoot(t, "")
	opts.SessionDir = sessDir
	opts.Diff = true
	opts.Files = []FileTarget{{Path: a}, {Path: b}}
	rt, err := buildRuntime(t.Context(), opts, true)
	if err != nil {
		t.Fatalf("buildRuntime() with a recovered tab: %v", err)
	}
	t.Cleanup(rt.close)
	if !rt.model.Comparing() {
		t.Fatal("the model is not in the compare view")
	}
	names, _ := apptest.TabNames(rt.ed)
	if len(names) != 3 || names[0] != "a.txt" || names[1] != "b.txt" {
		t.Fatalf("tabs %v, want a.txt and b.txt first, then the recovered tab", names)
	}
}
