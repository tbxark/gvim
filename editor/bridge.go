package editor

import "github.com/tbxark/gvim/document"

// Bridge from the old editor package surface to package document. The types,
// values and functions below were moved to document; these aliases keep the
// rest of the module compiling while the editor package is the session half.
//
// This file is temporary: consumers are retargeted to document over time.

type Action = document.Action

const CursorMap = document.CursorMap
const DefaultPageWidth = document.DefaultPageWidth

type DocID = document.DocID
type Document = document.Document
type EditOptions = document.EditOptions

var ErrAlreadyOpen = document.ErrAlreadyOpen
var ErrBinary = document.ErrBinary
var ErrEmptyPath = document.ErrEmptyPath
var ErrIndexing = document.ErrIndexing
var ErrLargeFile = document.ErrLargeFile
var ErrNoFold = document.ErrNoFold
var ErrNoPath = document.ErrNoPath
var ErrNotDecodable = document.ErrNotDecodable
var ErrNothingSelected = document.ErrNothingSelected
var ErrPasteTooLarge = document.ErrPasteTooLarge
var ErrReadOnly = document.ErrReadOnly
var ErrRestoring = document.ErrRestoring
var ErrScanLimit = document.ErrScanLimit
var ErrStaleRevert = document.ErrStaleRevert
var ErrStashStale = document.ErrStashStale
var ErrTooLargeToTranscode = document.ErrTooLargeToTranscode
var ErrUnknownEncoding = document.ErrUnknownEncoding
var ErrVirtual = document.ErrVirtual

type LSPEdit = document.LSPEdit
type LanguageSettings = document.LanguageSettings

const MaxViewColumn = document.MaxViewColumn
const MaxViewTabSize = document.MaxViewTabSize
const MaxVirtualText = document.MaxVirtualText

type NonCodeContext = document.NonCodeContext

var OverrideOr = document.OverrideOr

type PropSpan = document.PropSpan
type SaveJob = document.SaveJob
type SaveResult = document.SaveResult
type SettingsResolver = document.SettingsResolver
type StringContext = document.StringContext
type ViewSettings = document.ViewSettings
type VirtualOptions = document.VirtualOptions

var absPath = document.AbsPath
var angleBrackets = document.AngleBrackets
var asciiWordRune = document.AsciiWordRune
var blankPrefix = document.BlankPrefix
var bracketPairs = document.BracketPairs
var bracketTarget = document.BracketTarget

type caret = document.CaretState

var caretLines = document.CaretLines
var cellColumn = document.CellColumn
var cellsFrom = document.CellsFrom
var cleanVirtualName = document.CleanVirtualName
var closerIndent = document.CloserIndent
var cmpLineCol = document.CmpLineCol
var colAtCell = document.ColAtCell
var colAtCellFrom = document.ColAtCellFrom

type contentMark = document.ContentMark
type cursorSnapshot = document.CursorSnapshot
type dabbrevCycle = document.DabbrevCycle

const defaultCodec = document.DefaultCodec

type diskState = document.DiskState

// fileAccess is the document file I/O seam (nil = local filesystem).
type fileAccess = document.FileAccess

var diskStateOf = document.DiskStateOf

type docExt = document.DocExt
type edit = document.Edit

var encodingLabel = document.EncodingLabel

const eolCRLF = document.EolCRLF
const eolLF = document.EolLF

var errFileChanged = document.ErrFileChanged
var errLargeAction = document.ErrLargeAction
var errNotLargeNow = document.ErrNotLargeNow
var firstNonBlank = document.FirstNonBlank
var frontierSensitive = document.FrontierSensitive
var hasText = document.HasText

type indentRules = document.IndentRulesState

var indentRulesOf = document.IndentRulesOf
var indentString = document.IndentString

type indentView = document.IndentView

const indexBatchBytes = document.IndexBatchBytes

var indexUnfinished = document.IndexUnfinished
var isCloser = document.IsCloser
var joinsNextLine = document.JoinsNextLine

type jumpEntry = document.JumpEntry

var jumpEntryOf = document.JumpEntryOf

