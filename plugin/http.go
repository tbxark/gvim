package plugin

// Restricted HTTP GET for plugins (gvim.http.get). The security boundary lives here, not in
// the Lua caller:
//   - only absolute https URLs; credentials, localhost/private/link-local addresses and
//     non-public resolved addresses are refused (SSRF), and a DNS failure is final (no
//     fallback to the hostname or another resolver);
//   - redirects are limited and every hop is revalidated (https + SSRF policy);
//   - the response is bounded in time and bytes and stays in memory (never written to disk);
//   - only safe response headers are returned; callers cannot set request headers at all
//     (the User-Agent and Accept are set here), so Authorization/Cookie cannot be sent;
//   - per-plugin (2) and global (4) concurrency caps keep a plugin from exhausting the host.
// The caller must additionally have user-layer settings network = true; this is checked by
// the await handler before any DNS or request work.

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/pluginapi"
)

const (
	defaultHTTPTimeout = 5 * time.Second
	minHTTPTimeout     = 100 * time.Millisecond
	maxHTTPTimeout     = 10 * time.Second
	maxHTTPRedirects   = 3
	maxHTTPBody        = 1 << 20 // 1 MiB
	maxHTTPURLLen      = 4096
	httpPerPlugin      = 2
	httpGlobal         = 4
)

// httpState is the per-runtime HTTP capability: the client (tests inject one) and the
// concurrency slots.
type httpState struct {
	client *http.Client

	global chan struct{}
	mu     sync.Mutex
	per    map[string]chan struct{}
}

func newHTTPState(client *http.Client) *httpState {
	return &httpState{
		client: client,
		global: make(chan struct{}, httpGlobal),
		per:    map[string]chan struct{}{},
	}
}

// pluginSlot returns the per-plugin semaphore, creating it once per plugin name.
func (h *httpState) pluginSlot(name string) chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.per[name]
	if s == nil {
		s = make(chan struct{}, httpPerPlugin)
		h.per[name] = s
	}
	return s
}

// acquire takes a per-plugin and a global slot, honouring the request context. The
// per-plugin slot is acquired first so one plugin cannot starve the global slots while
// waiting for its own.
func (h *httpState) acquire(ctx context.Context, plugin string) error {
	local := h.pluginSlot(plugin)
	select {
	case local <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case h.global <- struct{}{}:
		return nil
	case <-ctx.Done():
		<-local
		return ctx.Err()
	}
}

func (h *httpState) release(plugin string) {
	<-h.global
	<-h.pluginSlot(plugin)
}

// httpPolicyError reports a URL/host/redirect refused by policy (invalid, not IO).
type httpPolicyError struct{ reason string }

func (e *httpPolicyError) Error() string { return "http: " + e.reason }

func httpPolicy(reason string, args ...any) error {
	return &httpPolicyError{reason: fmt.Sprintf(reason, args...)}
}

// httpTooLarge reports a response over the body cap.
type httpTooLarge struct{ n int64 }

func (e *httpTooLarge) Error() string {
	return fmt.Sprintf("http: response is larger than %d bytes", maxHTTPBody)
}

// validateHTTPURL applies the scheme/credential/host-literal policy. Hostnames are only
// checked for the obvious local names here; their addresses are validated at dial time.
func validateHTTPURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, httpPolicy("empty URL")
	}
	if len(raw) > maxHTTPURLLen {
		return nil, httpPolicy("URL longer than %d bytes", maxHTTPURLLen)
	}
	for _, r := range raw {
		if r <= 0x20 || r == 0x7f {
			return nil, httpPolicy("URL contains whitespace or a control character")
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Opaque != "" {
		return nil, httpPolicy("not an absolute URL with a host")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return nil, httpPolicy("scheme must be https, not %q", u.Scheme)
	}
	if u.User != nil {
		return nil, httpPolicy("credentials in the URL are not allowed")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if err := checkHTTPHost(host); err != nil {
		return nil, err
	}
	return u, nil
}

// checkHTTPHost refuses local names and non-public IP literals. A DNS name that is not
// obviously local still has its resolved addresses checked by safeHTTPDial.
func checkHTTPHost(host string) error {
	if host == "" {
		return httpPolicy("missing host")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		host == "localhost.localdomain" || strings.HasSuffix(host, ".local") {
		return httpPolicy("local host %q is not allowed", host)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return checkPublicHTTPAddr(ip)
	}
	return nil
}

// checkPublicHTTPAddr refuses anything that is not a public unicast address.
func checkPublicHTTPAddr(ip netip.Addr) error {
	if !ip.IsValid() {
		return httpPolicy("invalid address")
	}
	ip = ip.Unmap()
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || !ip.IsGlobalUnicast() {
		return httpPolicy("address %s is not public", ip)
	}
	for _, p := range nonPublicHTTPPrefixes {
		if p.Contains(ip) {
			return httpPolicy("address %s is not public", ip)
		}
	}
	return nil
}

// nonPublicHTTPPrefixes are address ranges that are not a public destination even though
// netip.IsGlobalUnicast can report them as global. The IPv4-embedded IPv6 forms matter
// for SSRF: a NAT64/6to4/Teredo/IPv4-compatible address can encode 127.0.0.1 or 10.0.0.1
// and a translator/relay on the host's network may then reach the private IPv4 target,
// so the whole translation prefix is refused rather than trying to decode every form.
var nonPublicHTTPPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),   // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("::/96"),           // IPv4-compatible (deprecated), can embed IPv4
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64 well-known prefix
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local-use NAT64
	netip.MustParsePrefix("2002::/16"),       // 6to4, embeds an IPv4 address
	netip.MustParsePrefix("2001::/32"),       // Teredo, embeds an IPv4 address
	netip.MustParsePrefix("fec0::/10"),       // deprecated IPv6 site-local (private)
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("2001:2::/48"),     // benchmarking
	netip.MustParsePrefix("3fff::/20"),       // documentation (RFC 9637)
	netip.MustParsePrefix("100::/64"),        // discard-only
	netip.MustParsePrefix("2001:10::/28"),    // ORCHID overlay (not public)
	netip.MustParsePrefix("2001:20::/28"),    // ORCHIDv2 overlay (not public)
}

// safeHTTPDial is the DialContext of the production transport: it resolves the host itself,
// refuses if any resolved address is non-public (no mixed-address fallback), and dials a
// validated literal address. TLS still verifies the URL hostname because the transport sees
// the original request URL.
func safeHTTPDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, httpPolicy("invalid dial address %q", addr)
	}
	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{ip}
	} else {
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("http: resolve %s: %w", host, err)
		}
		addrs = ips
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("http: resolve %s: no addresses", host)
	}
	for _, ip := range addrs {
		if err := checkPublicHTTPAddr(ip); err != nil {
			return nil, err
		}
	}
	d := &net.Dialer{}
	return d.DialContext(ctx, network, net.JoinHostPort(addrs[0].Unmap().String(), port))
}

