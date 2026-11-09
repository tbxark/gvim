package app

import (
	"context"
	"strings"
	"testing"

	"github.com/tbxark/gvim/env"
)

func TestProjectEnvFactsShowsRemoteDetectionNotice(t *testing.T) {
	trusted := false
	p := env.NewRemote("/remote/proj", nil, func() bool { return trusted }, func(context.Context, string) (string, error) {
		return "", nil
	})
	p.Warm(context.Background())
	svc := &settingsService{env: p}
	facts := svc.projectEnvFacts()
	found := false
	for _, f := range facts {
		if f.name == "Environment detection" && strings.Contains(f.note, "needs a trusted workspace") {
			found = true
		}
	}
	if !found {
		t.Fatalf("settings env facts = %+v, want the untrusted detection row", facts)
	}
	trusted = true
	p.Warm(context.Background())
	facts = svc.projectEnvFacts()
	for _, f := range facts {
		if f.name == "Environment detection" {
			t.Fatalf("trusted detection left a degraded Settings row: %+v", facts)
		}
	}
}

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

	// A backend that cannot watch env inputs degrades to a visible Settings row.
	p.SetWatchNotice("Remote environment: this authority cannot watch files; use Env: Reload")
	found = false
	facts = svc.projectEnvFacts()
	for _, f := range facts {
		if f.name == "Environment watch" && strings.Contains(f.note, "Env: Reload") {
			found = true
		}
	}
	if !found {
		t.Fatalf("settings env facts = %+v, want the env-watch degradation row", facts)
	}
	p.SetWatchNotice("")
	facts = svc.projectEnvFacts()
	for _, f := range facts {
		if f.name == "Environment watch" {
			t.Fatalf("a working watcher did not clear the Settings row: %+v", facts)
		}
	}
}
