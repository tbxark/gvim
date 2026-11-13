// Package updatecheck answers "is a newer gvim released?" against a release
// feed, for the status row's {update} indicator.
//
// It is TUI-free: the editor runs the check in a background command and paints
// the answer. A check is one bounded request (a timeout, a 64 KiB body cap), a
// configured feed must be https (an http URL on the loopback interface is
// allowed for a local mirror or a test server), and the last successful check is
// remembered in a stamp file so the editor asks the feed at most once a day. A
// failed check is silent: the indicator stays absent and the next launch tries
// again.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultReleasesURL is the feed gvim polls when neither the config nor the
	// environment names one: the GitHub releases API of the project.
	DefaultReleasesURL = "https://api.github.com/repos/tbxark/gvim/releases/latest"
	// ReleasesURLEnv overrides the feed URL for every launch, like Fresh's
	// endpoint environment variables.
	ReleasesURLEnv = "GVIM_RELEASES_URL"
	// DefaultInterval is how often a successful check asks the feed again.
	DefaultInterval = 24 * time.Hour

	// requestTimeout bounds one check.
	requestTimeout = 8 * time.Second
	// maxFeedBytes bounds the feed body. The GitHub release object is a few
	// kilobytes; anything larger is not a release feed.
	maxFeedBytes = 64 << 10
)

// Release is a released version the check found, newer than this build.
type Release struct {
	// Version is the release's version without the "v" prefix, such as "0.4.2".
	Version string
	// URL is the release page, when the feed names one.
	URL string
}

// CurrentVersion returns the version of this build, without the "v" prefix, or
// "" when the build has no version (a "go run" build or a development binary).
// A build without a version never checks: it cannot say what "newer" means.
func CurrentVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	v := strings.TrimSpace(info.Main.Version)
	if v == "" || v == "(devel)" || v == "devel" {
		return ""
	}
	return strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
}

// ReleasesURL returns the feed URL for this launch: ReleasesURLEnv when it is
// set, else DefaultReleasesURL. The value is returned as written; ValidateURL
// applies the transport policy when the check runs.
func ReleasesURL() string {
	if v := strings.TrimSpace(os.Getenv(ReleasesURLEnv)); v != "" {
		return v
	}
	return DefaultReleasesURL
}

// ValidateURL applies the transport policy to a feed URL: an absolute https URL,
// or an http URL whose host is the loopback interface (a local mirror or a test
// server). Anything else is refused rather than silently polled in the clear.
func ValidateURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("update check: feed URL: %w", err)
	}
	if u.Host == "" || u.Scheme == "" {
		return "", errors.New("update check: the feed URL is not absolute")
	}
	switch u.Scheme {
	case "https":
		return u.String(), nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return u.String(), nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return u.String(), nil
		}
		return "", fmt.Errorf("update check: refusing the plain-http feed %q: only https, or http on the loopback interface", raw)
	default:
		return "", fmt.Errorf("update check: unsupported feed scheme %q", u.Scheme)
	}
}

// Checker is one launch's release check.
type Checker struct {
	// Current is this build's version (see CurrentVersion). Empty disables the
	// check.
	Current string
	// URL is the feed (see ReleasesURL).
	URL string
	// Client makes the request; nil uses a client with the package timeout.
	Client *http.Client
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Interval is how long a successful check is remembered; zero means
	// DefaultInterval.
	Interval time.Duration
	// StampPath is the file that remembers the last successful check; empty
	// disables the debounce (every call checks).
	StampPath string
}

// stamp is the file that remembers the last successful check, so a launch inside
// the interval neither asks the feed again nor forgets a release it already knows.
// A version 1 file carried only the timestamp; the extra fields are optional, so
// such a file still debounces (and reports no known release).
type stamp struct {
	Checked int64  `json:"checked"`
	Latest  string `json:"latest,omitempty"`
	URL     string `json:"url,omitempty"`
}

// readStamp returns the stored stamp, or false when there is none or it is
// unreadable.
func (c *Checker) readStamp() (stamp, bool) {
	if c.StampPath == "" {
		return stamp{}, false
	}
	data, err := os.ReadFile(c.StampPath)
	if err != nil {
		return stamp{}, false
	}
	var st stamp
	if err := json.Unmarshal(data, &st); err != nil || st.Checked <= 0 {
		return stamp{}, false
	}
	return st, true
}

// Due reports whether a check is allowed now: the stamp is missing, unreadable,
// or older than the interval.
func (c *Checker) Due() bool {
	st, ok := c.readStamp()
	if !ok {
		return true
	}
	then := time.Unix(st.Checked, 0)
	return c.now().Sub(then) >= c.interval()
}

// Last returns the release the last successful check recorded, and when that
// check ran. It reports false when no check has succeeded. This is what lets a
// launch inside the interval paint the indicator without asking the feed.
func (c *Checker) Last() (Release, time.Time, bool) {
	st, ok := c.readStamp()
	if !ok {
		return Release{}, time.Time{}, false
	}
	return Release{Version: st.Latest, URL: st.URL}, time.Unix(st.Checked, 0), true
}

