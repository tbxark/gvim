package authority

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
)

// RemoteSpec is one parsed remote target: Fresh's scp-style or ssh:// form.
type RemoteSpec struct {
	User string
	Host string
	Port int // 0 means the default SSH port
	Path string
	Line int // 0: not given; 1-based
	Col  int // 0: not given; 1-based
	Raw  string
	// UserExplicit is true when the target spelled a user (user@host), so an
	// ssh_config User must not override it. A URL without a user leaves it false:
	// the environment default is a fallback, not an override.
	UserExplicit bool
	// PortExplicit is true when the target spelled the port (ssh://host:port), so an
	// ssh_config Port must not override it.
	PortExplicit bool
}

// Display is the user@host form the status row shows (the port is included when it is not
// the default).
func (s RemoteSpec) Display() string {
	host := s.Host
	if s.Port != 0 && s.Port != DefaultSSHPort {
		host = fmt.Sprintf("%s:%d", s.Host, s.Port)
	}
	if s.User == "" {
		return host
	}
	return s.User + "@" + host
}

// Workspace is the remote directory the workspace is rooted at: the target path when it is
// a directory, else its parent. The target decides only through its spelling (no IO here).
func (s RemoteSpec) Workspace() string {
	p := s.Path
	if p == "" || p == "/" || p == "~" {
		return p
	}
	if strings.HasSuffix(p, "/") {
		return strings.TrimSuffix(p, "/") + "/"
	}
	if i := strings.LastIndex(p, "/"); i >= 0 {
		if i == 0 {
			return "/"
		}
		return p[:i]
	}
	return "."
}

// DefaultSSHPort is the port a spec without one uses.
const DefaultSSHPort = 22

// ErrNotRemote is returned by ParseTarget when the argument is not a remote form.
var ErrNotRemote = errors.New("not a remote target")

// IsSSHURL reports whether arg claims the ssh:// scheme (malformed URLs included: the
// scheme is exclusive and never falls back to a local path).
func IsSSHURL(arg string) bool {
	return strings.HasPrefix(arg, "ssh://")
}

// LooksRemote reports whether arg parses as one of the remote forms. It never errors; a
// malformed ssh:// URL is still a remote claim.
func LooksRemote(arg string) bool {
	if IsSSHURL(arg) {
		return true
	}
	_, ok := scpCandidate(arg)
	return ok
}

// ParseTarget parses arg as a remote target. The second result reports whether arg is
// remote at all; for a claimed ssh:// URL an error means startup must fail, never fall back
// to a local file.
func ParseTarget(arg string) (RemoteSpec, bool, error) {
	if IsSSHURL(arg) {
		s, err := parseSSHURL(arg)
		return s, true, err
	}
	if !LooksRemote(arg) {
		return RemoteSpec{}, false, nil
	}
	s, err := parseSCP(arg)
	return s, true, err
}

// ParseRemote parses a remote target and errors when arg is not one.
func ParseRemote(arg string) (RemoteSpec, error) {
	s, ok, err := ParseTarget(arg)
	if err != nil {
		return RemoteSpec{}, err
	}
	if !ok {
		return RemoteSpec{}, ErrNotRemote
	}
	return s, nil
}

// scpCandidate reports whether arg has the scp shape [user@]host:path. The checks keep
// ordinary local files (main.go:42, C:\x, ./a:b) out of the remote path.
func scpCandidate(arg string) (int, bool) {
	i := strings.Index(arg, ":")
	if i <= 0 {
		return 0, false
	}
	head, rest := arg[:i], arg[i+1:]
	if rest == "" {
		// A trailing colon after a user is a malformed remote target, not a local file.
		return i, strings.Contains(head, "@")
	}
	if strings.ContainsAny(head, "/"+string('\\')) {
		return 0, false
	}
	// A windows drive letter and its separator are never a host.
	if len(head) == 1 && (strings.HasPrefix(rest, string('\\')) || strings.HasPrefix(rest, "/")) {
		return 0, false
	}
	at := strings.Contains(head, "@")
	// A tail that is exactly a position suffix is the local FILE:LINE[:COL] form,
	// unless the @ says remote.
	if !at {
		if isDigits(rest) {
			return 0, false
		}
		if p, _, _, ok := splitRemotePosition(rest); ok && p == "" {
			return 0, false
		}
	}
	if at {
		return i, true
	}
	// Without a user the path must start at the remote root or home. This keeps the
	// local FILE:LINE form unambiguous: host.example:42 is a local file name (gvim
	// cannot tell it from a line suffix), while host.example:/srv/x and host:~/x are
	// remote. Use user@host:... to force a relative remote path.
	if strings.HasPrefix(rest, "/") || strings.HasPrefix(rest, "~") {
		return i, true
	}
	return 0, false
}

