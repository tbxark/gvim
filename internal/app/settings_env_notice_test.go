package app

import (
	"context"
	"strings"
	"testing"

	"github.com/tbxark/gvim/env"
)

func TestProjectEnvFactsShowsRemoteShellFallback(t *testing.T) {
	p := env.NewRemote("/remote/proj", nil, func() bool { return true }, func(context.Context, string) (string, error) {
		return "", nil
	})
	p.SetNotice("Remote environment: zsh -ilc failed; using sh -c")
	svc := &settingsService{env: p}
	facts := svc.projectEnvFacts()
	found := false
	for _, f := range facts {
		if f.name == "Environment shell" && strings.Contains(f.note, "using sh -c") {
			found = true
		}
	}
	if !found {
		t.Fatalf("settings env facts = %+v, want the shell fallback notice", facts)
	}
	p.SetNotice("")
	facts = svc.projectEnvFacts()
	for _, f := range facts {
		if f.name == "Environment shell" {
			t.Fatalf("a successful re-probe did not clear the Settings fallback row: %+v", facts)
		}
	}
}
