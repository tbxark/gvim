package pluginhost

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/pluginapi"
)

// largeIndexedPluginEditor opens a file of the given number of 13 byte lines in large file
// mode, with its line index complete.
func largeIndexedPluginEditor(t *testing.T, lines int) (*pluginEditor, *editor.Document, pluginapi.BufferID) {
	t.Helper()
	var sb strings.Builder
	for i := range lines {
		fmt.Fprintf(&sb, "line %07d x\n", i)
	}
	path := filepath.Join(t.TempDir(), "big.txt")
	apptest.Must(t, os.WriteFile(path, []byte(sb.String()), 0o644))
	ed := editor.New(editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1 << 20})
	apptest.Must(t, ed.OpenFile(path))
	for {
		job, ok := ed.PlanIndex()
		if !ok || !ed.ApplyIndex(job.Run(t.Context())) {
			break
		}
	}
	d := ed.Active()
	if !d.Large() || !d.Index().Complete {
		t.Fatal("the document is not large with a complete index")
	}
	pe := newPluginEditor(ed, func(*editor.Document) {})
	return pe, d, pe.ListBuffers()[0].ID
}

// TestPluginRefusalsOnALargeFileDoNotReadTheText checks that a read or an edit that is over
// its cap is refused from the line index: it used to sum the lengths of the lines one by one,
// which reads as many bytes as the cap before it says no.
func TestPluginRefusalsOnALargeFileDoNotReadTheText(t *testing.T) {
	pe, d, id := largeIndexedPluginEditor(t, 1_500_000) // about 20 MB
	whole := pluginapi.Range{To: pluginapi.Pos{Line: 1 << 30}}
	tests := []struct {
		name string
		call func() error
		code pluginapi.Code
	}{
		{"text of the whole file", func() error { _, err := pe.Slice(id, whole); return err }, pluginapi.CodeTooLarge},
		{"text of a range over the cap", func() error {
			_, err := pe.Slice(id, pluginapi.Range{From: pluginapi.Pos{Line: 100}, To: pluginapi.Pos{Line: 1_400_000, Col: 3}})
			return err
		}, pluginapi.CodeTooLarge},
		{"set_text", func() error {
			return pe.Edit(id, []pluginapi.Edit{{Range: whole, Text: "hello"}}, pluginapi.EditOptions{})
		}, pluginapi.CodeLimit},
		{"delete of a range over the cap", func() error {
			return pe.Edit(id, []pluginapi.Edit{{Range: pluginapi.Range{To: pluginapi.Pos{Line: 1_000_000}}}}, pluginapi.EditOptions{})
		}, pluginapi.CodeLimit},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := d.Buffer.Stats()
			err := tc.call()
			if got := pluginapi.CodeOf(err); got != tc.code {
				t.Fatalf("error = %v (code %q), want code %q", err, got, tc.code)
			}
			after := d.Buffer.Stats()
			if read := after.ReadBytes - before.ReadBytes; read > 1<<20 {
				t.Errorf("the refused call read %d bytes of the file", read)
			}
			if after.LineCalls-before.LineCalls > 10 {
				t.Errorf("the refused call read %d lines", after.LineCalls-before.LineCalls)
			}
		})
	}
	// A request within the caps still reads and edits.
	lines, err := pe.Lines(id, 700_000, 3)
	if err != nil || len(lines) != 3 || lines[0] != "line 0700000 x" {
		t.Fatalf("Lines = %q, %v", lines, err)
	}
	text, err := pe.Slice(id, pluginapi.Range{From: pluginapi.Pos{Line: 700_000}, To: pluginapi.Pos{Line: 700_002}})
	if err != nil || text != "line 0700000 x\nline 0700001 x\n" {
		t.Fatalf("Slice = %q, %v", text, err)
	}
}