const keywordDedentReach = document.KeywordDedentReach

type largeError = document.LargeError
type largeSpec = document.LargeSpec

var lastCharCol = document.LastCharCol
var leadColumns = document.LeadColumns

type lineEdit = document.LineEdit

var lineEnds = document.LineEnds
var lineIndexStatus = document.LineIndexStatus
var loadDocument = document.LoadDocument

type loadSpec = document.LoadSpec

var lowerCase = document.LowerCase
var matchingBracket = document.MatchingBracket

const maxFullLoad = document.MaxFullLoad
const maxIndentScan = document.MaxIndentScan

var mtimeNano = document.MtimeNano
var newDocument = document.NewDocument
var newStdinDocument = document.NewStdinDocument
var newLineIndent = document.NewLineIndent
var newScanBudget = document.NewScanBudget
var nextBoundary = document.NextBoundary
var nextPos = document.NextPos
var normalizePaste = document.NormalizePaste
var onLastChar = document.OnLastChar
var openSavedSource = document.OpenSavedSource
var orDefault = document.OrDefault
var pairFor = document.PairFor
var pairedBrackets = document.PairedBrackets
var pairedQuotes = document.PairedQuotes

type pending = document.Pending

const pendingAction = document.PendingAction
const pendingGoto = document.PendingGoto

type placeFn = document.PlaceFn

var prevBoundary = document.PrevBoundary
var prevPos = document.PrevPos
var readDocument = document.ReadDocument
var replaceEdit = document.ReplaceEdit
var rowBounds = document.RowBounds
var rowOf = document.RowOf
var sameFile = document.SameFile
var scanningStatus = document.ScanningStatus
var selectionOrLines = document.SelectionOrLines

type sourceOpener = document.SourceOpener

var splitLineEnding = document.SplitLineEnding
var textChanging = document.TextChanging
var upperCase = document.UpperCase
var viLastLine = document.ViLastLine
var within = document.Within
var wordEnd = document.WordEnd
var wordLeft = document.WordLeft
var wordRight = document.WordRight
var writtenOf = document.WrittenOf

// Find-selection limits and refusals that moved to document.
const maxSearchSelection = document.MaxSearchSelection

var errSearchTextTooLarge = document.ErrSearchTextTooLarge

// Types and values consumers still name through package editor.
type BlockRect = document.BlockRect
type Caret = document.Caret

const CursorEnd = document.CursorEnd
const CursorKeep = document.CursorKeep

type CursorMode = document.CursorMode

var EncodingChoices = document.EncodingChoices
var ErrChangedOnDisk = document.ErrChangedOnDisk
var ErrExists = document.ErrExists

type ErrLargeNeedsFullLoad = document.ErrLargeNeedsFullLoad

var ErrOverlappingEdits = document.ErrOverlappingEdits
var ErrStale = document.ErrStale

type FormatterSpec = document.FormatterSpec
type IndentRules = document.IndentRules
type OnSaveAction = document.OnSaveAction
type Version = document.Version

// More names the editor tests use.
const ScanBytesBudget = document.ScanBytesBudget
const ScanLinesCap = document.ScanLinesCap
const bulkEditMin = document.BulkEditMin
const changePayloadMax = document.ChangePayloadMax
const changeRingBytes = document.ChangeRingBytes
const changeRingRecords = document.ChangeRingRecords

var clusterCells = document.ClusterCells

const clusterWholeLine = document.ClusterWholeLine

var comparePos = document.ComparePos
var errFileGrew = document.ErrFileGrew
var formatSize = document.FormatSize

const largeLimitsHint = document.LargeLimitsHint
const largeSampleSize = document.LargeSampleSize

var libraryPath = document.LibraryPath

const maxLargeCarets = document.MaxLargeCarets
const maxLargeLines = document.MaxLargeLines
const maxVirtualName = document.MaxVirtualName

type multiEditRecord = document.MultiEditRecord

var normalize = document.Normalize

type scanBudget = document.ScanBudget

var unwritable = document.Unwritable

const walkChunk = document.WalkChunk
