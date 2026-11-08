package app

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/tbxark/gvim/internal/daemon"
)

// Daemon host marks (task-100). A daemon writes one small file per workspace root while it
// hosts that workspace; a direct launch reads them to refuse becoming a second writer even
// when the daemon was started with an explicit name (the per-root default socket probe only
// covers the default name). The registry of workspaces is global, so "which workspace does
// this daemon host" cannot be asked of the registry: the daemon records it here instead.
//
// Location: <daemon socket dir>/hosts/<sha1(canonical root)>/<name>.json, one file per
// daemon name (so several named daemons for one root are all visible). Marks are advisory:
// one whose socket is no longer live is stale, ignored, and removed on read. The write is
// atomic (temp file + rename) and 0600; a write failure is a warning, never a start failure.

// hostMark is one daemon's claim on a workspace root.
type hostMark struct {
	Name   string `json:"name"`
	Root   string `json:"root"`
	Socket string `json:"socket"`
	PID    int    `json:"pid"`
}

// hostsDir is the directory of the host marks, created on demand.
func hostsDir() (string, error) {
	dir, err := daemon.Dir()
	if err != nil {
		return "", err
	}
	hosts := filepath.Join(dir, "hosts")
	if err := os.MkdirAll(hosts, 0o700); err != nil {
		return "", err
	}
	return hosts, nil
}

// rootKey is the mark directory of one canonical root: a stable, file-name-safe hash so a
// long or unusual path never becomes a file name.
func rootKey(root string) string {
	sum := sha1.Sum([]byte(root))
	return hex.EncodeToString(sum[:8])
}

// markPath is the file of one daemon's mark for root.
func markPath(root, name string) (string, error) {
	dir, err := hostsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, rootKey(root), name+".json"), nil
}

// writeHostMark records that daemon name hosts root until clearHostMark. It is best
// effort: the caller treats the error as a warning, never as a launch failure.
func writeHostMark(root, name string) error {
	socket, err := daemon.SocketPath(name)
	if err != nil {
		return err
	}
	path, err := markPath(root, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(hostMark{Name: name, Root: root, Socket: socket, PID: os.Getpid()})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// clearHostMark removes daemon name's mark for root and an empty root directory. A failed
// removal is harmless (the next read sees a dead socket and removes it).
func clearHostMark(root, name string) {
	path, err := markPath(root, name)
	if err != nil {
		return
	}
	_ = os.Remove(path)
	_ = os.Remove(filepath.Dir(path))
}

// liveHostsFor returns the marks of live daemons hosting root, sorted by name. Stale marks
// (a socket that is no longer live) are removed on the way. An unreadable marks directory
// is "no daemons", not an error: a direct launch still probes the default socket.
func liveHostsFor(root string) []hostMark {
	dir, err := hostsDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(dir, rootKey(root)))
	if err != nil {
		return nil
	}
	var out []hostMark
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, rootKey(root), entry.Name()))
		if err != nil {
			continue
		}
		var mark hostMark
		if err := json.Unmarshal(raw, &mark); err != nil || mark.Name == "" || mark.Socket == "" || mark.Root != root {
			continue
		}
		if !daemon.IsLive(mark.Socket) {
			_ = os.Remove(filepath.Join(dir, rootKey(root), entry.Name()))
			continue
		}
		out = append(out, mark)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
