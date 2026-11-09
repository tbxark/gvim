package env

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
)

func venvDetectors() []config.EnvDetector {
	return []config.EnvDetector{{
		Name: ".venv", Markers: []string{".venv"}, Kind: config.EnvKindPathOnly,
		Snippet: "source .venv/bin/activate", Require: []string{".venv/bin/python"},
	}}
}

func TestRemoteDetectionIsTrustGated(t *testing.T) {
	s := &remoteScript{markers: "0\n"}
	trusted := false
	p := NewRemote("/remote/proj", direnvDetectors(), func() bool { return trusted }, s.run)
	p.Warm(context.Background())
	if calls := s.recorded(); len(calls) != 0 {
		t.Fatalf("an untrusted workspace probed the remote host: %v", calls)
	}
	if det, ok := p.Detected(); ok || det.Name != "" {
		t.Fatalf("untrusted detected = %+v, %v", det, ok)
	}
	if notice := p.DetectionNotice(); !strings.Contains(notice, "until this workspace is trusted") {
		t.Fatalf("untrusted detection notice = %q", notice)
	}

	// Trusting the workspace (the live gate flips) re-probes without needing Reload.
	trusted = true
	p.Warm(context.Background())
	if det, ok := p.Detected(); !ok || det.Name != "direnv" {
		t.Fatalf("trusted detected = %+v, %v", det, ok)
	}
	if calls := s.recorded(); len(calls) != 1 {
		t.Fatalf("trusted probe calls = %v", calls)
	}
	if notice := p.DetectionNotice(); notice != "" {
		t.Fatalf("healthy detection notice = %q", notice)
	}

	// Losing trust clears the detection and re-gates the probe.
	trusted = false
	p.Warm(context.Background())
	if det, ok := p.Detected(); ok || det.Name != "" {
		t.Fatalf("redisabled detected = %+v, %v", det, ok)
	}
	if calls := s.recorded(); len(calls) != 1 {
		t.Fatalf("untrusted re-probe ran a remote command: %v", calls)
	}
}

func TestRemoteDetectionProbesRequirePaths(t *testing.T) {
	// The probe must include the detector's require evidence: markers and requires are
	// both existence-checked in the same bounded remote command, matching trust.DetectEnv.
	s := &remoteScript{markers: "0\n1\n"}
	p := NewRemote("/remote/proj", venvDetectors(), func() bool { return true }, s.run)
	p.Warm(context.Background())
	det, ok := p.Detected()
	if !ok || det.Name != ".venv" || det.Kind != config.EnvKindPathOnly {
		t.Fatalf("detected = %+v, %v", det, ok)
	}
	calls := s.recorded()
	if len(calls) != 1 {
		t.Fatalf("probe calls = %v", calls)
	}
	probe := calls[0]
	if !strings.Contains(probe, "[ -e '.venv' ]") || !strings.Contains(probe, "[ -e '.venv/bin/python' ]") {
		t.Fatalf("probe does not existence-check markers and requires: %q", probe)
	}
	if !strings.Contains(probe, "printf '%s\\n' 0") || !strings.Contains(probe, "printf '%s\\n' 1") {
		t.Fatalf("probe does not report both indices: %q", probe)
	}

	// Marker present, require missing: no detector, as locally.
	s.markers = "0\n"
	p = NewRemote("/remote/proj", venvDetectors(), func() bool { return true }, s.run)
	p.Warm(context.Background())
	if det, ok := p.Detected(); ok || det.Name != "" {
		t.Fatalf("marker-only venv detected = %+v, %v", det, ok)
	}
}

func TestRemoteDetectionFailureIsExplicitAndNotCached(t *testing.T) {
	s := &remoteScript{actErr: errors.New("remote host disconnected")}
	// The probe's default branch returns s.markers; make the runner fail on the first
	// probe, then succeed.
	failProbe := true
	run := func(ctx context.Context, command string) (string, error) {
		if strings.Contains(command, "[ -e") && failProbe {
			failProbe = false
			return "", errors.New("remote host disconnected")
		}
		return s.run(ctx, command)
	}
	p := NewRemote("/remote/proj", direnvDetectors(), func() bool { return true }, run)
	p.Warm(context.Background())
	if det, ok := p.Detected(); ok || det.Name != "" {
		t.Fatalf("failed-probe detected = %+v, %v", det, ok)
	}
	if notice := p.DetectionNotice(); !strings.Contains(notice, "marker detection failed") || !strings.Contains(notice, "disconnected") {
		t.Fatalf("failure notice = %q", notice)
	}
	// The failure is not cached: a later successful probe clears it.
	s.markers = "0\n"
	p.Warm(context.Background())
	if det, ok := p.Detected(); !ok || det.Name != "direnv" {
		t.Fatalf("recovered detection = %+v, %v", det, ok)
	}
	if notice := p.DetectionNotice(); notice != "" {
		t.Fatalf("recovered detection notice = %q", notice)
	}
}

func TestRemoteDetectionCancellationIsBounded(t *testing.T) {
	run := func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "0\n", nil
	}
	p := NewRemote("/remote/proj", direnvDetectors(), func() bool { return true }, run)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.Warm(ctx)
	if notice := p.DetectionNotice(); !strings.Contains(notice, "marker detection failed") {
		t.Fatalf("cancelled probe notice = %q", notice)
	}
}
