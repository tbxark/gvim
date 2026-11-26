package editor

import (
	"errors"
	"github.com/tbxark/gvim/document"
	"testing"
)

// TestSaveResultWritten checks which results name a written file: the path is the one
// written and the id is the document's.
func TestSaveResultWritten(t *testing.T) {
	t.Parallel()
	ed, path := openForAutoSave(t, "abc\n")
	id := ed.Active().ID()
	typeIntoDoc(t, ed, "x")
	plan, err := ed.PlanSave()
	if err != nil {
		t.Fatal(err)
	}
	ok := plan.Run()
	if len(ok) != 1 {
		t.Fatalf("PlanSave ran %d jobs, want 1", len(ok))
	}
	failed := document.SaveResult{Job: ok[0].Job}.WithErr(errors.New("disk full"))
	skipped := document.SaveResult{Job: ok[0].Job, Skipped: true}
	tests := []struct {
		Name     string
		r        document.SaveResult
		wantOK   bool
		wantPath string
		wantID   document.DocID
	}{
		{"written", ok[0], true, path, id},
		{"failed", failed, false, "", 0},
		{"large save with nothing to write", skipped, false, "", 0},
		{"zero value", document.SaveResult{}, false, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			p, d, gotOK := tt.r.Written()
			if gotOK != tt.wantOK || p != tt.wantPath || d != tt.wantID {
				t.Fatalf("Written() = (%q, %d, %v), want (%q, %d, %v)", p, d, gotOK, tt.wantPath, tt.wantID, tt.wantOK)
			}
		})
	}
}
