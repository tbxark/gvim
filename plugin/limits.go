package plugin

import (
	"time"

	lua "github.com/yuin/gopher-lua"
)

// APIVersion is the plugin API level (gvim.api_version). A plugin manifest may ask for
// a minimum level; the loader supports 1..APIVersion.
//
// Level 2 adds the declarative widget panel and sidebar-section API (gvim.ui.mount_widget_panel,
// update_widget_panel, unmount_widget_panel, widget_command, widget_mutate,
// mount_sidebar_section), plus the theme override, menu, action popup, prompt-suggestion,
// plugin i18n and all-files diagnostics surfaces of tier 1a.
const APIVersion = 2

// Size limits (AMENDMENTS C5). Every Go function that allocates in proportion to its
// arguments computes the final size before allocating and raises beyond these caps.
// Sizes use int64 arithmetic and division, never an unchecked multiplication.
const (
	MaxString     = 64 << 20 // a string crossing the API, string.rep result, table.concat result
	MaxFormat     = 1 << 20  // string.format result
	MaxJSONNodes  = 1_000_000
	MaxJSONDepth  = 200
	MaxSortN      = 500_000
	MaxSplitParts = 1_000_000
	MaxPadWidth   = 8192
	MaxDateFormat = 1024
)

// Limits of data handed to the host (FINAL 2.4).
const (
	maxHostNodes = 1_000_000 // nodes in a table passed to the host
	maxHostDepth = 32        // depth of such a table
	maxLogLine   = 4 << 10   // one log message
	maxLogRing   = 2000      // entries kept in the log
	maxNotify    = 512       // bytes of a gvim.notify message
	maxHandles   = 10_000    // cached Buffer/Window handles per state
)

// Quotas, counted per plugin (FINAL 2.4). Exceeding one returns `nil, "limit: ..."`.
const (
	QuotaSubscriptions = 256
	QuotaCommands      = 512
	QuotaTimers        = 64
	QuotaProcesses     = 16
	QuotaNamespaces    = 64
	QuotaBindings      = 4096
	QuotaStatusItems   = 8
	QuotaWork          = 32 // in-flight Work items
	QuotaParkedTasks   = 64
	QuotaPanels        = 8      // mounted widget panels and sidebar sections
	QuotaMenuItems     = 32     // menu bar rows a plugin adds
	maxTasks           = 10_000 // live tasks of one plugin (parked plus queued)
)

// maxArrayIndex is the largest integer key a table keeps in its array part. gopher-lua
// pads the array with nils up to a sparse key below lua.MaxArrayIndex (64M slots, about a GiB
// in one VM instruction that neither the time budget nor the heap guard can interrupt), so
// the cap is lowered here; larger keys live in the hash part. JSON arrays stop at
// MaxJSONNodes, which still fits.
const maxArrayIndex = 1 << 20

func init() { lua.MaxArrayIndex = maxArrayIndex }

// maxCoDepth bounds nested coroutine resumes per state (AMENDMENTS C9), like the C stack
// limit of reference Lua. Together with CallStackSize it bounds the worst-case memory of
// runaway pcall/wrap recursion.
const maxCoDepth = 190

// Time budgets (FINAL 2.4, D16).
const (
	DefaultTimeout   = 500 * time.Millisecond // plugin_timeout_ms
	MaxTimeout       = 60 * time.Second
	eventBudget      = 50 * time.Millisecond  // upper bound of an event/timer slice
	vetoBudget       = 25 * time.Millisecond  // before_file_save, validate callbacks
	unloadBudget     = 200 * time.Millisecond // on_unload
	shutdownBudget   = 200 * time.Millisecond // all shutdown handlers together
	shutdownEach     = 50 * time.Millisecond
	perUpdateCap     = 100 * time.Millisecond // all event-class slices of one Update
	maxLoadBudget    = 5 * time.Second
	hardFactor       = 4 // the hard deadline is hardFactor x the slice budget
	strikeWindow     = 60 * time.Second
	strikeLimit      = 3
	noticeDedupe     = 10 * time.Second
	maxDrainRounds   = 8    // cascade rounds per Drain
	maxInvocations   = 1000 // event handler invocations per Drain
	stormDrains      = 10   // consecutive capped Drains with a plugin's own events queued: a strike
	userStormDrains  = 10   // consecutive cut Drains with a plugin's own tasks queued: a strike
	maxFailureStreak = 5    // consecutive failures after which a subscription is dropped
	guardStartAfter  = 2 * time.Millisecond
	guardSampleEvery = time.Millisecond
	defaultHeapLimit = 128 << 20
)
