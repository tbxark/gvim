package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloseActionClosesTheActiveTab(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		dirty   bool
		Args    map[string]any
		wantLen int
		wantErr error
	}{
		{Name: "a clean tab closes", wantLen: 1},
		{Name: "a modified tab is kept without force", dirty: true, wantLen: 2, wantErr: ErrUnsaved},
		{Name: "force discards a modified tab", dirty: true, Args: map[string]any{"force": true}, wantLen: 1},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := withTabs("a.txt", "b.txt")
			if tt.dirty {
				typeText(t, e, "x")
			}
			r := e.Do("close", tt.Args)
			if !errors.Is(r.Err, tt.wantErr) {
				t.Fatalf("close err = %v, want %v", r.Err, tt.wantErr)
			}
			if got := len(e.docs); got != tt.wantLen {
				t.Fatalf("tabs = %d, want %d", got, tt.wantLen)
			}
		})
	}
}

func TestCloseWithNoTabIsAnError(t *testing.T) {
	t.Parallel()
	e := New(Options{Empty: true})
	if r := e.Do("close", nil); !errors.Is(r.Err, ErrNoDocument) {
		t.Fatalf("close with no tab err = %v, want ErrNoDocument", r.Err)
	}
}

func TestSaveAllSavesNamedModifiedTabs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name          string
		namedDirty    bool // a named file with unsaved edits
		namedClean    bool // a named file without edits
		untitledDirty bool // an Untitled-N buffer with text
		changedOnDisk bool // the dirty file changed on disk after it was opened
		wantErr       bool
		wantStatus    string
		wantFile      string // content of the dirty file after save_all, if checked
	}{
		{
			Name:          "saves named edits and skips the untitled buffer",
			namedDirty:    true,
			namedClean:    true,
			untitledDirty: true,
			wantStatus:    "Saved 1 file; skipped 1 untitled",
			wantFile:      "xone\n",
		},
		{
			Name:       "nothing modified",
			namedClean: true,
			wantStatus: "No modified files to save",
		},
		{
			Name:          "only an untitled buffer is modified",
			untitledDirty: true,
			wantStatus:    "No files saved; skipped 1 untitled",
		},
		{
			Name:          "a file changed on disk is not overwritten",
			namedDirty:    true,
			changedOnDisk: true,
			wantErr:       true,
			wantStatus:    "changed on disk",
			wantFile:      "external\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			dirty := filepath.Join(dir, "dirty.txt")
			clean := filepath.Join(dir, "clean.txt")
			if err := os.WriteFile(dirty, []byte("one\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(clean, []byte("two\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			e := New(Options{})
			if tt.namedDirty {
				if err := e.OpenFile(dirty); err != nil {
					t.Fatalf("OpenFile(dirty): %v", err)
				}
				typeText(t, e, "x")
			}
			if tt.namedClean {
				if err := e.OpenFile(clean); err != nil {
					t.Fatalf("OpenFile(clean): %v", err)
				}
			}
			if tt.untitledDirty {
				navDo(t, e, "new")
				typeText(t, e, "scratch")
			}
			if tt.changedOnDisk {
				if err := os.WriteFile(dirty, []byte("external\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			r := e.Do("save_all", nil)
			if (r.Err != nil) != tt.wantErr {
				t.Fatalf("save_all err = %v, want error %v", r.Err, tt.wantErr)
			}
			if got := e.Status(); !strings.Contains(got, tt.wantStatus) {
				t.Fatalf("status = %q, want it to contain %q", got, tt.wantStatus)
			}
			if tt.wantFile != "" {
				got, err := os.ReadFile(dirty)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != tt.wantFile {
					t.Fatalf("dirty file = %q, want %q", got, tt.wantFile)
				}
			}
			if tt.untitledDirty && !e.docs[len(e.docs)-1].Modified() {
				t.Fatal("the untitled buffer was saved or cleared, want it left modified")
			}
		})
	}
}
