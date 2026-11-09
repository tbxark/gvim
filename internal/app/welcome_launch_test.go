package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// welcomeTabOpen reports whether the real editor has the welcome plugin's virtual tab.
func welcomeTabOpen(rt *runtime) bool {
	for _, tab := range rt.ed.Tabs() {
		if strings.Contains(tab.Name, "Welcome") {
			return true
		}
	}
	return false
}

// TestWelcomeAutoOpenFollowsTheFileLaunchSeam drives the real app startup and the real
// welcome plugin: a file launch suppresses the automatic page, while a bare launch and
// restored session tabs (not a file launch) open it. The explicit Welcome command is
// covered by the bundled plugin suite and stays available.
func TestWelcomeAutoOpenFollowsTheFileLaunchSeam(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	writeTestFile(t, a, "one\n")
	writeTestFile(t, b, "two\n")

	cases := []struct {
		name     string
		config   string
		sess     bool
		opts     func(root, sess string) Options
		wantOpen bool
	}{
		{
			name:     "bare launch opens the background page",
			opts:     func(root, _ string) Options { return Options{Root: root} },
			wantOpen: true,
		},
		{
			name:     "explicit file launch suppresses it",
			opts:     func(root, _ string) Options { return Options{Root: root, Files: []FileTarget{{Path: a}}} },
			wantOpen: false,
		},
		{
			name: "explicit file launch over a saved session still suppresses it",
			sess: true,
			opts: func(root, sess string) Options {
				return Options{Root: root, SessionDir: sess, Files: []FileTarget{{Path: a}}}
			},
			wantOpen: false,
		},
		{
			name:     "--simple suppresses it",
			sess:     true,
			opts:     func(root, sess string) Options { return Options{Root: root, SessionDir: sess, Simple: true} },
			wantOpen: false,
		},
		{
			name: "--diff suppresses it",
			sess: true,
			opts: func(root, sess string) Options {
				return Options{Root: root, SessionDir: sess, Diff: true, Files: []FileTarget{{Path: a}, {Path: b}}}
			},
			wantOpen: false,
		},
		{
			name: "a directory-only root is not a file launch",
			opts: func(root, _ string) Options {
				return Options{Root: root}
			},
			wantOpen: true,
		},
		{
			name: "standalone stdin is not a Fresh file launch",
			sess: true,
			opts: func(root, sess string) Options {
				return Options{Root: root, SessionDir: sess, Stdin: []byte("piped\n")}
			},
			wantOpen: true,
		},
		{
			name: "stdin with explicit files follows the files rule",
			opts: func(root, _ string) Options {
				return Options{Root: root, Stdin: []byte("piped\n"), Files: []FileTarget{{Path: a}}}
			},
			wantOpen: false,
		},
		{
			name:     "a restored session is not a file launch",
			sess:     true,
			opts:     func(root, sess string) Options { return Options{Root: root, SessionDir: sess} },
			wantOpen: true,
		},
		{
			name: "--restore with files is a session launch",
			sess: true,
			opts: func(root, sess string) Options {
				return Options{Root: root, SessionDir: sess, Restore: true, Files: []FileTarget{{Path: a}}}
			},
			wantOpen: true,
		},
		{
			name:   "skip_session_restore_when_files_passed=false is a session launch",
			config: "\"editor\": {\"skip_session_restore_when_files_passed\": false}, ",
			sess:   true,
			opts: func(root, sess string) Options {
				return Options{Root: root, SessionDir: sess, Files: []FileTarget{{Path: a}}}
			},
			wantOpen: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			cfg := "{" + tt.config + "\"plugins\":{\"welcome_screen\":{\"enabled\":true}}}"
			writeUserFile(t, "config.json", cfg)
			sess := ""
			if tt.sess {
				sess, _ = seedSession(t, root)
			}
			s := newSession(t, tt.opts(root, sess))
			s.waitStatus("welcome_screen", "loaded")
			if tt.wantOpen {
				s.until("the background welcome tab", func() bool { return welcomeTabOpen(s.rt) })
				return
			}
			// Let the plugins_loaded/ready handlers drain before asserting absence.
			s.settle(120 * time.Millisecond)
			if welcomeTabOpen(s.rt) {
				t.Fatalf("welcome page auto-opened for a file launch (tabs %v)", s.rt.ed.Tabs())
			}
		})
	}
}
