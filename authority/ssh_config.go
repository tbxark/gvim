package authority

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// OpenSSH config handling for the pure-Go SSH backend.
//
// Fresh shells out to the system ssh(1), so OpenSSH config is applied for it by
// the binary. gvim dials with x/crypto/ssh and therefore reads a bounded subset
// itself: Host/HostName/User/Port/IdentityFile/ProxyJump/IdentityAgent and
// ForwardAgent. The parser is deliberately small and fails closed on malformed
// input; unsupported directives (Include, Match, ProxyCommand, known-hosts and
// algorithm options, Canonicalize*) are ignored and are recorded as residuals in
// docs/authority.md. A missing config file is not an error: the old no-config
// behaviour is unchanged.

// sshConfigMaxBytes bounds one ssh config file.
const sshConfigMaxBytes = 256 << 10

// sshConfig limits, so a hostile or generated config cannot grow memory without
// bound.
const (
	sshConfigMaxBlocks   = 256
	sshConfigMaxPatterns = 32
	sshConfigMaxOptions  = 256
	sshConfigMaxLine     = 4096
)

// sshConfigPair is one directive in file order.
type sshConfigPair struct{ key, value string }

// sshHostBlock is one Host stanza.
type sshHostBlock struct {
	patterns []string
	pairs    []sshConfigPair
}

// sshConfig is the parsed user config.
type sshConfig struct {
	global []sshConfigPair
	blocks []sshHostBlock
}

// sshConfigValues is the resolved subset for one Host alias.
type sshConfigValues struct {
	HostName      string
	User          string
	Port          int
	IdentityFiles []string
	ProxyJump     string
	IdentityAgent string
	ForwardAgent  *bool
	Matched       bool
}

// DefaultSSHConfigPath is ~/.ssh/config, or "" when there is no home directory.
func DefaultSSHConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// loadSSHConfig reads path. An empty path disables config handling entirely
// (embedding/tests); a missing file is an empty config, never an error.
func loadSSHConfig(p string) (*sshConfig, error) {
	if p == "" {
		return nil, nil
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ssh config %s: %w", p, err)
	}
	if len(data) > sshConfigMaxBytes {
		return nil, fmt.Errorf("ssh config %s: larger than %d bytes", p, sshConfigMaxBytes)
	}
	cfg, err := parseSSHConfig(string(data))
	if err != nil {
		return nil, fmt.Errorf("ssh config %s: %w", p, err)
	}
	return cfg, nil
}

// parseSSHConfig parses the config text. It is line-oriented and bounded.
func parseSSHConfig(text string) (*sshConfig, error) {
	cfg := &sshConfig{}
	cur := -1 // -1: global section; else index of the current Host block
	ignoring := false
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > sshConfigMaxLine {
			return nil, fmt.Errorf("line %d is longer than %d bytes", n+1, sshConfigMaxLine)
		}
		key, value := splitSSHConfigLine(line)
		switch strings.ToLower(key) {
		case "host":
			if len(cfg.blocks) >= sshConfigMaxBlocks {
				return nil, fmt.Errorf("more than %d Host blocks", sshConfigMaxBlocks)
			}
			patterns := strings.Fields(value)
			if len(patterns) > sshConfigMaxPatterns {
				return nil, fmt.Errorf("line %d: more than %d Host patterns", n+1, sshConfigMaxPatterns)
			}
			if len(patterns) == 0 {
				return nil, fmt.Errorf("line %d: Host has no pattern", n+1)
			}
			cfg.blocks = append(cfg.blocks, sshHostBlock{patterns: patterns})
			cur = len(cfg.blocks) - 1
			ignoring = false
		case "match":
			// Conditional blocks are not interpreted; their options are skipped
			// until the next Host (recorded residual).
			ignoring = true
		case "include":
			// Includes are not followed (bounded parser); recorded residual.
		default:
			if ignoring {
				continue
			}
			pair := sshConfigPair{key: strings.ToLower(key), value: value}
			if cur < 0 {
				if len(cfg.global) >= sshConfigMaxOptions {
					return nil, fmt.Errorf("line %d: more than %d global options", n+1, sshConfigMaxOptions)
				}
				cfg.global = append(cfg.global, pair)
			} else {
				if len(cfg.blocks[cur].pairs) >= sshConfigMaxOptions {
					return nil, fmt.Errorf("line %d: more than %d options in one Host block", n+1, sshConfigMaxOptions)
				}
				cfg.blocks[cur].pairs = append(cfg.blocks[cur].pairs, pair)
			}
		}
	}
	return cfg, nil
}

