package trust

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tbxark/gvim/config"
)

// Environment detection. Detection is passive: it stats the marker and require
// paths at the workspace root and never runs anything. trust/markers.go derives
// the environment part of the executable-content markers from the same built-in
// detector list, so the trust prompt and activation cannot disagree about what
// an environment file is.

// DetectedEnv is the environment detected at a workspace root. Snippet has
// {dir} already expanded to Root.
type DetectedEnv struct {
	Name    string
	Kind    config.EnvKind
	Snippet string
}

// DetectEnv finds the first detector whose marker exists and, when it lists
// require paths, whose evidence exists too. incomplete reports a detector whose
// marker was found without its require evidence (a virtualenv still being
// written), so a watcher can retry later. Nothing is executed.
func DetectEnv(root string, detectors []config.EnvDetector) (DetectedEnv, bool) {
	if root == "" {
		return DetectedEnv{}, false
	}
	incomplete := false
	for _, d := range detectors {
		if !anyExists(root, d.Markers) {
			continue
		}
		if len(d.Require) > 0 && !anyExists(root, d.Require) {
			incomplete = true
			continue
		}
		return DetectedEnv{
			Name:    d.Name,
			Kind:    d.Kind,
			Snippet: strings.ReplaceAll(d.Snippet, "{dir}", root),
		}, incomplete
	}
	return DetectedEnv{}, incomplete
}

// EnvMarkers returns the environment marker names of the built-in detectors,
// in detector order, for the executable-content scan.
func EnvMarkers() []string {
	var markers []string
	seen := make(map[string]bool)
	for _, d := range config.DefaultEnvDetectors() {
		for _, m := range d.Markers {
			if !seen[m] {
				seen[m] = true
				markers = append(markers, m)
			}
		}
	}
	return markers
}

// anyExists reports whether any relative path exists under root. The validator
// rejects absolute or escaping paths in the config; this also ignores them
// defensively.
func anyExists(root string, paths []string) bool {
	for _, rel := range paths {
		rel = strings.TrimSpace(rel)
		if rel == "" || filepath.IsAbs(rel) {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, rel)); err == nil && info != nil {
			return true
		}
	}
	return false
}
