package trust

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SpawnRefusal is the spawn gate's denial: why a launch may not run in a
// workspace at Level. Path is the executable the gate refused ("" when every
// process is refused, as at Blocked, or when the launch names no executable).
type SpawnRefusal struct {
	Level Level
	Root  string
	Path  string
}

// ErrSpawnDenied lets callers test a refusal with errors.Is.
var ErrSpawnDenied = errors.New("spawn denied by workspace trust")

func (e *SpawnRefusal) Unwrap() error { return ErrSpawnDenied }

// Error is the status-row text of a refused spawn: the level, the executable (if
// the refusal names one), and the way out.
func (e *SpawnRefusal) Error() string {
	if e.Level == Blocked {
		return "workspace trust: the workspace is Blocked, so no processes may run; raise its trust level to run the terminal or tooling"
	}
	if e.Path != "" {
		return fmt.Sprintf("workspace trust: the workspace is Restricted, so it cannot run %q (a program inside the project); trust the folder to allow project tooling", e.Path)
	}
	return "workspace trust: the workspace is Restricted, so this command may not run; trust the folder to allow project tooling"
}

// shellCommands are the interpreters whose -c argument string the gate inspects
// for a project-relative executable (the gvim shell/formatter runner is one
// "sh -c").
var shellCommands = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true,
	"cmd": true, "cmd.exe": true, "powershell": true, "powershell.exe": true,
	"pwsh": true, "pwsh.exe": true,
}

// DecideSpawn is the workspace trust spawn gate: it decides for one launch of
// argv in cwd whether the workspace at level may run it. pathValue is the
// child's effective PATH (after the project environment overlay and any
// per-launch environment); it is required to reason about bare command names.
//
//   - Trusted allows everything.
//   - Blocked denies every spawn, the integrated terminal included.
//   - Restricted (and Undecided, whose effective level is Restricted) denies a
//     spawn whose executable resolves inside the workspace root. An explicit
//     path (relative to cwd, or absolute) is checked lexically and through
//     symlinks. A bare name is resolved through pathValue in order, with the
//     shell rules: an empty PATH entry is cwd, a relative entry is relative to
//     cwd; the first existing executable decides, so a project directory on
//     PATH is caught. A name that resolves nowhere is allowed (the exec then
//     fails; no project code ran). An empty argv (the terminal's shell probe,
//     where the caller supplies the shell separately) is allowed while
//     Restricted.
//
// For a shell interpreter ("sh -c ...") the shell itself is checked like any
// other command (a PATH-resolved shell may be a project one), and the first
// token of the -c string is checked the same way: a project-relative or
// project-resolved first token ("./.venv/bin/python -m black") is refused.
// Compound, quoted or variable-built commands beyond the first token are not
// parsed; that is the documented residual, not a containment guarantee.
func DecideSpawn(level Level, root, cwd string, argv []string, pathValue string) error {
	switch Effective(level) {
	case Trusted:
		return nil
	case Blocked:
		return &SpawnRefusal{Level: Blocked, Root: root}
	}
	if root == "" {
		return nil
	}
	exec := ""
	if len(argv) > 0 {
		exec = strings.TrimSpace(argv[0])
	}
	if exec == "" {
		return nil
	}
	if name := deniedExecutable(exec, root, cwd, pathValue); name != "" {
		return &SpawnRefusal{Level: Restricted, Root: root, Path: name}
	}
	if token := shellFirstToken(exec, argv); token != "" {
		if name := deniedExecutable(token, root, cwd, pathValue); name != "" {
			return &SpawnRefusal{Level: Restricted, Root: root, Path: name}
		}
	}
	return nil
}

// deniedExecutable returns the spelling to show when name must not run, or "".
// An explicit path is checked directly; a bare name is resolved through PATH
// first, so "the program the user would actually run" is what is judged.
func deniedExecutable(name, root, cwd, pathValue string) string {
	if !strings.ContainsAny(name, "/\\") {
		resolved, ok := ResolveSpawnPath(cwd, name, pathValue)
		if !ok {
			return ""
		}
		if deniedAbsolute(resolved, root) {
			return resolved
		}
		return ""
	}
	abs := name
	if !filepath.IsAbs(abs) {
		base := cwd
		if base == "" {
			base = root
		}
		abs = filepath.Join(base, abs)
	}
	if deniedAbsolute(filepath.Clean(abs), root) {
		return name
	}
	return ""
}

// ResolveSpawnPath resolves a bare command name through pathValue the way the
// shell and exec would: entries in order, an empty entry is cwd, a relative
// entry is relative to cwd. The first existing executable wins. It does not
// implement Windows PATHEXT (a documented residual): on Windows an exact file
// name is required.
func ResolveSpawnPath(cwd, name, pathValue string) (string, bool) {
	if name == "" || strings.ContainsAny(name, "/\\") {
		return "", false
	}
	base := cwd
	if base == "" {
		base = "."
	}
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			dir = base
		} else if !filepath.IsAbs(dir) {
			dir = filepath.Join(base, dir)
		}
		candidate := filepath.Join(dir, name)
		if isExecutable(candidate) {
			return candidate, true
		}
	}
	return "", false
}

// isExecutable reports whether path is a file the OS would execute. A symlink
// is followed (os.Stat); on Unix the executable bits are required, on Windows
// the file's existence is enough (the name must already carry the extension).
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

// deniedAbsolute reports whether the absolute path is inside root, lexically or
// through symlinks: a symlinked workspace or a symlinked executable cannot dodge
// the check.
func deniedAbsolute(abs, root string) bool {
	root = filepath.Clean(root)
	canonRoot := root
	if c, err := filepath.EvalSymlinks(root); err == nil {
		canonRoot = c
	}
	if within(root, abs) || within(canonRoot, abs) {
		return true
	}
	if canon, err := filepath.EvalSymlinks(abs); err == nil {
		if within(root, canon) || within(canonRoot, canon) {
			return true
		}
	}
	return false
}

// within reports whether path is root or below it. Both are cleaned absolute
// paths; a lexical comparison is enough for the gate because the canonical
// forms are checked too.
func within(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// shellFirstToken returns the first token of a shell interpreter's -c string,
// or "". It is deliberately shallow (first token only, one level of quoting):
// the gate looks for the common "shell runner with a path command" case, not a
// shell parser.
func shellFirstToken(exec string, argv []string) string {
	if !shellCommands[strings.ToLower(filepath.Base(exec))] || len(argv) < 3 {
		return ""
	}
	switch argv[1] {
	case "-c", "/c", "-Command":
	default:
		return ""
	}
	return firstCommandToken(argv[2])
}

func firstCommandToken(command string) string {
	s := strings.TrimSpace(command)
	if s == "" {
		return ""
	}
	if c := s[0]; c == '\'' || c == '"' {
		if i := strings.IndexByte(s[1:], c); i >= 0 {
			s = s[1 : 1+i]
		}
	} else if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	// An environment-assignment prefix (PATH=x cmd) is not a program path.
	if strings.Contains(s, "=") && !strings.ContainsAny(s, "/\\") {
		return ""
	}
	return s
}
