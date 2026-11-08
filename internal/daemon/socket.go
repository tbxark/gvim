package daemon

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dir returns the daemon socket directory: <user cache>/gvim/daemon. It creates
// the directory with 0700.
func Dir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "gvim", "daemon")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// DefaultName is the per-root daemon name: a slug of the root's basename plus a
// short hash of the absolute root, so two checkouts with the same directory name
// do not collide.
func DefaultName(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	base := strings.ToLower(filepath.Base(abs))
	var slug strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			slug.WriteRune(r)
		} else {
			slug.WriteByte('-')
		}
	}
	name := strings.Trim(slug.String(), "-")
	if name == "" {
		name = "root"
	}
	if len(name) > 32 {
		name = name[:32]
	}
	sum := sha1.Sum([]byte(abs))
	return name + "-" + hex.EncodeToString(sum[:4])
}

// SocketPath is the socket path of a named daemon. A Unix socket address is limited
// to about 104 bytes on several platforms, so a path that would not fit is refused
// with a message that names the shorter --name workaround.
func SocketPath(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", errors.New("daemon: empty name")
	}
	path := filepath.Join(dir, name+".sock")
	if len(path) > 100 {
		return "", fmt.Errorf("daemon: socket path %q is too long for a Unix socket; use a shorter --name", path)
	}
	return path, nil
}

// probeDialTimeout bounds the liveness probe of an existing socket.
const probeDialTimeout = 300 * time.Millisecond

// IsLive reports whether a daemon is listening at path.
func IsLive(path string) bool {
	conn, err := net.DialTimeout("unix", path, probeDialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Listen binds the daemon socket for name. An existing socket is probed: a live
// daemon yields an error ("another daemon is running"), a stale file is unlinked.
// The socket file is set to 0600. The caller closes the returned listener.
func Listen(name string) (net.Listener, string, error) {
	path, err := SocketPath(name)
	if err != nil {
		return nil, "", err
	}
	if _, err := os.Lstat(path); err == nil {
		if IsLive(path) {
			return nil, path, fmt.Errorf("daemon: another daemon is running at %s", path)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, path, fmt.Errorf("daemon: remove stale socket: %w", err)
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, path, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return nil, path, fmt.Errorf("daemon: chmod socket: %w", err)
	}
	return ln, path, nil
}

// Attach dials the daemon socket for name.
func Attach(name string) (net.Conn, error) {
	path, err := SocketPath(name)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", path, probeDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("daemon: no daemon for %q (start it with gvim --daemon in that workspace): %w", name, err)
	}
	return conn, nil
}