// parseSCP parses [user@]host:path[:line[:col]].
func parseSCP(arg string) (RemoteSpec, error) {
	i, _ := scpCandidate(arg)
	if i <= 0 {
		return RemoteSpec{}, fmt.Errorf("%s: not a remote target", arg)
	}
	head, rest := arg[:i], arg[i+1:]
	var user string
	userGiven := false
	if at := strings.LastIndex(head, "@"); at >= 0 {
		user, head = head[:at], head[at+1:]
		userGiven = true
		if user == "" {
			return RemoteSpec{}, fmt.Errorf("%s: empty user", arg)
		}
	}
	if head == "" {
		return RemoteSpec{}, fmt.Errorf("%s: empty host", arg)
	}
	path, line, col := rest, 0, 0
	if p, l, c, ok := splitRemotePosition(rest); ok {
		path, line, col = p, l, c
	}
	if path == "" {
		return RemoteSpec{}, fmt.Errorf("%s: missing remote path", arg)
	}
	return RemoteSpec{User: user, Host: head, Path: path, Line: line, Col: col, Raw: arg, UserExplicit: userGiven}, nil
}

// parseSSHURL parses ssh://[user@][host][:port]/path[:line[:col]]. Everything about the
// URL is an error at startup; the scheme is exclusive.
func parseSSHURL(arg string) (RemoteSpec, error) {
	rest := strings.TrimPrefix(arg, "ssh://")
	if rest == "" {
		return RemoteSpec{}, fmt.Errorf("%s: missing host and path", arg)
	}
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return RemoteSpec{}, fmt.Errorf("%s: missing path", arg)
	}
	authorityPart, rawPath := rest[:slash], rest[slash:]
	if authorityPart == "" {
		return RemoteSpec{}, fmt.Errorf("%s: empty host", arg)
	}
	var user string
	userFromURL := false
	if at := strings.LastIndex(authorityPart, "@"); at >= 0 {
		user, authorityPart = authorityPart[:at], authorityPart[at+1:]
		userFromURL = true
		if user == "" {
			return RemoteSpec{}, fmt.Errorf("%s: empty user", arg)
		}
	}
	host, port, err := splitHostPort(authorityPart)
	if err != nil {
		return RemoteSpec{}, fmt.Errorf("%s: %w", arg, err)
	}
	path, err := url.PathUnescape(rawPath)
	if err != nil {
		return RemoteSpec{}, fmt.Errorf("%s: bad path escape: %w", arg, err)
	}
	line, col := 0, 0
	if p, l, c, ok := splitRemotePosition(path); ok {
		path, line, col = p, l, c
	}
	if path == "" {
		return RemoteSpec{}, fmt.Errorf("%s: missing path", arg)
	}
	if user == "" {
		user = os.Getenv("USER")
		if user == "" {
			user = os.Getenv("USERNAME")
		}
		if user == "" {
			return RemoteSpec{}, fmt.Errorf("%s: no user in the URL and neither $USER nor $USERNAME is set", arg)
		}
	}
	return RemoteSpec{User: user, Host: host, Port: port, Path: path, Line: line, Col: col, Raw: arg, UserExplicit: userFromURL, PortExplicit: port != 0}, nil
}

// splitHostPort parses host or host:port, including a bracketed IPv6 host.
func splitHostPort(s string) (host string, port int, err error) {
	if s == "" {
		return "", 0, errors.New("empty host")
	}
	hostPart := s
	portPart := ""
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return "", 0, errors.New("unterminated IPv6 host")
		}
		hostPart = s[1:end]
		if tail := s[end+1:]; tail != "" {
			if !strings.HasPrefix(tail, ":") {
				return "", 0, errors.New("bad host")
			}
			portPart = tail[1:]
		}
	} else if strings.Count(s, ":") == 1 {
		hostPart, portPart, _ = strings.Cut(s, ":")
	} else if strings.Count(s, ":") > 1 {
		return "", 0, errors.New("bad host (bracket an IPv6 address)")
	}
	if hostPart == "" {
		return "", 0, errors.New("empty host")
	}
	if portPart != "" {
		n, perr := strconv.Atoi(portPart)
		if perr != nil || n < 1 || n > 65535 {
			return "", 0, fmt.Errorf("bad port %q", portPart)
		}
		port = n
	}
	return hostPart, port, nil
}

