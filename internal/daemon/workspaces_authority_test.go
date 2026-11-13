package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegistryAuthoritySpecRoundTrip checks a remote workspace's canonical authority text
// survives a registry reopen, discovery by (root, spec) reuses its identity, a different
// spec is a different workspace, and the root is stored without local IO (it does not have
// to exist on this host).
func TestRegistryAuthoritySpecRoundTrip(t *testing.T) {
	reg := newRegistry(t)
	const spec = "u@example.test:/srv/proj"
	ws, created, err := reg.CreateWithAuthority("/srv/proj", "", spec)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("CreateWithAuthority reported an existing workspace")
	}
	if ws.File.AuthoritySpec != spec {
		t.Fatalf("authority spec = %q, want %q", ws.File.AuthoritySpec, spec)
	}
	if ws.File.Root != "/srv/proj" {
		t.Fatalf("remote root = %q", ws.File.Root)
	}
	if ws.File.Label != "proj" {
		t.Fatalf("label = %q, want the remote basename", ws.File.Label)
	}
	reopened, err := OpenRegistry(reg.Dir())
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := reopened.Get(ws.File.StableID)
	if err != nil || !ok {
		t.Fatalf("reopen Get = %+v, %v, %v", got, ok, err)
	}
	if got.File.AuthoritySpec != spec || got.File.Root != "/srv/proj" {
		t.Fatalf("reopened workspace = %+v", got.File)
	}
	again, err := reopened.DiscoverWithAuthority("/srv/proj/", spec)
	if err != nil {
		t.Fatal(err)
	}
	if again.Foreground.File.StableID != ws.File.StableID {
		t.Fatalf("DiscoverWithAuthority minted a new identity: %q vs %q", again.Foreground.File.StableID, ws.File.StableID)
	}
	other, createdOther, err := reopened.CreateWithAuthority("/srv/proj", "", "u@example.test:/srv/other")
	if err != nil {
		t.Fatal(err)
	}
	if !createdOther || other.File.StableID == ws.File.StableID {
		t.Fatalf("a different authority spec reused the identity: %+v", other.File)
	}
}

// TestRegistryLocalDiscoveryIgnoresRemoteWorkspaces checks a remote workspace whose root
// text equals a local root is never adopted as the local foreground workspace: local
// discovery keys on an empty authority spec, while the remote entry stays dormant.
func TestRegistryLocalDiscoveryIgnoresRemoteWorkspaces(t *testing.T) {
	dir := t.TempDir()
	local, err := CanonicalRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := newRegistry(t)
	remote, _, err := reg.CreateWithAuthority(local, "", "u@h:"+local)
	if err != nil {
		t.Fatal(err)
	}
	disc, err := reg.Discover(local)
	if err != nil {
		t.Fatal(err)
	}
	if disc.Foreground.File.AuthoritySpec != "" {
		t.Fatalf("local discovery adopted a remote workspace: %+v", disc.Foreground.File)
	}
	if disc.Foreground.File.StableID == remote.File.StableID {
		t.Fatal("local foreground reused the remote workspace identity")
	}
	found := false
	for _, w := range disc.Dormant {
		if w.File.StableID == remote.File.StableID && w.File.AuthoritySpec == "u@h:"+local {
			found = true
		}
	}
	if !found {
		t.Fatalf("the remote workspace is not dormant: %+v", disc.Dormant)
	}
}

// TestRegistryAuthoritySpecVersionTolerance checks old/foreign files: a v2 file without the
// field loads as local (empty spec), and a file with a control character in the spec is
// refused, not silently accepted.
func TestRegistryAuthoritySpecVersionTolerance(t *testing.T) {
	reg := newRegistry(t)
	plain := WorkspaceFile{
		Version: WorkspaceVersion, StableID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Label: "old", Root: filepath.Join(t.TempDir(), "root"), LastFocusedAt: 1,
	}
	if err := os.MkdirAll(plain.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	workspaceFileAt(t, reg, plain)
	list, problems := reg.List()
	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
	seen := map[string]string{}
	for _, w := range list {
		seen[w.File.StableID] = w.File.AuthoritySpec
	}
	if spec, ok := seen[plain.StableID]; !ok || spec != "" {
		t.Fatalf("old v2 workspace = %v, want empty spec", seen)
	}
	bad := WorkspaceFile{
		Version: WorkspaceVersion, StableID: "cccccccccccccccccccccccccccccccc",
		Label: "bad", Root: "/srv/x", LastFocusedAt: 3, AuthoritySpec: "u@h:/x\nnext",
	}
	path := reg.path(bad.Root, bad.StableID)
	if err := os.WriteFile(path, mustJSON(t, bad), 0o600); err != nil {
		t.Fatal(err)
	}
	_, problems = reg.List()
	var found bool
	for _, p := range problems {
		if strings.Contains(p.Error(), "control character") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a spec with a control character was not reported: %v", problems)
	}
}
