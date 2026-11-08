package cli

import (
	"errors"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/internal/app"
)

// fakeFS is a stat function over a fixed set of paths. The value says whether
// the path is a directory. Paths not in the map do not exist.
type fakeFS map[string]bool

func (f fakeFS) stat(name string) (fs.FileInfo, error) {
	isDir, ok := f[name]
	if !ok {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	return fakeInfo{name: filepath.Base(name), dir: isDir}, nil
}

type fakeInfo struct {
	name string
	dir  bool
}

func (i fakeInfo) Name() string { return i.name }
func (i fakeInfo) Size() int64  { return 0 }
func (i fakeInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return i.dir }
func (i fakeInfo) Sys() any           { return nil }

func TestParseTargets(t *testing.T) {
	disk := fakeFS{
		"a.txt":     false,
		"b.txt":     false,
		"a.txt:12":  false, // a file literally named with a suffix
		"dir":       true,
		"other":     true,
		"notes:one": false,
	}
	tests := []struct {
		name string
		args []string
		want Targets
	}{
		{name: "empty args", args: nil, want: Targets{}},
		{name: "empty slice", args: []string{}, want: Targets{}},
		{
			name: "existing file",
			args: []string{"a.txt"},
			want: Targets{Files: []app.FileTarget{{Path: "a.txt"}}},
		},
		{
			name: "file with line",
			args: []string{"a.txt:12"},
			want: Targets{Files: []app.FileTarget{{Path: "a.txt:12"}}},
		},
		{
			name: "file with line when the literal name is not a file",
			args: []string{"b.txt:12"},
			want: Targets{Files: []app.FileTarget{{Path: "b.txt", Line: 12}}},
		},
		{
			name: "file with line and column",
			args: []string{"b.txt:12:3"},
			want: Targets{Files: []app.FileTarget{{Path: "b.txt", Line: 12, Col: 3}}},
		},
		{
			name: "missing file opens as a new buffer",
			args: []string{"new.go"},
			want: Targets{Files: []app.FileTarget{{Path: "new.go"}}},
		},
		{
			name: "missing file with line and column",
			args: []string{"new.go:4:2"},
			want: Targets{Files: []app.FileTarget{{Path: "new.go", Line: 4, Col: 2}}},
		},
		{
			name: "directory alone is the root",
			args: []string{"dir"},
			want: Targets{Root: "dir"},
		},
		{
			name: "directory then file",
			args: []string{"dir", "b.txt"},
			want: Targets{Root: "dir", Files: []app.FileTarget{{Path: "b.txt"}}},
		},
		{
			name: "file then directory keeps file order",
			args: []string{"b.txt", "dir", "missing.md"},
			want: Targets{Root: "dir", Files: []app.FileTarget{{Path: "b.txt"}, {Path: "missing.md"}}},
		},
		{
			name: "zero line is not a position",
			args: []string{"b.txt:0"},
			want: Targets{Files: []app.FileTarget{{Path: "b.txt:0"}}},
		},
		{
			name: "zero column is not a position",
			args: []string{"b.txt:3:0"},
			want: Targets{Files: []app.FileTarget{{Path: "b.txt:3:0"}}},
		},
		{
			name: "suffix without digits stays in the path",
			args: []string{"notes:two"},
			want: Targets{Files: []app.FileTarget{{Path: "notes:two"}}},
		},
		{
			name: "colon in the path before the position",
			args: []string{"x:notes:5"},
			want: Targets{Files: []app.FileTarget{{Path: "x:notes", Line: 5}}},
		},
		{
			name: "leading colon is not a position",
			args: []string{":5"},
			want: Targets{Files: []app.FileTarget{{Path: ":5"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTargets(tt.args, disk.stat)
			if err != nil {
				t.Fatalf("ParseTargets(%q) error = %v", tt.args, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseTargets(%q) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseTargetsErrors(t *testing.T) {
	disk := fakeFS{"dir": true, "other": true, "a.txt": false}
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "two directories", args: []string{"dir", "other"}, wantErr: "only one directory"},
		{name: "two directories with a file", args: []string{"dir", "a.txt", "other"}, wantErr: "only one directory"},
		{name: "position on a directory", args: []string{"dir:3"}, wantErr: "needs a file, not a directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTargets(tt.args, disk.stat)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseTargets(%q) error = %v, want %q", tt.args, err, tt.wantErr)
			}
		})
	}
}

func TestParseTargetsStatErrorIsNotFatal(t *testing.T) {
	stat := func(string) (fs.FileInfo, error) { return nil, errors.New("permission denied") }
	got, err := ParseTargets([]string{"locked.txt"}, stat)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	want := []app.FileTarget{{Path: "locked.txt"}}
	if !reflect.DeepEqual(got.Files, want) {
		t.Fatalf("files = %+v, want %+v", got.Files, want)
	}
}

func TestSplitPosition(t *testing.T) {
	tests := []struct {
		arg      string
		wantPath string
		wantLine int
		wantCol  int
		wantOK   bool
	}{
		{arg: "main.go", wantOK: false},
		{arg: "main.go:7", wantPath: "main.go", wantLine: 7, wantOK: true},
		{arg: "main.go:7:9", wantPath: "main.go", wantLine: 7, wantCol: 9, wantOK: true},
		{arg: "main.go:7:9:2", wantPath: "main.go:7", wantLine: 9, wantCol: 2, wantOK: true},
		{arg: "main.go:0", wantOK: false},
		{arg: "main.go:-1", wantOK: false},
		{arg: "main.go:+1", wantOK: false},
		{arg: "main.go:", wantOK: false},
		{arg: ":7", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			path, line, col, ok := splitPosition(tt.arg)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if path != tt.wantPath || line != tt.wantLine || col != tt.wantCol {
				t.Fatalf("got (%q, %d, %d), want (%q, %d, %d)", path, line, col, tt.wantPath, tt.wantLine, tt.wantCol)
			}
		})
	}
}

// TestParseRemoteTargets checks the remote forms reach Targets.Remote and never touch
// the local file system.
func TestParseRemoteTargets(t *testing.T) {
	t.Setenv("USER", "envuser")
	disk := fakeFS{"dir": true}
	tests := []struct {
		name string
		arg  string
		want authority.RemoteSpec
	}{
		{"scp", "deploy@host:/etc/x", authority.RemoteSpec{User: "deploy", Host: "host", Path: "/etc/x"}},
		{"scp line col", "u@h:/a/b.go:4:7", authority.RemoteSpec{User: "u", Host: "h", Path: "/a/b.go", Line: 4, Col: 7}},
		{"url", "ssh://host.example/srv/x", authority.RemoteSpec{User: "envuser", Host: "host.example", Path: "/srv/x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTargets([]string{tt.arg}, disk.stat)
			if err != nil {
				t.Fatalf("ParseTargets(%q): %v", tt.arg, err)
			}
			if got.Remote == nil {
				t.Fatalf("ParseTargets(%q) has no remote target", tt.arg)
			}
			tt.want.Raw = tt.arg
			if *got.Remote != tt.want {
				t.Fatalf("remote = %+v, want %+v", *got.Remote, tt.want)
			}
			if len(got.Files) != 0 || got.Root != "" {
				t.Fatalf("remote launch parsed local targets: %+v", got)
			}
		})
	}
}

// TestParseRemoteTargetErrors checks the startup errors of a remote launch.
func TestParseRemoteTargetErrors(t *testing.T) {
	t.Setenv("USER", "")
	disk := fakeFS{"dir": true, "a.txt": false}
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"malformed url", []string{"ssh://"}, "missing host and path"},
		{"two remotes", []string{"u@h:/a", "u@h:/b"}, "only one remote target"},
		{"remote and file", []string{"u@h:/a", "a.txt"}, "cannot be combined with local"},
		{"remote and dir", []string{"u@h:/a", "dir"}, "cannot be combined with local"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTargets(tt.args, disk.stat)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseTargets(%v) error = %v, want %q", tt.args, err, tt.wantErr)
			}
		})
	}
}
