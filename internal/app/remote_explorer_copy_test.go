package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	expanel "github.com/tbxark/gvim/internal/tui/explorer"
)

// copyTestDecoy creates a local decoy tree and returns a snapshot of it.
func copyTestDecoy(t *testing.T) (string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local.txt"), []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "x.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, copySnapshot(t, dir)
}

// copySnapshot reads every regular file of dir into a map.
func copySnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(dir, p)
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameSnapshot(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestRemoteCopyFileAndDirectory checks a binary file copy and a recursive directory
// copy go through the authority with atomic per-file writes, and a local decoy tree is
// untouched (no local fallback).
func TestRemoteCopyFileAndDirectory(t *testing.T) {
	decoy, before := copyTestDecoy(t)
	fake := newRemoteOpsFake()
	binary := "a\x00b\xff"
	fake.put("/remote/w/a.bin", binary)
	fake.put("/remote/w/dst/", "")
	fake.put("/remote/w/src/", "")
	fake.put("/remote/w/src/x.txt", "x")
	fake.put("/remote/w/src/sub/", "")
	fake.put("/remote/w/src/sub/y.txt", "y")
	ops, _ := fake.ops(true)

	p, err := ops.Copy(context.Background(), "/remote/w/a.bin", "/remote/w/dst", expanel.ConflictCancel)
	if err != nil || p != "/remote/w/dst/a.bin" {
		t.Fatalf("copy file = %q, %v", p, err)
	}
	if got := string(fake.files[p]); got != binary {
		t.Fatalf("binary content = %q", got)
	}
	dir, err := ops.Copy(context.Background(), "/remote/w/src", "/remote/w/dst", expanel.ConflictCancel)
	if err != nil || dir != "/remote/w/dst/src" {
		t.Fatalf("copy dir = %q, %v", dir, err)
	}
	if got := string(fake.files["/remote/w/dst/src/x.txt"]); got != "x" {
		t.Fatalf("dir file content = %q", got)
	}
	if got := string(fake.files["/remote/w/dst/src/sub/y.txt"]); got != "y" {
		t.Fatalf("nested file content = %q", got)
	}
	if len(fake.writeOpts) == 0 {
		t.Fatal("no write options recorded")
	}
	for _, o := range fake.writeOpts {
		if !o.Atomic {
			t.Fatalf("a remote copy wrote a file non-atomically: %+v", o)
		}
	}
	after := copySnapshot(t, decoy)
	if !sameSnapshot(before, after) {
		t.Fatalf("the local decoy changed: before=%v after=%v", before, after)
	}
}

// TestRemoteMoveRemovesSourceOnlyAfterCopy checks cut semantics: the destination is
// complete before the source is removed, a failed copy keeps the source, and a failed
// source removal keeps both and reports it.
func TestRemoteMoveRemovesSourceOnlyAfterCopy(t *testing.T) {
	fake := newRemoteOpsFake().put("/remote/w/a.txt", "cut me").put("/remote/w/dst/", "")
	ops, _ := fake.ops(true)
	dst, err := ops.Move(context.Background(), "/remote/w/a.txt", "/remote/w/dst")
	if err != nil || dst != "/remote/w/dst/a.txt" {
		t.Fatalf("Move = %q, %v", dst, err)
	}
	if _, ok := fake.files["/remote/w/a.txt"]; ok {
		t.Fatal("the cut source survived a successful copy")
	}
	if got := string(fake.files[dst]); got != "cut me" {
		t.Fatalf("moved content = %q", got)
	}
	if len(fake.removes) != 1 || fake.removes[0] != "/remote/w/a.txt" {
		t.Fatalf("removes = %v", fake.removes)
	}

	// A source-removal failure keeps both copies and says so.
	fake2 := newRemoteOpsFake().put("/remote/w/a.txt", "keep").put("/remote/w/dst/", "")
	fake2.removeErr = errors.New("remove denied by test")
	ops2, _ := fake2.ops(true)
	_, err = ops2.Move(context.Background(), "/remote/w/a.txt", "/remote/w/dst")
	if err == nil || !strings.Contains(err.Error(), "removing the source failed") || !strings.Contains(err.Error(), "the copy is kept") {
		t.Fatalf("source-removal error = %v", err)
	}
	if _, ok := fake2.files["/remote/w/a.txt"]; !ok {
		t.Fatal("a failed source removal lost the source")
	}
	if got := string(fake2.files["/remote/w/dst/a.txt"]); got != "keep" {
		t.Fatalf("the copy was not kept: %q", got)
	}

	// A copy failure leaves the source alone and creates no destination file.
	fake3 := newRemoteOpsFake().put("/remote/w/a.txt", "source").put("/remote/w/dst/", "")
	fake3.writeErr = errors.New("write denied by test")
	ops3, _ := fake3.ops(true)
	if _, err := ops3.Move(context.Background(), "/remote/w/a.txt", "/remote/w/dst"); err == nil {
		t.Fatal("a failed copy was accepted as a move")
	}
	if _, ok := fake3.files["/remote/w/a.txt"]; !ok {
		t.Fatal("a failed copy lost the source")
	}
	if len(fake3.removes) != 0 {
		t.Fatalf("a failed copy removed the source: %v", fake3.removes)
	}
}

// TestRemotePasteConflictPolicies checks a paste into another directory with an existing
// name follows the explorer's conflict choice: cancel reports explorer.ErrConflict (so the
// root can offer overwrite/rename), skip leaves the destination and reports its path,
// overwrite removes it and replaces it with the copy.
func TestRemotePasteConflictPolicies(t *testing.T) {
	mk := func() (*remoteOpsFake, *remoteExplorerOps) {
		fake := newRemoteOpsFake().
			put("/remote/w/other/a.go", "new").
			put("/remote/w/dst/a.go", "old").
			put("/remote/w/dst/", "")
		ops, _ := fake.ops(true)
		return fake, ops
	}

	fake, ops := mk()
	if _, err := ops.Copy(context.Background(), "/remote/w/other/a.go", "/remote/w/dst", expanel.ConflictCancel); !errors.Is(err, expanel.ErrConflict) {
		t.Fatalf("cancel error = %v, want explorer.ErrConflict", err)
	}
	if got := string(fake.files["/remote/w/dst/a.go"]); got != "old" || len(fake.writes) != 0 || len(fake.removes) != 0 {
		t.Fatalf("cancel changed the destination: content %q writes %v removes %v", got, fake.writes, fake.removes)
	}

	fake, ops = mk()
	out, err := ops.Copy(context.Background(), "/remote/w/other/a.go", "/remote/w/dst", expanel.ConflictSkip)
	if err != nil || out != "/remote/w/dst/a.go" {
		t.Fatalf("skip = %q, %v; want the existing destination path", out, err)
	}
	if got := string(fake.files["/remote/w/dst/a.go"]); got != "old" || len(fake.writes) != 0 {
		t.Fatalf("skip changed the destination: content %q writes %v", got, fake.writes)
	}

	fake, ops = mk()
	out, err = ops.Copy(context.Background(), "/remote/w/other/a.go", "/remote/w/dst", expanel.ConflictOverwrite)
	if err != nil || out != "/remote/w/dst/a.go" {
		t.Fatalf("overwrite = %q, %v", out, err)
	}
	if got := string(fake.files["/remote/w/dst/a.go"]); got != "new" {
		t.Fatalf("overwrite content = %q, want the copy", got)
	}
}

// TestRemotePasteConflictOverwriteDirectory checks overwrite replaces an existing
// destination directory (the old contents are removed before the copy; no rollback).
func TestRemotePasteConflictOverwriteDirectory(t *testing.T) {
	fake := newRemoteOpsFake().put("/remote/w/other/sub/", "").put("/remote/w/other/sub/new.txt", "new")
	fake.put("/remote/w/dst/", "").put("/remote/w/dst/sub/", "").put("/remote/w/dst/sub/stale.txt", "stale")
	ops, _ := fake.ops(true)
	out, err := ops.Copy(context.Background(), "/remote/w/other/sub", "/remote/w/dst", expanel.ConflictOverwrite)
	if err != nil || out != "/remote/w/dst/sub" {
		t.Fatalf("overwrite directory = %q, %v", out, err)
	}
	if _, ok := fake.files["/remote/w/dst/sub/stale.txt"]; ok {
		t.Fatal("the stale destination file survived the overwrite")
	}
	if got := string(fake.files["/remote/w/dst/sub/new.txt"]); got != "new" {
		t.Fatalf("copied content = %q", got)
	}
}

// TestRemoteCopyAsRename checks the conflict prompt's rename choice copies under the given
// name (and refuses a name that is now taken, so the prompt can run again).
func TestRemoteCopyAsRename(t *testing.T) {
	fake := newRemoteOpsFake().
		put("/remote/w/other/a.go", "new").
		put("/remote/w/dst/a.go", "old").
		put("/remote/w/dst/", "")
	ops, _ := fake.ops(true)
	out, err := ops.CopyAs(context.Background(), "/remote/w/other/a.go", "/remote/w/dst", "a-renamed.go", expanel.ConflictCancel)
	if err != nil || out != "/remote/w/dst/a-renamed.go" {
		t.Fatalf("CopyAs = %q, %v", out, err)
	}
	if got := string(fake.files["/remote/w/dst/a-renamed.go"]); got != "new" {
		t.Fatalf("renamed copy content = %q", got)
	}
	if got := string(fake.files["/remote/w/dst/a.go"]); got != "old" {
		t.Fatalf("the original conflict destination changed: %q", got)
	}
	if _, err := ops.CopyAs(context.Background(), "/remote/w/other/a.go", "/remote/w/dst", "a.go", expanel.ConflictCancel); !errors.Is(err, expanel.ErrConflict) {
		t.Fatalf("CopyAs onto a taken name = %v, want explorer.ErrConflict", err)
	}
}

// TestRemoteCutPasteConflictPolicies checks a cut paste follows the conflict choice too:
// cancel reports the conflict with both entries intact, skip cancels the cut (the source is
// returned, the destination untouched), overwrite replaces the destination and removes the
// source only after the copy succeeds.
func TestRemoteCutPasteConflictPolicies(t *testing.T) {
	mk := func() (*remoteOpsFake, *remoteExplorerOps) {
		fake := newRemoteOpsFake().
			put("/remote/w/other/a.go", "new").
			put("/remote/w/dst/a.go", "old").
			put("/remote/w/dst/", "")
		ops, _ := fake.ops(true)
		return fake, ops
	}
	// Move is the plain cancel-policy cut paste.
	fake, ops := mk()
	if _, err := ops.Move(context.Background(), "/remote/w/other/a.go", "/remote/w/dst"); !errors.Is(err, expanel.ErrConflict) {
		t.Fatalf("cut cancel = %v, want explorer.ErrConflict", err)
	}
	if got := string(fake.files["/remote/w/other/a.go"]); got != "new" {
		t.Fatalf("source changed on a cancelled cut: %q", got)
	}
	if got := string(fake.files["/remote/w/dst/a.go"]); got != "old" || len(fake.removes) != 0 {
		t.Fatalf("destination/source changed on a cancelled cut: %q removes %v", got, fake.removes)
	}
	fake, ops = mk()
	out, err := ops.MoveAs(context.Background(), "/remote/w/other/a.go", "/remote/w/dst", "", expanel.ConflictSkip)
	if err != nil || out != "/remote/w/other/a.go" {
		t.Fatalf("cut skip = %q, %v; want the source back", out, err)
	}
	if _, ok := fake.files["/remote/w/other/a.go"]; !ok {
		t.Fatal("cut skip removed the source")
	}
	if got := string(fake.files["/remote/w/dst/a.go"]); got != "old" {
		t.Fatalf("cut skip changed the destination: %q", got)
	}
	fake, ops = mk()
	out, err = ops.MoveAs(context.Background(), "/remote/w/other/a.go", "/remote/w/dst", "a-moved.go", expanel.ConflictOverwrite)
	if err != nil || out != "/remote/w/dst/a-moved.go" {
		t.Fatalf("cut overwrite/rename = %q, %v", out, err)
	}
	if got := string(fake.files["/remote/w/dst/a-moved.go"]); got != "new" {
		t.Fatalf("moved content = %q", got)
	}
	if _, ok := fake.files["/remote/w/other/a.go"]; ok {
		t.Fatal("the cut source survived the move")
	}
}

// TestRemoteDuplicateNaming checks Fresh's naming rule for a same-directory copy
// (Duplicate): "name copy.ext", "name copy 2.ext", and the dotfile/extension rules.
func TestRemoteDuplicateNaming(t *testing.T) {
	fake := newRemoteOpsFake().
		put("/remote/w/a.go", "a").
		put("/remote/w/README", "r").
		put("/remote/w/.env", "e")
	ops, _ := fake.ops(true)
	got1, err := ops.Copy(context.Background(), "/remote/w/a.go", "/remote/w", expanel.ConflictCancel)
	if err != nil || got1 != "/remote/w/a copy.go" {
		t.Fatalf("first duplicate = %q, %v", got1, err)
	}
	got2, err := ops.Copy(context.Background(), "/remote/w/a.go", "/remote/w", expanel.ConflictCancel)
	if err != nil || got2 != "/remote/w/a copy 2.go" {
		t.Fatalf("second duplicate = %q, %v", got2, err)
	}
	noExt, err := ops.Copy(context.Background(), "/remote/w/README", "/remote/w", expanel.ConflictCancel)
	if err != nil || noExt != "/remote/w/README copy" {
		t.Fatalf("extensionless duplicate = %q, %v", noExt, err)
	}
	dot, err := ops.Copy(context.Background(), "/remote/w/.env", "/remote/w", expanel.ConflictCancel)
	if err != nil || dot != "/remote/w/.env copy" {
		t.Fatalf("dotfile duplicate = %q, %v", dot, err)
	}
}

// TestRemoteCopyRefusals checks the unsafe destinations: inside the source, outside the
// workspace, the workspace root, and a symbolic link anywhere in the subtree.
func TestRemoteCopyRefusals(t *testing.T) {
	fake := newRemoteOpsFake().
		put("/remote/w/tree/", "").
		put("/remote/w/tree/x.txt", "x").
		put("/remote/w/tree/child/", "").
		put("/remote/w/other/", "").
		put("/remote/w/link-target.txt", "t").
		put("/remote/w/linked/", "").
		put("/remote/w/linked/file.txt", "f")
	fake.putSymlink("/remote/w/link", "/remote/w/link-target.txt")
	fake.putSymlink("/remote/w/linked/inside-link", "/remote/w/link-target.txt")
	ops, _ := fake.ops(true)
	ctx := context.Background()

	if _, err := ops.Copy(ctx, "/remote/w/tree", "/remote/w/tree/child", expanel.ConflictCancel); err == nil || !strings.Contains(err.Error(), "inside the source") {
		t.Fatalf("inside-itself copy = %v", err)
	}
	if _, err := ops.Move(ctx, "/remote/w/tree", "/remote/w/tree/child"); err == nil || !strings.Contains(err.Error(), "inside the source") {
		t.Fatalf("inside-itself move = %v", err)
	}
	if _, err := ops.Copy(ctx, "/other/a", "/remote/w", expanel.ConflictCancel); err == nil || !strings.Contains(err.Error(), "outside the remote workspace") {
		t.Fatalf("outside source copy = %v", err)
	}
	if _, err := ops.Copy(ctx, "/remote/w/tree", "/other", expanel.ConflictCancel); err == nil || !strings.Contains(err.Error(), "outside the remote workspace") {
		t.Fatalf("outside dest copy = %v", err)
	}
	if _, err := ops.Copy(ctx, "/remote/w", "/remote/w/other", expanel.ConflictCancel); err == nil || !strings.Contains(err.Error(), "workspace root") {
		t.Fatalf("root copy = %v", err)
	}
	if _, err := ops.Copy(ctx, "/remote/w/link", "/remote/w/other", expanel.ConflictCancel); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink source copy = %v", err)
	}
	if _, err := ops.Copy(ctx, "/remote/w/linked", "/remote/w/other", expanel.ConflictCancel); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink-in-tree copy = %v", err)
	}
	if len(fake.writes) != 0 {
		t.Fatalf("a refused copy wrote files: %v", fake.writes)
	}
}

