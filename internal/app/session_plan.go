package app

import "github.com/tbxark/gvim/config"

// sessionPlan says what a launch does with the saved session and with the
// unsaved text the session keeps. gvim has one session directory per workspace and
// no per-buffer hot-exit store, so Fresh's launch modes map onto the session file
// gvim already writes (session/ and internal/restore). A daemon workspace has its
// own session directory and capture path (workspace_capture.go).
//
// The three config keys involved are editor.restore_previous_session,
// editor.skip_session_restore_when_files_passed and editor.hot_exit, and the
// one-shot flags are --restore, --no-restore (Fresh's --no-session) and
// --simple.
type sessionPlan struct {
	// fileLaunch records Fresh's is_file_launch decision for plugins: --simple or
	// --diff, or explicit non-directory files with the skip-session setting on and no
	// --restore. A standalone stdin launch is not a Fresh file launch even though
	// gvim still keeps it out of the session (sessionLaunch below). It is fixed at
	// startup and exposed read-only to plugins.
	fileLaunch bool
	// load reads the saved session file at startup.
	load bool
	// tabs restores the saved tabs, their cursors and the explorer state.
	tabs bool
	// hot restores only the stashed unsaved text — the dirty files and the
	// unnamed buffers — when tabs is false. It is Fresh's
	// restore_previous_session=false behavior: skip the layout, keep the work.
	hot bool
	// save writes the session when the program ends.
	save bool
	// hotExit grants a quit without asking about each dirty tab: the session
	// save at exit keeps their text for the next start. Off, a quit asks
	// save/discard per dirty tab, as it always has.
	hotExit bool
	// explorer lets the launch show the file explorer: from a directory
	// argument, or from the restored session. --simple hides it.
	explorer bool
}

// planSession returns what a launch of opts does under cfg.
//
// A launch with files is a file launch under Fresh's default:
// editor.skip_session_restore_when_files_passed skips the session for it, so
// only the named files open and the session is neither restored nor saved.
// --restore turns such a launch back into a session launch, and setting the
// config key to false does it for every launch. A session launch restores the
// saved tabs unless restore_previous_session is off or --no-restore is given,
// in which case only the saved unsaved text (the hot-exit content) comes back;
// the session is still saved when it ends, as Fresh documents. --simple is a
// file launch whatever is named, and saves nothing.
//
// hotExit follows editor.hot_exit, but only when the launch will actually save
// a session: a file launch and --simple write no session, so keeping unsaved
// buffers "for later" would lose them and those launches keep asking instead.
// gvim has no per-buffer hot-exit store, unlike Fresh.
func planSession(opts Options, cfg config.Config) sessionPlan {
	files := len(opts.Files) > 0
	// A file launch is a launch with files that Fresh's default config keeps out
	// of the session. --restore forces the session; --simple forces a file
	// launch whatever is named. A compare launch is always a file launch: the
	// compare view is its two named files.
	fileLaunch := files && cfg.SkipSessionRestoreWhenFilesPassed && !opts.Restore
	if opts.Simple || opts.Diff {
		fileLaunch = true
	}
	// The session side is broader than Fresh's plugin-visible fileLaunch: gvim keeps a
	// stdin launch out of the session (the piped text is not session state and must
	// never be saved), but Fresh's is_file_launch does not count stdin alone. Keeping
	// the two decisions separate preserves both the recorded session semantics and the
	// plugin property.
	sessionLaunch := !fileLaunch && !opts.StdinLaunch()
	hasStore := opts.SessionDir != ""
	plan := sessionPlan{
		fileLaunch: fileLaunch,
		load:       hasStore && sessionLaunch,
		save:       hasStore && sessionLaunch,
		explorer:   !opts.Simple,
	}
	if !plan.load {
		return plan
	}
	plan.tabs = !opts.NoRestore && (opts.Restore || cfg.RestorePreviousSession)
	plan.hot = !plan.tabs
	plan.hotExit = cfg.HotExit
	return plan
}