// Check asks the feed once and returns the newer release, or nil when this build
// is the newest (or the feed has no newer version). A failure is an error the
// caller may ignore: it is never fatal, and a successful check is the only one
// that writes the stamp.
func (c *Checker) Check(ctx context.Context) (*Release, error) {
	if strings.TrimSpace(c.Current) == "" {
		return nil, nil
	}
	if _, _, err := parseVersion(c.Current); err != nil {
		return nil, fmt.Errorf("update check: the build version %q: %w", c.Current, err)
	}
	feed, err := ValidateURL(c.URL)
	if err != nil {
		return nil, err
	}
	body, err := c.fetch(ctx, feed)
	if err != nil {
		return nil, err
	}
	latest, page, err := parseFeed(body)
	if err != nil {
		return nil, err
	}
	if Compare(latest, c.Current) <= 0 {
		c.stamp("", "")
		return nil, nil
	}
	c.stamp(latest, page)
	return &Release{Version: latest, URL: page}, nil
}

// fetch performs the request and returns at most maxFeedBytes of the body.
func (c *Checker) fetch(ctx context.Context, feed string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
	if err != nil {
		return nil, fmt.Errorf("update check: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gvim/"+c.Current)
	client := c.Client
	if client == nil {
		client = &http.Client{CheckRedirect: checkRedirect}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("update check: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update check: the feed answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("update check: %w", err)
	}
	if len(body) > maxFeedBytes {
		return nil, fmt.Errorf("update check: the feed is larger than %d bytes", maxFeedBytes)
	}
	return body, nil
}

// checkRedirect holds a redirect target to the transport policy of ValidateURL.
func checkRedirect(req *http.Request, _ []*http.Request) error {
	_, err := ValidateURL(req.URL.String())
	return err
}

// parseFeed reads the version and page from a release feed. GitHub's release
// object names the release "tag_name"; a small mirror may use "version". Either
// may carry the "v" prefix.
func parseFeed(body []byte) (version, page string, err error) {
	var feed struct {
		TagName string `json:"tag_name"`
		Version string `json:"version"`
		HTMLURL string `json:"html_url"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(body, &feed); err != nil {
		return "", "", fmt.Errorf("update check: the feed is not JSON: %w", err)
	}
	version = strings.TrimSpace(feed.TagName)
	if version == "" {
		version = strings.TrimSpace(feed.Version)
	}
	if version == "" {
		return "", "", errors.New("update check: the feed names no version (tag_name or version)")
	}
	page = strings.TrimSpace(feed.HTMLURL)
	if page == "" {
		page = strings.TrimSpace(feed.URL)
	}
	return strings.TrimPrefix(strings.TrimPrefix(version, "v"), "V"), page, nil
}

// stamp remembers a successful check and, when there is one, the release it
// found. Best effort: a stamp that cannot be written only means the next launch
// asks the feed again.
func (c *Checker) stamp(latest, page string) {
	if c.StampPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.StampPath), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(stamp{Checked: c.now().Unix(), Latest: latest, URL: page})
	if err != nil {
		return
	}
	_ = os.WriteFile(c.StampPath, data, 0o600)
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) interval() time.Duration {
	if c.Interval > 0 {
		return c.Interval
	}
	return DefaultInterval
}

// Compare orders two version strings: -1 when a is older, 0 when they are the
// same release, 1 when a is newer. Versions are "MAJOR[.MINOR[.PATCH...]]" with
// an optional "-PRERELEASE" and "+BUILD" suffix; a leading "v" is ignored. A
// release is newer than its prereleases, and a missing part counts as zero, so
// "1.2" equals "1.2.0". A prerelease compares part by part: numeric parts
// numerically and before alphanumeric ones. An unparsable version sorts as
// older than any parsable one, which keeps a development build quiet.
func Compare(a, b string) int {
	an, ap, aerr := parseVersion(a)
	bn, bp, berr := parseVersion(b)
	if aerr != nil || berr != nil {
		switch {
		case aerr != nil && berr != nil:
			return 0
		case aerr != nil:
			return -1
		default:
			return 1
		}
	}
	for i := 0; i < len(an) || i < len(bn); i++ {
		var x, y int
		if i < len(an) {
			x = an[i]
		}
		if i < len(bn) {
			y = bn[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	if len(ap) == 0 && len(bp) == 0 {
		return 0
	}
	if len(ap) == 0 {
		return 1
	}
	if len(bp) == 0 {
		return -1
	}
	for i := 0; i < len(ap) || i < len(bp); i++ {
		if i >= len(ap) {
			return -1
		}
		if i >= len(bp) {
			return 1
		}
		x, y := ap[i], bp[i]
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case xerr == nil && yerr == nil:
			if xn != yn {
				if xn < yn {
					return -1
				}
				return 1
			}
		case xerr == nil:
			return -1
		case yerr == nil:
			return 1
		default:
			if x != y {
				if x < y {
					return -1
				}
				return 1
			}
		}
	}
	return 0
}

// parseVersion splits a version into its numeric parts and its prerelease parts.
func parseVersion(s string) (nums []int, pre []string, err error) {
	v := strings.TrimSpace(s)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	if v == "" {
		return nil, nil, errors.New("empty version")
	}
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre = strings.Split(v[i+1:], ".")
		v = v[:i]
	}
	if v == "" {
		return nil, nil, fmt.Errorf("%q has no release number", s)
	}
	for _, part := range strings.Split(v, ".") {
		n, convErr := strconv.Atoi(part)
		if convErr != nil || n < 0 {
			return nil, nil, fmt.Errorf("%q is not a version", s)
		}
		nums = append(nums, n)
	}
	for _, p := range pre {
		if p == "" {
			return nil, nil, fmt.Errorf("%q has an empty prerelease part", s)
		}
	}
	return nums, pre, nil
}
