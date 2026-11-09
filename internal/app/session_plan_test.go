package app

import (
	"testing"

	"github.com/tbxark/gvim/config"
)

// sessionConfig returns the default config, which has the three session keys at
// Fresh's defaults (all true), with the given changes applied.
func sessionConfig(t *testing.T, change func(*config.Config)) config.Config {
	t.Helper()
	cfg := config.Defaults()
	if change != nil {
		change(&cfg)
	}
	return cfg
}

func TestPlanSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts Options
		cfg  config.Config
		want sessionPlan
	}{
		{
			name: "bare launch restores and saves the session and hot-exits",
			opts: Options{SessionDir: "/s"},
			cfg:  sessionConfig(t, nil),
			want: sessionPlan{load: true, tabs: true, save: true, hotExit: true, explorer: true},
		},
		{
			name: "bare launch with hot_exit off asks on quit",
			opts: Options{SessionDir: "/s"},
			cfg:  sessionConfig(t, func(c *config.Config) { c.HotExit = false }),
			want: sessionPlan{load: true, tabs: true, save: true, explorer: true},
		},
		{
			name: "a file launch skips the session by default",
			opts: Options{SessionDir: "/s", Files: []FileTarget{{Path: "a.txt"}}},
			cfg:  sessionConfig(t, nil),
			want: sessionPlan{fileLaunch: true, explorer: true},
		},
		{
			name: "a file launch with no session directory",
			opts: Options{Files: []FileTarget{{Path: "a.txt"}}},
			cfg:  sessionConfig(t, nil),
			want: sessionPlan{fileLaunch: true, explorer: true},
		},
		{
			name: "restore_previous_session off keeps only the unsaved text",
			opts: Options{SessionDir: "/s"},
			cfg:  sessionConfig(t, func(c *config.Config) { c.RestorePreviousSession = false }),
			want: sessionPlan{load: true, hot: true, save: true, hotExit: true, explorer: true},
		},
		{
			name: "no-restore skips the tabs but keeps the unsaved text",
			opts: Options{SessionDir: "/s", NoRestore: true},
			cfg:  sessionConfig(t, nil),
			want: sessionPlan{load: true, hot: true, save: true, hotExit: true, explorer: true},
		},
		{
			name: "restore overrides restore_previous_session off",
			opts: Options{SessionDir: "/s", Restore: true},
			cfg:  sessionConfig(t, func(c *config.Config) { c.RestorePreviousSession = false }),
			want: sessionPlan{load: true, tabs: true, save: true, hotExit: true, explorer: true},
		},
		{
			name: "restore makes a launch with files a session launch",
			opts: Options{SessionDir: "/s", Restore: true, Files: []FileTarget{{Path: "a.txt"}}},
			cfg:  sessionConfig(t, nil),
			want: sessionPlan{load: true, tabs: true, save: true, hotExit: true, explorer: true},
		},
		{
			name: "skip_session_restore_when_files_passed off restores with files",
			opts: Options{SessionDir: "/s", Files: []FileTarget{{Path: "a.txt"}}},
			cfg: sessionConfig(t, func(c *config.Config) {
				c.SkipSessionRestoreWhenFilesPassed = false
			}),
			want: sessionPlan{load: true, tabs: true, save: true, hotExit: true, explorer: true},
		},
		{
			name: "skip_session_restore_when_files_passed off still honors the restore config",
			opts: Options{SessionDir: "/s", Files: []FileTarget{{Path: "a.txt"}}},
			cfg: sessionConfig(t, func(c *config.Config) {
				c.SkipSessionRestoreWhenFilesPassed = false
				c.RestorePreviousSession = false
			}),
			want: sessionPlan{load: true, hot: true, save: true, hotExit: true, explorer: true},
		},
		{
			name: "no-restore overrides restore_previous_session on",
			opts: Options{SessionDir: "/s", Files: []FileTarget{{Path: "a.txt"}}, NoRestore: true},
			cfg: sessionConfig(t, func(c *config.Config) {
				c.SkipSessionRestoreWhenFilesPassed = false
			}),
			want: sessionPlan{load: true, hot: true, save: true, hotExit: true, explorer: true},
		},
		{
			name: "simple restores and saves nothing",
			opts: Options{SessionDir: "/s", Simple: true},
			cfg:  sessionConfig(t, nil),
			want: sessionPlan{fileLaunch: true},
		},
		{
			name: "simple with a directory and files opens just them",
			opts: Options{SessionDir: "/s", Simple: true, Root: "/w", Files: []FileTarget{{Path: "a.txt"}}},
			cfg:  sessionConfig(t, nil),
			want: sessionPlan{fileLaunch: true},
		},
		{
			name: "a compare launch is a file launch even with restore",
			opts: Options{SessionDir: "/s", Diff: true, Restore: true, Files: []FileTarget{{Path: "a"}, {Path: "b"}}},
			cfg:  sessionConfig(t, nil),
			want: sessionPlan{fileLaunch: true, explorer: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := planSession(tt.opts, tt.cfg); got != tt.want {
				t.Fatalf("planSession = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestPlanSessionRootOnlyIsNotAFileLaunch: a directory argument is the workspace root, not
// a file target; Fresh's fileLaunch is false for it (and for a bare launch).
func TestPlanSessionRootOnlyIsNotAFileLaunch(t *testing.T) {
	t.Parallel()
	cfg := sessionConfig(t, nil)
	if plan := planSession(Options{Root: "/w", SessionDir: "/s"}, cfg); plan.fileLaunch {
		t.Fatalf("directory-only plan = %+v, want no file launch", plan)
	}
	if plan := planSession(Options{Root: "/w", SessionDir: "/s", Files: []FileTarget{{Path: "/w/a.txt"}}}, cfg); !plan.fileLaunch {
		t.Fatalf("file plan = %+v, want a file launch", plan)
	}
}
