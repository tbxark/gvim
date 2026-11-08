package editor

import "testing"

// A key typed while the write is in flight is not in the file, so the status must not
// say "Saved" over a buffer that is modified again.
func TestSavedStatusDoesNotSurviveAnEditThatRacedTheWrite(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "abc\n")
	typeText(t, e, "x")
	p, err := e.PlanSave()
	if err != nil {
		t.Fatalf("PlanSave = %v", err)
	}
	results := p.Run()
	typeText(t, e, "Z")
	if err := e.ApplySavePlan(p, results); err != nil {
		t.Fatalf("ApplySavePlan = %v", err)
	}
	if !e.Active().Modified() {
		t.Fatal("setup: the edit did not leave the buffer modified")
	}
	if got := e.Status(); got == "Saved" {
		t.Errorf("status = %q over a modified buffer, want none", got)
	}
}

// Without an edit in between, the save still says "Saved".
func TestSavedStatusAfterACleanWrite(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "abc\n")
	typeText(t, e, "x")
	p, err := e.PlanSave()
	if err != nil {
		t.Fatalf("PlanSave = %v", err)
	}
	if err := e.ApplySavePlan(p, p.Run()); err != nil {
		t.Fatalf("ApplySavePlan = %v", err)
	}
	if got := e.Status(); got != "Saved" {
		t.Errorf("status = %q, want Saved", got)
	}
}