// TestRemoteCopyBounds checks the entry/byte/depth bounds are refused before the
// destination is created.
func TestRemoteCopyBounds(t *testing.T) {
	fake := newRemoteOpsFake().
		put("/remote/w/src/", "").
		put("/remote/w/src/a.txt", "12345").
		put("/remote/w/src/b.txt", "b").
		put("/remote/w/deep/", "").
		put("/remote/w/deep/one/", "").
		put("/remote/w/deep/one/two/", "").
		put("/remote/w/deep/one/two/x.txt", "x")
	ops, _ := fake.ops(true)
	ctx := context.Background()
	st, err := fake.Stat(ctx, "/remote/w/src")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.planCopy(ctx, "/remote/w/src", st, remoteCopyLimits{MaxEntries: 1}); err == nil || !strings.Contains(err.Error(), "more than 1 entries") {
		t.Fatalf("entries bound = %v", err)
	}
	if _, err := ops.planCopy(ctx, "/remote/w/src", st, remoteCopyLimits{MaxBytes: 4}); err == nil || !strings.Contains(err.Error(), "4-byte copy bound") {
		t.Fatalf("bytes bound = %v", err)
	}
	deep, err := fake.Stat(ctx, "/remote/w/deep")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.planCopy(ctx, "/remote/w/deep", deep, remoteCopyLimits{MaxDepth: 1}); err == nil || !strings.Contains(err.Error(), "deeper than 1 directories") {
		t.Fatalf("depth bound = %v", err)
	}
	if len(fake.writes) != 0 {
		t.Fatalf("a bounded plan wrote files: %v", fake.writes)
	}
}

