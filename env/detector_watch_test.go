package env

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
)

func TestDetectorWatchPathsCustom(t *testing.T) {
	detectors := []config.EnvDetector{
		{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true"},
		{Name: "custom", Markers: []string{"config/env.toml"}, Require: []string{"config/bin/python"}, Kind: config.EnvKindShell, Snippet: "true"},
	}
	spec := DetectorWatchPaths(detectors)
	for _, name := range []string{".nvmrc", "config"} {
		if !spec.RootNames[name] {
			t.Fatalf("root names = %v, want %s", spec.RootNames, name)
		}
	}
	if !spec.RearmNames["config"] {
		t.Fatalf("rearm names = %v, want config", spec.RearmNames)
	}
	if !spec.Dirs["config"]["env.toml"] {
		t.Fatalf("config dir children = %v", spec.Dirs["config"])
	}
	if !spec.Dirs["config/bin"]["python"] {
		t.Fatalf("config/bin dir children = %v", spec.Dirs["config/bin"])
	}
	if spec.Omitted != 0 || spec.OmittedDirs != 0 {
		t.Fatalf("unexpected omissions: %+v", spec)
	}
}

func TestDetectorWatchPathsBound(t *testing.T) {
	var detectors []config.EnvDetector
	for i := 0; i < MaxDetectorWatchDirs+5; i++ {
		detectors = append(detectors, config.EnvDetector{
			Name: "d" + strings.Repeat("x", i), Markers: []string{strings.Repeat("d", i+1) + "/marker"},
			Kind: config.EnvKindShell,
		})
	}
	spec := DetectorWatchPaths(detectors)
	if len(spec.Dirs) != MaxDetectorWatchDirs {
		t.Fatalf("kept %d dirs, want %d", len(spec.Dirs), MaxDetectorWatchDirs)
	}
	if spec.OmittedDirs != 5 || spec.Omitted != 5 {
		t.Fatalf("omissions = dirs %d paths %d, want 5/5", spec.OmittedDirs, spec.Omitted)
	}
}