// splitRemotePosition cuts a trailing ":line[:col]" from a remote path. Both numbers are
// 1-based and only digits form a position; a colon in a path stays a path.
func splitRemotePosition(p string) (path string, line, col int, ok bool) {
	i := strings.LastIndex(p, ":")
	if i <= 0 {
		return "", 0, 0, false
	}
	last, ok := parsePosition(p[i+1:])
	if !ok {
		return "", 0, 0, false
	}
	head := p[:i]
	if j := strings.LastIndex(head, ":"); j > 0 {
		if first, ok := parsePosition(head[j+1:]); ok {
			return head[:j], first, last, true
		}
	}
	return head, last, 0, true
}

func isDigits(s string) bool { return s != "" && strings.Trim(s, "0123456789") == "" }

// parsePosition reads a positive decimal number.
func parsePosition(s string) (int, bool) {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// ShellQuote quotes one string as a POSIX shell word.
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'"+"\\"+"''") + "'"
}

// validTildeUser reports whether name is a plausible ~user prefix: the shell expands only
// a word consisting of the user name, so anything with spaces or metacharacters must be
// quoted literally instead (the cd then fails safely, rather than shell-splitting).
func validTildeUser(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.' || r == '+':
		default:
			return false
		}
	}
	return true
}

// quoteTildeTail quotes the part of a ~ path after its first slash. The slash itself stays
// unquoted, so the shell still sees the tilde word and expands it; only the tail (which may
// contain spaces, quotes or $()) is quoted. Quoting the slash too would make the whole
// tilde word quoted, which sh/bash do NOT expand (~'/proj' is the literal word ~/proj).
// tail always starts with "/".
func quoteTildeTail(tail string) string {
	rest := strings.TrimPrefix(tail, "/")
	if rest == "" {
		return "/"
	}
	return "/" + ShellQuote(rest)
}

// QuoteDir quotes a remote directory for a "cd" hop. A leading ~ (bare, ~/... or
// ~user/...) is kept expandable by the remote shell: bare ~ becomes "$HOME" (the shell's
// own expansion; note ~'/proj' would NOT expand, so the slash after the tilde stays
// unquoted), ~user is left unquoted when it is a valid user word, and the tail after the
// first slash is quoted.
func QuoteDir(dir string) string {
	if dir == "" {
		return ""
	}
	if strings.HasPrefix(dir, "~") {
		rest := dir[1:]
		i := strings.Index(rest, "/")
		if i < 0 {
			if rest == "" {
				return "\"$HOME\""
			}
			if validTildeUser(rest) {
				return "~" + rest
			}
			return ShellQuote(dir)
		}
		user, tail := rest[:i], rest[i:]
		if user == "" {
			return "\"$HOME\"" + quoteTildeTail(tail)
		}
		if !validTildeUser(user) {
			return ShellQuote(dir)
		}
		return "~" + user + quoteTildeTail(tail)
	}
	return ShellQuote(dir)
}

// RemoteCommand builds the shell command line an SSH exec channel runs: cd to dir (when
// given), export env entries (when any), then the quoted argv. SSH exec has no cwd/env
// flags, so this is the composition point. An empty argv has no command to build and
// returns "": the backends refuse it before they get here.
func RemoteCommand(argv []string, dir string, env map[string]string) string {
	if len(argv) == 0 {
		return ""
	}
	var b strings.Builder
	if dir != "" {
		b.WriteString("cd ")
		b.WriteString(QuoteDir(dir))
		b.WriteString(" && ")
	}
	if len(env) > 0 {
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("env")
		for _, k := range keys {
			b.WriteString(" ")
			b.WriteString(ShellQuote(k + "=" + env[k]))
		}
		b.WriteString(" ")
	}
	for i, a := range argv {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(ShellQuote(a))
	}
	return b.String()
}