// TestRemoteCopyCancellationKeepsPartialDestination checks a cancellation in the write
// phase keeps the files written so far and reports the partial destination.
func TestRemoteCopyCancellationKeepsPartialDestination(t *testing.T) {
	fake := newRemoteOpsFake().
		put("/remote/w/src/", "").
		put("/remote/w/src/a.txt", "a").
		put("/remote/w/src/b.txt", "b").
		put("/remote/w/src/c.txt", "c").
		put("/remote/w/dst/", "")
	ops, _ := fake.ops(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake.onWrite = func() {
		if len(fake.writes) == 1 {
			cancel()
		}
	}
	_, err := ops.Copy(ctx, "/remote/w/src", "/remote/w/dst", expanel.ConflictCancel)
	if err == nil || !strings.Contains(err.Error(), "partial destination is kept") {
		t.Fatalf("cancelled copy error = %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the cancellation was not preserved: %v", err)
	}
	if _, ok := fake.files["/remote/w/dst/src/a.txt"]; !ok {
		t.Fatal("the file written before the cancellation is missing")
	}
	if _, ok := fake.files["/remote/w/dst/src/c.txt"]; ok {
		t.Fatal("a file after the cancellation point was written")
	}
}

// TestRemoteMoveCancellationKeepsSourceAndPartialDestination checks a cut that is
// cancelled in the write phase: the source tree is not removed (source removal only runs
// after a complete copy), the files already copied stay as a reported partial destination,
// and the cancellation is preserved.
func TestRemoteMoveCancellationKeepsSourceAndPartialDestination(t *testing.T) {
	fake := newRemoteOpsFake().
		put("/remote/w/src/", "").
		put("/remote/w/src/a.txt", "a").
		put("/remote/w/src/b.txt", "b").
		put("/remote/w/src/c.txt", "c").
		put("/remote/w/dst/", "")
	ops, _ := fake.ops(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake.onWrite = func() {
		if len(fake.writes) == 1 {
			cancel()
		}
	}
	_, err := ops.Move(ctx, "/remote/w/src", "/remote/w/dst")
	if err == nil || !strings.Contains(err.Error(), "partial destination is kept") {
		t.Fatalf("cancelled move error = %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the cancellation was not preserved: %v", err)
	}
	if _, ok := fake.files["/remote/w/src/a.txt"]; !ok {
		t.Fatal("a cancelled move removed a source file")
	}
	if _, ok := fake.files["/remote/w/src/c.txt"]; !ok {
		t.Fatal("a cancelled move removed an uncopied source file")
	}
	if len(fake.removes) != 0 {
		t.Fatalf("a cancelled move removed the source: %v", fake.removes)
	}
	if _, ok := fake.files["/remote/w/dst/src/a.txt"]; !ok {
		t.Fatal("the partial destination written before the cancellation is missing")
	}
}

// TestRemoteCopyRefusedWhenDisconnected checks every clipboard operation is refused
// while the authority is down, touching nothing.
func TestRemoteCopyRefusedWhenDisconnected(t *testing.T) {
	fake := newRemoteOpsFake().put("/remote/w/a.txt", "a").put("/remote/w/dst/", "")
	ops, _ := fake.ops(false)
	if _, err := ops.Copy(context.Background(), "/remote/w/a.txt", "/remote/w/dst", expanel.ConflictCancel); !errors.Is(err, errRemoteExplorerDisconnected) {
		t.Fatalf("copy = %v", err)
	}
	if _, err := ops.Move(context.Background(), "/remote/w/a.txt", "/remote/w/dst"); !errors.Is(err, errRemoteExplorerDisconnected) {
		t.Fatalf("move = %v", err)
	}
	if len(fake.writes) != 0 || len(fake.mkdirs) != 0 || len(fake.removes) != 0 {
		t.Fatalf("a disconnected copy touched the remote: %+v", fake)
	}
}

// TestRemoteCopyTrustGate checks the trust gate runs before a copy or move and nothing
// is written when it refuses.
func TestRemoteCopyTrustGate(t *testing.T) {
	fake := newRemoteOpsFake().put("/remote/w/a.txt", "a").put("/remote/w/dst/", "")
	ops, _ := fake.ops(true)
	gateErr := errors.New("workspace is not trusted")
	calls := 0
	ops.gate = func(context.Context, []string, string) error { calls++; return gateErr }
	if _, err := ops.Copy(context.Background(), "/remote/w/a.txt", "/remote/w/dst", expanel.ConflictCancel); !errors.Is(err, gateErr) {
		t.Fatalf("copy gate = %v", err)
	}
	if _, err := ops.Move(context.Background(), "/remote/w/a.txt", "/remote/w/dst"); !errors.Is(err, gateErr) {
		t.Fatalf("move gate = %v", err)
	}
	if calls != 2 || len(fake.writes) != 0 || len(fake.removes) != 0 {
		t.Fatalf("gate calls = %d writes = %v removes = %v", calls, fake.writes, fake.removes)
	}
}
