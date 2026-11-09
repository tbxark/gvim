package plugin_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
)

// rewriteTransport sends requests for the public test URLs to a local httptest TLS
// server. The URL validation/SSRF policy still sees the public hostname; the transport
// only replaces the destination, so no real network access happens.
type rewriteTransport struct {
	base   http.RoundTripper
	target *url.URL
	hits   atomic.Int32
	mu     sync.Mutex
	last   http.Header
}

func (r *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.hits.Add(1)
	r.mu.Lock()
	r.last = req.Header.Clone()
	r.mu.Unlock()
	clone := req.Clone(req.Context())
	clone.URL.Scheme = r.target.Scheme
	clone.URL.Host = r.target.Host
	clone.Host = r.target.Host
	return r.base.RoundTrip(clone)
}

// httpPlugin mounts a command that calls gvim.http.get(url) and reports the result.
func httpPlugin(rawURL string, timeoutMS int) string {
	return fmt.Sprintf("\nlocal gvim = require 'gvim'\ngvim.command.register{ name = 'get', run = function()\n  local res, err = gvim.http.get(%q, { timeout_ms = %d })\n  if res then\n    local h = res.headers or {}\n    gvim.notify('ok:' .. tostring(res.status) .. ':' .. res.body .. ':etag=' .. tostring(h['etag']) .. ':secret=' .. tostring(h['x-secret']))\n  else\n    gvim.notify('err:' .. tostring(err))\n  end\nend }\n", rawURL, timeoutMS)
}

type httpTest struct {
	h      *plugintest.Harness
	rt     *rewriteTransport
	server *httptest.Server
}

func newHTTPTest(t *testing.T, handler http.HandlerFunc, rawURL string, timeoutMS int, user, project map[string]any) *httpTest {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	rt := &rewriteTransport{base: server.Client().Transport, target: target}
	h := plugintest.New(t, plugintest.Files{"t.lua": httpPlugin(rawURL, timeoutMS)}, plugintest.Options{
		HTTPClient:  &http.Client{Transport: rt},
		Timeout:     2 * time.Second,
		EventBudget: time.Second,
	})
	h.Sources[0].Settings = user
	h.Sources[0].ProjectSettings = project
	h.Start()
	return &httpTest{h: h, rt: rt, server: server}
}

func (x *httpTest) run(t *testing.T) string {
	t.Helper()
	if err := x.h.Run("t.get", nil); err != nil {
		t.Fatal(err)
	}
	x.h.Settle()
	return strings.Join(x.h.NoticeTexts(), "|")
}

func TestHTTPDisabledByDefault(t *testing.T) {
	x := newHTTPTest(t, func(w http.ResponseWriter, r *http.Request) {}, "https://example.com/x", 1000, nil, nil)
	notices := x.run(t)
	if !strings.Contains(notices, "network is disabled") {
		t.Fatalf("notices = %q, want the network gate", notices)
	}
	if x.rt.hits.Load() != 0 {
		t.Fatalf("a disabled plugin made %d requests", x.rt.hits.Load())
	}
}

func TestHTTPProjectSettingsNeverGrantNetwork(t *testing.T) {
	x := newHTTPTest(t, func(w http.ResponseWriter, r *http.Request) {}, "https://example.com/x", 1000, nil, map[string]any{"network": true})
	notices := x.run(t)
	if !strings.Contains(notices, "network is disabled") {
		t.Fatalf("notices = %q, want a refusal", notices)
	}
	if x.rt.hits.Load() != 0 {
		t.Fatalf("a project-layer network setting made %d requests", x.rt.hits.Load())
	}
}

func TestHTTPEnabledGETAndHeaders(t *testing.T) {
	var got http.Header
	x := newHTTPTest(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("ETag", "\"abc\"")
		w.Header().Set("X-Secret", "hush")
		_, _ = fmt.Fprint(w, "hello")
	}, "https://example.com/hello", 1000, map[string]any{"network": true}, nil)
	notices := x.run(t)
	if !strings.Contains(notices, "ok:200:hello:etag=\"abc\":secret=") || strings.Contains(notices, "hush") {
		t.Fatalf("notices = %q, want status/body/etag and no x-secret", notices)
	}
	if x.rt.hits.Load() != 1 {
		t.Fatalf("requests = %d", x.rt.hits.Load())
	}
	if got.Get("Authorization") != "" || got.Get("Cookie") != "" {
		t.Fatalf("sensitive headers sent: %v", got)
	}
	if ua := got.Get("User-Agent"); !strings.HasPrefix(ua, "gvim-plugin") {
		t.Fatalf("User-Agent = %q", ua)
	}
	if got.Get("Accept") != "application/json" {
		t.Fatalf("Accept = %q", got.Get("Accept"))
	}
}

func TestHTTPOnlyHTTPS(t *testing.T) {
	x := newHTTPTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("an http URL reached the server")
	}, "http://example.com/x", 1000, map[string]any{"network": true}, nil)
	notices := x.run(t)
	if !strings.Contains(notices, "scheme must be https") {
		t.Fatalf("notices = %q", notices)
	}
	if x.rt.hits.Load() != 0 {
		t.Fatalf("an http URL made a request")
	}
}

func TestHTTPRedirectToHTTPRefused(t *testing.T) {
	x := newHTTPTest(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/elsewhere", http.StatusFound)
	}, "https://example.com/start", 1000, map[string]any{"network": true}, nil)
	notices := x.run(t)
	if !strings.Contains(notices, "scheme must be https") {
		t.Fatalf("notices = %q", notices)
	}
	if x.rt.hits.Load() != 1 {
		t.Fatalf("redirect requests = %d, want only the https start", x.rt.hits.Load())
	}
}

func TestHTTPRedirectLimit(t *testing.T) {
	x := newHTTPTest(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/loop", http.StatusFound)
	}, "https://example.com/loop", 1000, map[string]any{"network": true}, nil)
	notices := x.run(t)
	if !strings.Contains(notices, "redirects") {
		t.Fatalf("notices = %q", notices)
	}
	if hits := x.rt.hits.Load(); hits > 4 {
		t.Fatalf("followed %d redirects, want a bounded number", hits)
	}
}

func TestHTTPTimeoutAndSizeCaps(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		x := newHTTPTest(t, func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			_, _ = fmt.Fprint(w, "late")
		}, "https://example.com/slow", 100, map[string]any{"network": true}, nil)
		start := time.Now()
		notices := x.run(t)
		if !strings.Contains(notices, "timed out") {
			t.Fatalf("notices = %q", notices)
		}
		if time.Since(start) > 2*time.Second {
			t.Fatalf("the timeout call blocked too long: %v", time.Since(start))
		}
	})
	t.Run("too large", func(t *testing.T) {
		x := newHTTPTest(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(make([]byte, 1<<20+1))
		}, "https://example.com/big", 2000, map[string]any{"network": true}, nil)
		notices := x.run(t)
		if !strings.Contains(notices, "larger than") {
			t.Fatalf("notices = %q", notices)
		}
	})
}
