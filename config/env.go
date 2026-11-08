package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Environment detection (Fresh's env.detectors).
//
// A detector names a project environment by the markers at the workspace root
// and the shell snippet that activates it. Detection is a shallow marker scan
// (see trust.DetectEnv); nothing here runs. The snippet runs only when the
// workspace is trusted and the env-manager plugin activates it (see the env
// package and docs/trust.md).
//
// The project layer cannot set env.detectors: a repository must not decide
// which command the editor runs in it.

// EnvKind is how risky activating one environment is. The zero value is kept
// invalid on purpose so a detector that omits kind is rejected.
type EnvKind string

// The two activation kinds, Fresh's names.
const (
	// EnvKindPathOnly changes PATH and a few variables (a Python virtualenv).
	EnvKindPathOnly EnvKind = "path-only"
	// EnvKindShell runs project shell code (direnv, mise, pipenv, poetry).
	EnvKindShell EnvKind = "shell"
)

// EnvDetector is one entry of env.detectors.
type EnvDetector struct {
	// Name is the short label (the status pill and the Settings page show it).
	Name string `json:"name"`
	// Markers are paths at the workspace root; any existing one matches.
	Markers []string `json:"markers"`
	// Kind is the activation risk class.
	Kind EnvKind `json:"kind"`
	// Snippet is the shell command that activates the environment, run with the
	// workspace root as the working directory. {dir} expands to that root.
	Snippet string `json:"snippet"`
	// Require lists paths of which at least one must exist too (the interpreter
	// inside a virtualenv). Empty means the markers are enough.
	Require []string `json:"require,omitempty"`
}

// EnvConfig is the config "env" section.
type EnvConfig struct {
	// Detectors replaces the built-in list wholesale when the user config sets
	// it (Fresh's rule). The defaults are DefaultEnvDetectors.
	Detectors []EnvDetector
}

// Limits of one config's env section.
const (
	MaxEnvDetectors   = 64
	MaxEnvMarkers     = 16
	MaxEnvRequire     = 16
	MaxEnvNameBytes   = 80
	MaxEnvPathBytes   = 256
	MaxEnvSnippetSize = 4 << 10
)

// pythonVenvRequire is the interpreter evidence of a Python virtualenv: a bare
// .venv directory without one may still be written and must not activate.
func pythonVenvRequire(dir string) []string {
	return []string{
		dir + "/bin/python",
		dir + "/bin/python3",
		dir + "/Scripts/python.exe",
	}
}

// DefaultEnvDetectors returns Fresh's built-in environment detectors in his
// order. It is the single list shared by trust's marker scan and activation
// detection.
func DefaultEnvDetectors() []EnvDetector {
	return []EnvDetector{
		{
			Name: ".venv", Markers: []string{".venv"}, Kind: EnvKindPathOnly,
			Snippet: "source .venv/bin/activate", Require: pythonVenvRequire(".venv"),
		},
		{
			Name: "venv", Markers: []string{"venv"}, Kind: EnvKindPathOnly,
			Snippet: "source venv/bin/activate", Require: pythonVenvRequire("venv"),
		},
		{
			Name: "direnv", Markers: []string{".envrc"}, Kind: EnvKindShell,
			Snippet: "eval \"$(direnv export bash)\"",
		},
		{
			Name: "mise", Markers: []string{"mise.toml", ".mise.toml", ".tool-versions"}, Kind: EnvKindShell,
			Snippet: "eval \"$(mise env -s bash)\"",
		},
		{
			Name: "pipenv", Markers: []string{"Pipfile"}, Kind: EnvKindShell,
			Snippet: "source \"$(pipenv --venv)/bin/activate\"",
		},
		{
			Name: "poetry", Markers: []string{"poetry.lock"}, Kind: EnvKindShell,
			Snippet: "source \"$(poetry env info --path)/bin/activate\"",
		},
	}
}

// DefaultEnvConfig is the config env section with the built-in detectors.
func DefaultEnvConfig() EnvConfig { return EnvConfig{Detectors: DefaultEnvDetectors()} }

// EnvIsPathOnly reports whether d is the low-risk virtualenv kind.
func (d EnvDetector) EnvIsPathOnly() bool { return d.Kind == EnvKindPathOnly }

// setEnv applies the env object of a config file. It is only called for the user
// layer (see applyObject).
func (c *Config) setEnv(raw json.RawMessage) []Diagnostic {
	var object struct {
		Detectors *[]EnvDetector `json:"detectors"`
	}
	if err := decodeStrict(raw, &object); err != nil {
		return []Diagnostic{{Key: "env", Message: err.Error()}}
	}
	if object.Detectors == nil {
		// No detectors key keeps the built-in list.
		return nil
	}
	detectors := *object.Detectors
	if len(detectors) > MaxEnvDetectors {
		return []Diagnostic{{Key: "env.detectors", Message: fmt.Sprintf("at most %d detectors, got %d", MaxEnvDetectors, len(detectors))}}
	}
	if errs := ValidateEnvDetectors(detectors); len(errs) > 0 {
		return []Diagnostic{{Key: "env.detectors", Message: strings.Join(errs, "; ")}}
	}
	if detectors == nil {
		detectors = []EnvDetector{}
	}
	c.Env = EnvConfig{Detectors: detectors}
	return nil
}

// ValidateEnvDetectors returns the problems of a detector list, each naming the
// detector index and field.
func ValidateEnvDetectors(detectors []EnvDetector) []string {
	var problems []string
	seen := make(map[string]int, len(detectors))
	for i, d := range detectors {
		where := fmt.Sprintf("detectors[%d]", i)
		name := strings.TrimSpace(d.Name)
		if name == "" {
			problems = append(problems, where+".name must not be empty")
		} else if len(name) > MaxEnvNameBytes {
			problems = append(problems, fmt.Sprintf("%s.name is longer than %d bytes", where, MaxEnvNameBytes))
		} else if first, ok := seen[name]; ok {
			problems = append(problems, fmt.Sprintf("%s.name %q is also detectors[%d]", where, name, first))
		} else {
			seen[name] = i
		}
		if len(d.Markers) == 0 {
			problems = append(problems, where+".markers must not be empty")
		} else if len(d.Markers) > MaxEnvMarkers {
			problems = append(problems, fmt.Sprintf("%s.markers has more than %d entries", where, MaxEnvMarkers))
		}
		for j, marker := range d.Markers {
			problems = append(problems, validateEnvPath(fmt.Sprintf("%s.markers[%d]", where, j), marker)...)
		}
		if d.Kind != EnvKindPathOnly && d.Kind != EnvKindShell {
			problems = append(problems, fmt.Sprintf("%s.kind must be %q or %q", where, EnvKindPathOnly, EnvKindShell))
		}
		if strings.TrimSpace(d.Snippet) == "" {
			problems = append(problems, where+".snippet must not be empty")
		} else if len(d.Snippet) > MaxEnvSnippetSize {
			problems = append(problems, fmt.Sprintf("%s.snippet is longer than %d bytes", where, MaxEnvSnippetSize))
		}
		if len(d.Require) > MaxEnvRequire {
			problems = append(problems, fmt.Sprintf("%s.require has more than %d entries", where, MaxEnvRequire))
		}
		for j, require := range d.Require {
			problems = append(problems, validateEnvPath(fmt.Sprintf("%s.require[%d]", where, j), require)...)
		}
	}
	return problems
}

// validateEnvPath rejects a detector path that could escape the workspace root
// or is not a plain relative path.
func validateEnvPath(where, path string) []string {
	trimmed := strings.TrimSpace(path)
	switch {
	case trimmed == "":
		return []string{where + " must not be empty"}
	case len(trimmed) > MaxEnvPathBytes:
		return []string{fmt.Sprintf("%s is longer than %d bytes", where, MaxEnvPathBytes)}
	case strings.ContainsAny(trimmed, "\x00\n\r"):
		return []string{where + " must not contain control characters"}
	case filepath.IsAbs(trimmed):
		return []string{where + " must be relative to the workspace root"}
	case trimmed == ".." || strings.HasPrefix(trimmed, "../") || strings.HasPrefix(trimmed, "..\\"):
		return []string{where + " must not leave the workspace root"}
	}
	return nil
}

// EnvDetectors returns the detectors the config resolved: the user list, or the
// built-ins (also when Config.Env has no list, as the zero value does not).
func (c Config) EnvDetectors() []EnvDetector {
	if len(c.Env.Detectors) > 0 {
		return slices.Clone(c.Env.Detectors)
	}
	return DefaultEnvDetectors()
}
