package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestValidateHTTPURLPolicy(t *testing.T) {
	good := []string{
		"https://example.com/",
		"https://api.github.com/repos/o/r/pulls?state=open",
		"https://example.com:8443/x",
	}
	for _, raw := range good {
		if _, err := validateHTTPURL(raw); err != nil {
			t.Errorf("validateHTTPURL(%q) = %v, want accepted", raw, err)
		}
	}
	bad := []string{
		"",
		"http://example.com/",
		"ftp://example.com/",
		"javascript:alert(1)",
		"file:///etc/passwd",
		"https://user:pass@example.com/",
		"https://localhost/",
		"https://foo.localhost/",
		"https://something.local/",
		"https:///nohost",
		"not a url",
		"https://example.com/a b",
		"https://example.com/a\nb",
		"https://127.0.0.1/",
		"https://10.0.0.1/",
		"https://192.168.1.1/",
		"https://172.16.0.1/",
		"https://169.254.169.254/latest/meta-data/",
		"https://[::1]/",
		"https://[fe80::1]/",
		"https://[fc00::1]/",
		"https://100.64.0.1/",
		"https://0.0.0.0/",
	}
	for _, raw := range bad {
		if _, err := validateHTTPURL(raw); err == nil {
			t.Errorf("validateHTTPURL(%q) accepted, want refusal", raw)
		}
	}
	// The length cap.
	if _, err := validateHTTPURL("https://example.com/" + strings.Repeat("a", maxHTTPURLLen)); err == nil {
		t.Error("an over-long URL was accepted")
	}
}

func TestCheckPublicHTTPAddr(t *testing.T) {
	public := []string{"93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"}
	for _, raw := range public {
		if err := checkPublicHTTPAddr(netip.MustParseAddr(raw)); err != nil {
			t.Errorf("%s = %v, want public", raw, err)
		}
	}
	private := []string{"127.0.0.1", "10.1.2.3", "192.168.0.1", "172.16.4.5", "169.254.1.1", "::1", "fe80::1", "fd00::1", "0.0.0.0", "224.0.0.1", "100.64.0.1", "198.18.0.1"}
	for _, raw := range private {
		if err := checkPublicHTTPAddr(netip.MustParseAddr(raw)); err == nil {
			t.Errorf("%s accepted, want refusal", raw)
		}
	}
}

// TestSafeHTTPDialNoFallback checks the dialer refuses a private literal directly and a
// DNS failure is returned (not retried against the hostname/another resolver).
func TestSafeHTTPDialNoFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := safeHTTPDial(ctx, "tcp", "127.0.0.1:443"); err == nil {
		t.Fatal("dialing 127.0.0.1 was allowed")
	}
	if _, err := safeHTTPDial(ctx, "tcp", "[::1]:443"); err == nil {
		t.Fatal("dialing ::1 was allowed")
	}
	if _, err := safeHTTPDial(ctx, "tcp", "no-such-host.invalid:443"); err == nil {
		t.Fatal("a DNS failure fell back instead of failing")
	}
}

// TestHTTPConcurrencyCaps checks the per-plugin (2) and global (4) slots, including the
// context-aware wait.
func TestHTTPConcurrencyCaps(t *testing.T) {
	h := newHTTPState(nil)
	ctx := context.Background()
	for i := 0; i < httpPerPlugin; i++ {
		if err := h.acquire(ctx, "p"); err != nil {
			t.Fatalf("acquire p %d: %v", i, err)
		}
	}
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := h.acquire(short, "p"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("third per-plugin request err = %v, want a wait", err)
	}
	// A second plugin can take the other two global slots; a third plugin then waits.
	for i := 0; i < httpPerPlugin; i++ {
		if err := h.acquire(ctx, "q"); err != nil {
			t.Fatalf("acquire q %d: %v", i, err)
		}
	}
	short2, cancel2 := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel2()
	if err := h.acquire(short2, "r"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("fifth global request err = %v, want a wait", err)
	}
	h.release("p")
	h.release("p")
	h.release("q")
	h.release("q")
	if err := h.acquire(ctx, "r"); err != nil {
		t.Fatalf("after release: %v", err)
	}
	h.release("r")
}

// TestClientRedirectPolicy checks the redirect callback: non-https and unsafe targets are
// refused, and the hop count is limited.
func TestClientRedirectPolicy(t *testing.T) {
	h := newHTTPState(nil)
	c := h.clientFor()
	req, _ := http.NewRequest("GET", "https://example.com/next", nil)
	if err := c.CheckRedirect(req, []*http.Request{req}); err != nil {
		t.Fatalf("one https redirect = %v", err)
	}
	httpReq, _ := http.NewRequest("GET", "http://example.com/next", nil)
	if err := c.CheckRedirect(httpReq, []*http.Request{httpReq}); err == nil {
		t.Fatal("an http redirect was followed")
	}
	localReq, _ := http.NewRequest("GET", "https://127.0.0.1/x", nil)
	if err := c.CheckRedirect(localReq, []*http.Request{localReq}); err == nil {
		t.Fatal("a loopback redirect was followed")
	}
	via := make([]*http.Request, maxHTTPRedirects+1)
	for i := range via {
		via[i] = req
	}
	if err := c.CheckRedirect(req, via); err == nil {
		t.Fatal("the redirect hop cap was not enforced")
	}
}
