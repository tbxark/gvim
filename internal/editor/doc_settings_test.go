package editor

import "testing"

func TestResetBufferSettingsKeepsTheText(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "a")
	typeText(t, e, "x")
	run(t, e, "reset_buffer_settings")
	d := e.Active()
	if got := d.Buffer.Text(); got != "xa" {
		t.Fatalf("text = %q, want %q", got, "xa")
	}
	if !d.Modified() {
		t.Fatal("reset_buffer_settings cleared the modified state")
	}
	if want := "Buffer settings reset"; e.Status() != want {
		t.Fatalf("status = %q, want %q", e.Status(), want)
	}
}
