package editor

import (
	"errors"
	"testing"
)

// pathDoc returns an editor whose only tab is a document at path holding text. The
// file is not read, so the path can be made up.
func pathDoc(opts Options, path, text string) *Editor {
	e := New(opts)
	e.docs = []*Document{newDocument(path, text)}
	e.switchTo(0)
	return e
}

func TestCopyFilePath(t *testing.T) {
	t.Parallel()
	const path = "/work/proj/src/main.go"
	tests := []struct {
		name   string
		path   string
		root   string
		action Action
		want   string
	}{
		{name: "the absolute path is copied", path: path, root: "/work/proj", action: "copy_file_path", want: path},
		{name: "the absolute path is copied when no root is known", path: path, action: "copy_file_path", want: path},
		{name: "the path is relative to the root", path: path, root: "/work/proj", action: "copy_relative_file_path", want: "src/main.go"},
		{name: "a root with a trailing slash also works", path: path, root: "/work/proj/", action: "copy_relative_file_path", want: "src/main.go"},
		{name: "a file outside the root gives its absolute path", path: path, root: "/work/other", action: "copy_relative_file_path", want: path},
		{name: "a file with no root known gives its absolute path", path: path, action: "copy_relative_file_path", want: path},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := pathDoc(Options{Root: tt.root}, tt.path, "text")
			e.clipboard = "before"
			r := e.Do(tt.action, nil)
			if r.Err != nil {
				t.Fatalf("Do(%s): %v", tt.action, r.Err)
			}
			if r.Text != tt.want {
				t.Fatalf("Result.Text = %q, want %q", r.Text, tt.want)
			}
			if e.clipboard != tt.want {
				t.Fatalf("clipboard = %q, want %q", e.clipboard, tt.want)
			}
			if want := "Copied path: " + tt.want; e.Status() != want {
				t.Fatalf("status = %q, want %q", e.Status(), want)
			}
		})
	}
}

// TestSetRootMovesTheRelativePath checks that after a project switch the relative path is
// measured from the new root. A file under the old root gives its absolute path then.
func TestSetRootMovesTheRelativePath(t *testing.T) {
	t.Parallel()
	const path = "/work/other/src/main.go"
	e := pathDoc(Options{Root: "/work/proj"}, path, "text")
	e.SetRoot("/work/other")
	r := e.Do("copy_relative_file_path", nil)
	if r.Err != nil {
		t.Fatalf("Do(copy_relative_file_path): %v", r.Err)
	}
	if want := "src/main.go"; r.Text != want {
		t.Fatalf("Result.Text = %q, want %q after SetRoot", r.Text, want)
	}
	e.SetRoot("/work/proj")
	if r := e.Do("copy_relative_file_path", nil); r.Text != path {
		t.Fatalf("Result.Text = %q, want the absolute path outside the root %q", r.Text, "/work/proj")
	}
}

func TestCopyPathOfUntitledBufferFails(t *testing.T) {
	t.Parallel()
	for _, a := range []Action{"copy_file_path", "copy_relative_file_path"} {
		t.Run(string(a), func(t *testing.T) {
			t.Parallel()
			e := pathDoc(Options{Root: "/work"}, "", "text")
			e.clipboard = "keep"
			if r := e.Do(a, nil); !errors.Is(r.Err, ErrNoPath) {
				t.Fatalf("err = %v, want ErrNoPath", r.Err)
			}
			if e.clipboard != "keep" {
				t.Fatalf("clipboard = %q, want it unchanged", e.clipboard)
			}
		})
	}
}

// TestCopyPathThenPasteInsertsIt checks that the editor's own Paste gets the path that a copy
// action copied, so the path can be pasted inside gvim.
func TestCopyPathThenPasteInsertsIt(t *testing.T) {
	t.Parallel()
	const path = "/work/proj/src/main.go"
	tests := []struct {
		name   string
		root   string
		action Action
		want   string
	}{
		{name: "the absolute path", action: "copy_file_path", want: path},
		{name: "the relative path", root: "/work/proj", action: "copy_relative_file_path", want: "src/main.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := pathDoc(Options{Root: tt.root}, path, "")
			run(t, e, tt.action, "paste")
			if got := text(e); got != tt.want {
				t.Fatalf("text after paste = %q, want %q", got, tt.want)
			}
		})
	}
}
