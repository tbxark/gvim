package plugin

// gvim.http: the restricted network surface (task-199).
//
//	gvim.http.get(url, opts?) ⏳ -> {status = 200, body = "...", headers = {...}} | nil, err
//
// The policy (only https, no credentials, SSRF address checks, redirects, timeout/body caps,
// concurrency, header whitelist, no request-header control) lives in http.go. This file is
// the Lua surface and the await plumbing. The explicit user opt-in is checked here before
// any URL/DNS work: the plugin's USER config layer must contain settings.network = true; a
// project-layer value never grants network access.
//
// opts = {timeout_ms = 5000} (clamped 100..10000).

import (
	"context"
	"strings"
	"time"

	"github.com/tbxark/gvim/pluginapi"
)

const (
	defaultHTTPTimeoutMS = 5000
	minHTTPTimeoutMS     = 100
	maxHTTPTimeoutMS     = 10000
)

func init() {
	registerAwait("http_get", awaitHTTPGet)
}

func installHTTP(e *apiEnv) {
	area := e.area("http")
	area.RawSetString("get", e.async("http_get"))
	e.provides("http_get")
}

// pluginNetworkAllowed reports whether the plugin's USER layer enabled network access.
// Project settings are untrusted and never grant it.
func pluginNetworkAllowed(p *plugin) bool {
	on, _ := p.data.user["network"].(bool)
	return on
}

func awaitHTTPGet(c *awaitCall) {
	if !pluginNetworkAllowed(c.p) {
		c.Fail(pluginapi.Errf(pluginapi.CodeUnsupported,
			"network is disabled for plugin %q: set plugins.%s.settings.network = true in the user config",
			c.p.name, c.p.name))
		return
	}
	raw, ok := stringArg(c.args, 0)
	if !ok || strings.TrimSpace(raw) == "" {
		c.Bad(1, "http.get", "a URL string is required")
		return
	}
	o := optionsArg(c.args, 1)
	timeoutMS := o.number("timeout_ms", defaultHTTPTimeoutMS, minHTTPTimeoutMS, maxHTTPTimeoutMS)
	if o.bad != "" {
		c.Bad(2, "http.get", o.bad)
		return
	}
	if _, err := validateHTTPURL(raw); err != nil {
		c.Fail(err)
		return
	}
	if err := c.CheckPark(false); err != nil {
		c.Fail(err)
		return
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond
	p := c.p
	c.Park(&parkSpec{Work: &pluginapi.Work{Run: func(ctx context.Context) pluginapi.Completion {
		res, err := c.r.http.get(ctx, p.name, c.r.version, raw, timeout)
		if err != nil {
			return pluginapi.Completion{Err: errString(err)}
		}
		return pluginapi.Completion{Values: []any{res}}
	}}})
}
