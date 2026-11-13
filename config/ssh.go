package config

import (
	"encoding/json"
	"errors"
)

// SSHConfig is the ssh section: host-key verification policy, host-key algorithm
// preferences and password-keyring consent for the pure-Go SSH backend (gvim
// extensions; Fresh delegates SSH to the system ssh(1)). The policy is user-layer only:
// a repository cannot choose how its host keys are verified.
type SSHConfig struct {
	// KnownHostsPolicy is "strict" (default) or "accept-new". Strict refuses an unknown
	// host; accept-new records a first-seen host key in known_hosts after a clear notice
	// and always refuses a changed key. There is no "off": host keys are always verified.
	KnownHostsPolicy string
	// HostKeyAlgorithms is the ordered server host-key algorithm preference. Empty uses
	// the transport's safe default (ed25519, ECDSA, RSA-SHA2). An unknown name is a
	// clear dial/config error.
	HostKeyAlgorithms []string
	// Keyring enables the OS keyring integration: look up a host password before
	// prompting, and (after a notice) store a password the server accepted. Default
	// false: no lookup and no storage without this explicit consent.
	Keyring bool
}

// DefaultSSHConfig returns the SSH defaults: strict host-key policy, the transport's
// safe algorithm order and no keyring use.
func DefaultSSHConfig() SSHConfig {
	return SSHConfig{KnownHostsPolicy: "strict"}
}

// setSSH applies the object value of the top-level "ssh" key.
func (c *Config) setSSH(raw json.RawMessage, path string) []Diagnostic {
	entries, err := decodeObject(raw)
	if err != nil {
		return []Diagnostic{{Path: path, Key: "ssh", Message: err.Error()}}
	}
	var diags []Diagnostic
	for _, key := range sortedKeys(entries) {
		full := "ssh." + key
		if err := c.setSSHKey(key, entries[key]); err != nil {
			diags = append(diags, Diagnostic{Path: path, Key: full, Message: err.Error()})
			continue
		}
		c.note(full, path)
	}
	return diags
}

func (c *Config) setSSHKey(key string, raw json.RawMessage) error {
	switch key {
	case "known_hosts_policy":
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		switch s {
		case "strict", "accept-new":
			c.SSH.KnownHostsPolicy = s
			return nil
		case "off":
			return errors.New("\"off\" is not supported: host keys are always verified, so use \"strict\" or \"accept-new\"")
		default:
			return errors.New("must be \"strict\" or \"accept-new\"")
		}
	case "host_key_algorithms":
		if isNull(raw) {
			c.SSH.HostKeyAlgorithms = nil
			return nil
		}
		var names []string
		if err := json.Unmarshal(raw, &names); err != nil {
			return errors.New("must be an array of host-key algorithm names")
		}
		for _, name := range names {
			if name == "" {
				return errors.New("algorithm names must not be empty")
			}
		}
		c.SSH.HostKeyAlgorithms = append([]string(nil), names...)
		return nil
	case "keyring":
		var on bool
		if err := json.Unmarshal(raw, &on); err != nil {
			return errors.New("must be true or false")
		}
		c.SSH.Keyring = on
		return nil
	}
	return errors.New("unknown setting")
}
