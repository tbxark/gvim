package tui

import (
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/split"
)

// The frame's geometry and hit map live in internal/tui/frame, and the split tree
// and the pane geometry in internal/tui/split. These aliases keep the root's
// painter and controllers reading the names they had before the extractions.
type (
	rect = space.Rect

	splitNode   = split.Node
	splitDir    = split.Dir
	splitPane   = split.Pane
	splitBorder = split.Border
	leafInfo    = split.LeafInfo

	layout      = frame.Layout
	layoutInput = frame.Input
	statusView  = frame.StatusView
	run         = frame.Run
	hitKind     = frame.HitKind
	hitTarget   = frame.HitTarget
	strip       = frame.Strip
	welcomeLine = frame.WelcomeLine
	statusItem  = frame.StatusItem

	// TabInfo is the root's name for the port type; the frame kit places the strip
	// with it and the app fills it.
	TabInfo = frame.TabInfo
)

const (
	sideBySide = split.SideBySide // split_vertical: the children are left and right
	stacked    = split.Stacked    // split_horizontal: the children are top and bottom

	gateText        = frame.GateText
	gateMinWidth    = frame.GateMinWidth
	gateMinHeight   = frame.GateMinHeight
	maxNoticeRows   = frame.MaxNoticeRows
	paletteHelp     = frame.PaletteHelp
	labelGap        = frame.LabelGap
	noNameLabel     = frame.NoNameLabel
	previewSuffix   = frame.PreviewSuffix
	modifiedMark    = frame.ModifiedMark
	comparePairMark = frame.ComparePairMark

	hitNone                = frame.HitNone
	hitMenuLabel           = frame.HitMenuLabel
	hitTab                 = frame.HitTab
	hitTabClose            = frame.HitTabClose
	hitTabNew              = frame.HitTabNew
	hitTabScrollBefore     = frame.HitTabScrollBefore
	hitTabScrollAfter      = frame.HitTabScrollAfter
	hitExplorerRow         = frame.HitExplorerRow
	hitExplorerTitleReload = frame.HitExplorerTitleReload
	hitExplorerTitleClose  = frame.HitExplorerTitleClose
	hitExplorerGrip        = frame.HitExplorerGrip
	hitExplorerBody        = frame.HitExplorerBody
	hitSidebarHeader       = frame.HitSidebarHeader
	hitSidebarClose        = frame.HitSidebarClose
	hitSidebarRow          = frame.HitSidebarRow
	hitSidebarBody         = frame.HitSidebarBody
	hitEditorText          = frame.HitEditorText
	hitGutter              = frame.HitGutter
	hitScrollbarTrack      = frame.HitScrollbarTrack
	hitStatusPalette       = frame.HitStatusPalette
	hitStatusSegment       = frame.HitStatusSegment
	hitWelcomeRow          = frame.HitWelcomeRow
	hitSplitBorder         = frame.HitSplitBorder
	hitStatusPlugin        = frame.HitStatusPlugin

	statusSegmentEOL      = frame.StatusSegmentEOL
	statusSegmentEncoding = frame.StatusSegmentEncoding
	statusSegmentLanguage = frame.StatusSegmentLanguage
	statusSegmentPosition = frame.StatusSegmentPosition
	statusSegmentLSP      = frame.StatusSegmentLSP
	statusSegmentUpdate   = frame.StatusSegmentUpdate
	statusSegmentTrust    = frame.StatusSegmentTrust

	welcomeBlank      = frame.WelcomeBlank
	welcomeTitleRow   = frame.WelcomeTitleRow
	welcomeSubtitle   = frame.WelcomeSubtitle
	welcomeHeadingRow = frame.WelcomeHeadingRow
	welcomeDoorRow    = frame.WelcomeDoorRow
	welcomeRecentRow  = frame.WelcomeRecentRow
)