// splitSSHConfigLine splits "Keyword value" (or "Keyword=value") and strips surrounding quotes and a
// trailing unquoted comment. Values with spaces keep them.
func splitSSHConfigLine(line string) (key, value string) {
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return line, ""
	}
	// The keyword ends at a blank or an equals sign, as in OpenSSH: "Key value",
	// "Key=value" and "Key = value".
	key, value = line[:i], strings.TrimLeft(line[i:], " \t")
	if rest, ok := strings.CutPrefix(value, "="); ok {
		value = rest
	}
	value = strings.TrimSpace(value)
	// Strip a trailing comment outside quotes.
	quoted := byte(0)
	for j := 0; j < len(value); j++ {
		c := value[j]
		if quoted != 0 {
			if c == quoted {
				quoted = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quoted = c
			continue
		}
		if c == '#' && (j == 0 || value[j-1] == ' ' || value[j-1] == '\t') {
			value = strings.TrimSpace(value[:j])
			break
		}
	}
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}
	return key, value
}

// matchesHost reports whether alias matches a Host stanza: any positive pattern
// matches and no negated ("!pattern") pattern does.
func (b sshHostBlock) matchesHost(alias string) bool {
	positive := false
	for _, p := range b.patterns {
		neg := strings.HasPrefix(p, "!")
		if neg {
			p = p[1:]
		}
		ok, err := path.Match(p, alias)
		if err != nil {
			continue
		}
		if neg && ok {
			return false
		}
		if !neg && ok {
			positive = true
		}
	}
	return positive
}

// resolve applies the first-obtained-value rule (global options first, then each
// matching Host block in file order). IdentityFile accumulates, as OpenSSH does.
func (c *sshConfig) resolve(alias string) sshConfigValues {
	var out sshConfigValues
	seen := map[string]bool{}
	home, _ := os.UserHomeDir()
	localUser := os.Getenv("USER")
	if localUser == "" {
		localUser = os.Getenv("USERNAME")
	}
	apply := func(pairs []sshConfigPair) {
		for _, p := range pairs {
			switch p.key {
			case "hostname":
				if !seen[p.key] {
					seen[p.key] = true
					out.HostName = expandSSHValue(p.value, alias, out, home, localUser)
					out.Matched = true
				}
			case "user":
				if !seen[p.key] {
					seen[p.key] = true
					out.User = expandSSHValue(p.value, alias, out, home, localUser)
					out.Matched = true
				}
			case "port":
				if !seen[p.key] {
					seen[p.key] = true
					if port, ok := parseSSHPort(p.value); ok {
						out.Port = port
						out.Matched = true
					}
				}
			case "identityfile":
				out.IdentityFiles = append(out.IdentityFiles, expandSSHPath(expandSSHValue(p.value, alias, out, home, localUser), home))
				out.Matched = true
			case "proxyjump":
				if !seen[p.key] {
					seen[p.key] = true
					out.ProxyJump = expandSSHValue(p.value, alias, out, home, localUser)
					out.Matched = true
				}
			case "identityagent":
				if !seen[p.key] {
					seen[p.key] = true
					out.IdentityAgent = expandSSHPath(expandSSHValue(p.value, alias, out, home, localUser), home)
					out.Matched = true
				}
			case "forwardagent":
				if !seen[p.key] {
					seen[p.key] = true
					b := parseSSHYesNo(p.value)
					out.ForwardAgent = &b
					out.Matched = true
				}
			}
		}
	}
	apply(c.global)
	for _, block := range c.blocks {
		if block.matchesHost(alias) {
			apply(block.pairs)
		}
	}
	return out
}

func parseSSHPort(s string) (int, bool) {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil || n < 1 || n > 65535 {
		return 0, false
	}
	return n, true
}

func parseSSHYesNo(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes", "true", "on", "1":
		return true
	}
	return false
}

// expandSSHValue substitutes the OpenSSH tokens the bounded parser supports:
// %h host (HostName if already known, else alias), %r remote user, %p port, %d
// home, %u local user, %%; a leading ~ is expanded for path-like values by
// expandSSHPath, not here.
func expandSSHValue(v, alias string, cur sshConfigValues, home, localUser string) string {
	if !strings.Contains(v, "%") {
		return v
	}
	host := cur.HostName
	if host == "" {
		host = alias
	}
	user := cur.User
	if user == "" {
		user = localUser
	}
	port := "22"
	if cur.Port != 0 {
		port = fmt.Sprintf("%d", cur.Port)
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] != '%' || i+1 >= len(v) {
			b.WriteByte(v[i])
			continue
		}
		i++
		switch v[i] {
		case 'h':
			b.WriteString(host)
		case 'r':
			b.WriteString(user)
		case 'p':
			b.WriteString(port)
		case 'd':
			b.WriteString(home)
		case 'u':
			b.WriteString(localUser)
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(v[i])
		}
	}
	return b.String()
}

// expandSSHPath expands a leading ~ in an IdentityFile/IdentityAgent value.
func expandSSHPath(v, home string) string {
	if home == "" {
		return v
	}
	if v == "~" {
		return home
	}
	if strings.HasPrefix(v, "~/") {
		return filepath.Join(home, v[2:])
	}
	return v
}
