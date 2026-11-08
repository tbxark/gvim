package trust

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// envMarkers are the environment-file markers Fresh's built-in env detectors
// use. The presence of one, as a file or as a directory (a virtualenv is a
// directory), means a project environment may be activated in the folder.
var envMarkers = []string{
	".venv",
	"venv",
	".envrc",
	"mise.toml",
	".mise.toml",
	".tool-versions",
	"Pipfile",
	"poetry.lock",
}

// projectMarkers are the manifests whose language servers run project code when
// they load: analyzers, build scripts, proc-macros.
var projectMarkers = []string{
	"Cargo.toml",            // rust-analyzer: build scripts, proc-macros
	"go.mod",                // gopls
	"package.json",          // ts/eslint, npm scripts
	"pyproject.toml",        // python tooling
	"pom.xml",               // jdtls / maven
	"build.gradle",          // jdtls / gradle
	"build.gradle.kts",      // gradle (kotlin dsl)
	"CMakeLists.txt",        // clangd (compile_commands generation)
	"compile_commands.json", // clangd
	"Gemfile",               // ruby
	"composer.json",         // php
}

// ExecutableContentMarkers returns the names of the marker files and
// directories at root whose presence means the workspace can execute code, and
// so needs a trust decision. The scan is shallow, passive and never runs
// anything, as Fresh's is. The order is env files, project manifests, the
// dev-container config, then the .NET project files by name.
//
// A folder with no marker is plain text and documents: nothing to gate, so it
// is trusted silently (see Store.Open).
func ExecutableContentMarkers(root string) []string {
	found := make([]string, 0, 8)
	seen := make(map[string]bool, 8)
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			found = append(found, name)
		}
	}
	// Env files and directories. A virtualenv is a directory, so any entry
	// counts here, as it does in Fresh's marker scan.
	for _, name := range envMarkers {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			add(name)
		}
	}
	// Project manifests: regular files only.
	for _, name := range projectMarkers {
		if info, err := os.Stat(filepath.Join(root, name)); err == nil && info.Mode().IsRegular() {
			add(name)
		}
	}
	// The dev-container config: reopening or building it runs code.
	if isRegularFile(filepath.Join(root, ".devcontainer", "devcontainer.json")) ||
		isRegularFile(filepath.Join(root, ".devcontainer.json")) {
		add("devcontainer.json")
	}
	// C# / .NET: loading a solution or project file runs restore/build and
	// design-time analyzers, so report the files themselves.
	entries, err := os.ReadDir(root)
	if err == nil {
		var dotnet []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			switch strings.ToLower(filepath.Ext(e.Name())) {
			case ".sln", ".csproj", ".fsproj":
				dotnet = append(dotnet, e.Name())
			}
		}
		slices.Sort(dotnet)
		for _, name := range dotnet {
			add(name)
		}
	}
	return found
}

// HasExecutableContent reports whether root holds any executable-content
// marker (see ExecutableContentMarkers).
func HasExecutableContent(root string) bool {
	return len(ExecutableContentMarkers(root)) > 0
}

// isRegularFile reports whether path is an existing regular file.
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
