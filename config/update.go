package config

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// UpdateConfig is the release-check section: Fresh's check_for_updates and the
// gvim extension that names the feed.
type UpdateConfig struct {
	// Check is check_for_updates: look for a newer release in the background
	// when the editor starts. Fresh's default is true.
	Check bool
	// URL is the release feed to poll (gvim extension update_check_url). Empty
	// means the default feed, or the GVIM_RELEASES_URL environment override.
	// Only an https URL, or an http URL on the loopback interface for a local
	// mirror, is accepted (see package updatecheck).
	URL string
}

// DefaultUpdate returns the release-check defaults: checking is on (Fresh's
// default) and the feed is the built-in one.
func DefaultUpdate() UpdateConfig { return UpdateConfig{Check: true} }

// setUpdate applies one release-check key. The value is checked here only for
// shape; the transport policy (https) is enforced when the check runs, so a
// configuration file with a mirror the policy refuses is diagnosed at use.
func (c *Config) setUpdate(key string, raw json.RawMessage) error {
	switch key {
	case "check_for_updates":
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return errors.New("must be true or false")
		}
		c.Update.Check = v
	case "update_check_url":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return errors.New("must be a URL string")
		}
		s = strings.TrimSpace(s)
		if s == "" {
			c.Update.URL = ""
			return nil
		}
		u, err := url.Parse(s)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return errors.New("must be an absolute http(s) URL")
		}
		c.Update.URL = s
	}
	return nil
}
