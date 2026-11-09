package env

// Bounded watch plan for user-configured detector markers/requires. The fixed env-input
// watcher covers .envrc/mise/...; custom env.detectors can name other project paths, so the
// app additionally watches their parent directories through the authority file watch. The
// plan is deliberately bounded: at most MaxDetectorWatchPaths paths and
// MaxDetectorWatchDirs directories are tracked; the rest are reported (a manual Env: Reload
// still works) instead of creating an unbounded number of remote poll watchers.

import (
	"path"
	"sort"
	"strings"

	"github.com/tbxark/gvim/config"
)

const (
	// MaxDetectorWatchPaths bounds the marker/require paths considered for watching.
	MaxDetectorWatchPaths = MaxMarkerProbePaths
	// MaxDetectorWatchDirs bounds the parent directories watched for custom detectors.
	MaxDetectorWatchDirs = 32
)

// DetectorWatchSpec is the bounded watch plan of one detector set. RootNames are the
// top-level entries a workspace-root watch must treat as relevant; Dirs maps a
// workspace-relative directory to the child names relevant inside it; RearmNames are the
// root-level directory names whose appearance means the plan should be rebuilt (a parent
// watched directory may now exist).
type DetectorWatchSpec struct {
	RootNames   map[string]bool
	RearmNames  map[string]bool
	Dirs        map[string]map[string]bool
	OmittedDirs int
	Omitted     int // paths omitted because of the path/dir bounds
}

// DetectorWatchPaths builds the bounded plan for detectors. Absolute and escaping paths
// are ignored defensively (the config validator already rejects them).
func DetectorWatchPaths(detectors []config.EnvDetector) DetectorWatchSpec {
	spec := DetectorWatchSpec{
		RootNames:  map[string]bool{},
		RearmNames: map[string]bool{},
		Dirs:       map[string]map[string]bool{},
	}
	type planned struct {
		root  string
		dir   string
		child string
	}
	var all []planned
	considered := 0
	seen := map[string]bool{}
	add := func(rel string) {
		rel = strings.TrimSpace(rel)
		if rel == "" || strings.HasPrefix(rel, "/") || seen[rel] {
			return
		}
		seen[rel] = true
		considered++
		if considered > MaxDetectorWatchPaths {
			spec.Omitted++
			return
		}
		rel = path.Clean(rel)
		if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
			return
		}
		parts := strings.Split(rel, "/")
		root := parts[0]
		if len(parts) == 1 {
			all = append(all, planned{root: root})
			return
		}
		// Root-level events for the first directory segment still matter (creation of the
		// watched parent itself).
		spec.RearmNames[root] = true
		all = append(all, planned{root: root, dir: path.Dir(rel), child: path.Base(rel)})
	}
	for _, d := range detectors {
		for _, rel := range d.Markers {
			add(rel)
		}
		for _, rel := range d.Require {
			add(rel)
		}
	}
	for _, p := range all {
		spec.RootNames[p.root] = true
		if p.dir != "" && p.dir != "." {
			if spec.Dirs[p.dir] == nil {
				spec.Dirs[p.dir] = map[string]bool{}
			}
			spec.Dirs[p.dir][p.child] = true
		}
	}
	// Deterministically keep the first MaxDetectorWatchDirs directories. Paths whose
	// parent directory is dropped are counted as omitted too.
	if len(spec.Dirs) > MaxDetectorWatchDirs {
		dirs := make([]string, 0, len(spec.Dirs))
		for dir := range spec.Dirs {
			dirs = append(dirs, dir)
		}
		sort.Strings(dirs)
		kept := map[string]bool{}
		for _, dir := range dirs[:MaxDetectorWatchDirs] {
			kept[dir] = true
		}
		for _, dir := range dirs[MaxDetectorWatchDirs:] {
			spec.OmittedDirs++
			spec.Omitted += len(spec.Dirs[dir])
			delete(spec.Dirs, dir)
		}
	}
	return spec
}
