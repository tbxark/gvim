package env

import (
	"context"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
)

func TestRemoteUsesCaptureRunnerForActivationOnly(t *testing.T) {
	var detectCalls, captureCalls int
	detect := func(_ context.Context, command string) (string, error) {
		detectCalls++
		return "0\n", nil
	}
	capture := func(_ context.Context, command string) (string, error) {
		captureCalls++
		if strings.Contains(command, "set -a;") {
			return "HOME=/home/u\x00PATH=/venv/bin\x00REMOTE_ONLY=1\x00", nil
		}
		return "HOME=/home/u\x00PATH=/usr/bin\x00", nil
	}
	detectors := []config.EnvDetector{{Name: "direnv", Markers: []string{".envrc"}, Kind: config.EnvKindShell, Snippet: "eval \"$(direnv export sh)\""}}
	p := NewRemote("/remote/proj", detectors, func() bool { return true }, detect, capture)
	if err := p.Set("eval \"$(direnv export sh)\"", "", config.EnvKindShell); err != nil {
		t.Fatal(err)
	}
	overlay := p.Overlay()
	if detectCalls == 0 {
		t.Fatal("detection did not use the detection runner")
	}
	if captureCalls < 2 {
		t.Fatalf("capture calls = %d, want baseline and activation through the capture runner", captureCalls)
	}
	want := "PATH=/venv/bin,REMOTE_ONLY=1"
	if strings.Join(overlay, ",") != want {
		t.Fatalf("overlay = %v, want %s", overlay, want)
	}
	if p.Notice() != "" {
		t.Fatalf("unexpected notice = %q", p.Notice())
	}
	p.SetNotice("Remote environment: fallback notice")
	if got := p.Notice(); got != "Remote environment: fallback notice" {
		t.Fatalf("notice = %q", got)
	}
}
