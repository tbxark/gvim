//go:build unix

package authority

// Direct tests of the realpath helper script, with fake tools that deliberately behave
// like GNU readlink -f (they report a path even when the final component is missing).
// The script must preserve file-name newlines/CR/whitespace and must enforce the local
// EvalSymlinks semantics (a missing final component or broken symlink is an error), so
// the tool difference can never leak a nonexistent path to the caller.

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runRealPathScript(t *testing.T, pathDir, p string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command("sh", "-c", sshRealPathScript, "sh", p)
	cmd.Env = append(os.Environ(), "PATH="+pathDir)
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	err := cmd.Run()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("run helper: %v", err)
		}
		code = exit.ExitCode()
	}
	return out.String(), errBuf.String(), code
}

// fakePathTool installs one shell script named name that prints its last argument and
// exits 0 — the permissive shape of readlink -f on missing final components.
func fakePathTool(t *testing.T, dir, name string) {
	t.Helper()
	body := "#!/bin/sh\nfor last; do :; done\nprintf '%s\\n' \"$last\"\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestRealPathScriptPreservesOddNames(t *testing.T) {
	data := t.TempDir()
	for _, name := range []string{"a\nb", "c\rd", " ", "trailing\n\n"} {
		full := filepath.Join(data, name)
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, tool := range []string{"realpath", "readlink"} {
			bin := t.TempDir()
			fakePathTool(t, bin, tool)
			out, errText, code := runRealPathScript(t, bin, full)
			if code != 0 {
				t.Fatalf("%s %q: code %d, stderr %q", tool, name, code, errText)
			}
			if want := full + "\n"; out != want {
				t.Fatalf("%s %q output = %q, want %q (bytes % x vs % x)", tool, name, out, want, []byte(out), []byte(want))
			}
		}
	}
}

func TestRealPathScriptRejectsMissingFinalComponent(t *testing.T) {
	for _, tool := range []string{"realpath", "readlink"} {
		bin := t.TempDir()
		// Only the chosen fake tool exists: the script takes that branch.
		fakePathTool(t, bin, tool)
		missing := filepath.Join(t.TempDir(), "missing")
		out, errText, code := runRealPathScript(t, bin, missing)
		if code == 0 {
			t.Fatalf("%s reported a missing path as success: stdout %q", tool, out)
		}
		if !strings.Contains(strings.ToLower(errText), "no such file") {
			t.Fatalf("%s missing stderr = %q, want a not-found message", tool, errText)
		}

		// A broken symlink resolves to a non-existent target: also not-found, never the
		// symlink's target path.
		dir := t.TempDir()
		broken := filepath.Join(dir, "broken")
		if err := os.Symlink(filepath.Join(dir, "gone"), broken); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		out, errText, code = runRealPathScript(t, bin, broken)
		if code == 0 {
			t.Fatalf("%s reported a broken symlink as success: stdout %q", tool, out)
		}
		if !strings.Contains(strings.ToLower(errText), "no such file") {
			t.Fatalf("%s broken-link stderr = %q", tool, errText)
		}
	}
}