// safeHTTPTransport clones t and replaces its dialer with safeHTTPDial. Proxy support is
// removed: an environment proxy would bypass the address policy. TLS 1.2 is the floor.
func safeHTTPTransport(t *http.Transport) *http.Transport {
	cloned := t.Clone()
	cloned.Proxy = nil
	cloned.DialContext = safeHTTPDial
	if cloned.TLSClientConfig == nil {
		cloned.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else if cloned.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		cloned.TLSClientConfig = cloned.TLSClientConfig.Clone()
		cloned.TLSClientConfig.MinVersion = tls.VersionTLS12
	}
	return cloned
}

// httpResultHeaders are the only response headers returned to Lua.
var httpResultHeaders = map[string]bool{
	"content-type":          true,
	"content-length":        true,
	"etag":                  true,
	"last-modified":         true,
	"cache-control":         true,
	"link":                  true,
	"x-ratelimit-limit":     true,
	"x-ratelimit-remaining": true,
	"x-ratelimit-reset":     true,
}

// clientFor returns a copy of the configured client with the redirect policy and the safe
// transport installed. A custom RoundTripper (tests) is preserved; in production there is
// no client and the safe default transport is used.
func (h *httpState) clientFor() *http.Client {
	var base http.Client
	if h.client != nil {
		base = *h.client
	}
	switch t := base.Transport.(type) {
	case nil:
		base.Transport = safeHTTPTransport(http.DefaultTransport.(*http.Transport))
	case *http.Transport:
		base.Transport = safeHTTPTransport(t)
	}
	base.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > maxHTTPRedirects {
			return httpPolicy("more than %d redirects", maxHTTPRedirects)
		}
		if _, err := validateHTTPURL(req.URL.String()); err != nil {
			return err
		}
		return nil
	}
	return &base
}

// get performs one bounded HTTPS GET. It is called from a Work goroutine, never the update
// loop. The result map is converted to Lua by the await framework.
func (h *httpState) get(ctx context.Context, pluginName, version, raw string, timeout time.Duration) (map[string]any, error) {
	u, err := validateHTTPURL(raw)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := h.acquire(ctx, pluginName); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, pluginapi.Errf(pluginapi.CodeTimeout, "http: timed out")
		}
		return nil, pluginapi.Errf(pluginapi.CodeCancelled, "http: cancelled")
	}
	defer h.release(pluginName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, httpPolicy("bad request: %v", err)
	}
	req.Header.Set("User-Agent", httpUserAgent(version))
	req.Header.Set("Accept", "application/json")
	resp, err := h.clientFor().Do(req)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.ContentLength > maxHTTPBody {
		return nil, &httpTooLarge{n: resp.ContentLength}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBody+1))
	if err != nil {
		var large *httpTooLarge
		if errors.As(err, &large) {
			return nil, large
		}
		if ctx.Err() != nil {
			return nil, httpError(ctx, err)
		}
		return nil, pluginapi.Errf(pluginapi.CodeIO, "http: read: %v", err)
	}
	if len(body) > maxHTTPBody {
		return nil, &httpTooLarge{n: int64(len(body))}
	}
	headers := make(map[string]any, len(httpResultHeaders))
	for name, values := range resp.Header {
		key := strings.ToLower(name)
		if !httpResultHeaders[key] || len(values) == 0 {
			continue
		}
		headers[key] = values[0]
	}
	return map[string]any{
		"status":  resp.StatusCode,
		"body":    string(body),
		"headers": headers,
	}, nil
}

// httpError maps a transport error to a plugin error: policy stays invalid, a deadline is
// timeout, DNS/resolution and every other network failure is io. No fallback happens here.
func httpError(ctx context.Context, err error) error {
	var policy *httpPolicyError
	if errors.As(err, &policy) {
		return pluginapi.Errf(pluginapi.CodeInvalid, "%v", err)
	}
	var large *httpTooLarge
	if errors.As(err, &large) {
		return pluginapi.Errf(pluginapi.CodeTooLarge, "%v", err)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return pluginapi.Errf(pluginapi.CodeTimeout, "http: timed out")
	}
	if errors.Is(err, context.Canceled) {
		return pluginapi.Errf(pluginapi.CodeCancelled, "http: cancelled")
	}
	return pluginapi.Errf(pluginapi.CodeIO, "http: %v", err)
}

// httpUserAgent is the only identifying request header sent. version is gvim.version.
func httpUserAgent(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "gvim-plugin"
	}
	return "gvim-plugin/" + version
}
