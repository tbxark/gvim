package editor

import "testing"

// A reload that the editor starts because the file changed on disk must not say "Reverted":
// that reads as if the user discarded something (PC06). A revert the user asked for still does.
func TestAutoReloadStatusDoesNotSayReverted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		auto bool
		want string
	}{
		{"asked for", false, "Reverted a.txt"},
		{"changed on disk", true, "Reloaded a.txt from disk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, path := openRevertable(t, "a\n")
			d := e.Active()
			putFile(t, path, "a\nb\n")
			plan := e.PlanRevertPath(path)
			plan.Auto = tt.auto
			if err := e.ApplyRevert(plan, RunRevert(plan)); err != nil {
				t.Fatalf("ApplyRevert: %v", err)
			}
			if d.status != tt.want {
				t.Errorf("status = %q, want %q", d.status, tt.want)
			}
			if got := d.Buffer.Text(); got != "a\nb\n" {
				t.Errorf("text = %q, want the new file", got)
			}
		})
	}
}
