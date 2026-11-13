package document

// Exported aliases for the editor side. These names let package editor use the
// document internals it still needs; they are aliases, not copies, so they keep
// the exact type identity and behaviour of the unexported names.

var AbsPath = absPath
var AngleBrackets = angleBrackets
var ASCIIWordRune = asciiWordRune
var BlankPrefix = blankPrefix
var BracketPairs = bracketPairs
var BracketTarget = bracketTarget

type CaretState = caret

var CaretLines = caretLines
var CellColumn = cellColumn
var CellsFrom = cellsFrom
var CleanVirtualName = cleanVirtualName
var CloserIndent = closerIndent
var CmpLineCol = cmpLineCol
var ColAtCell = colAtCell
var ColAtCellFrom = colAtCellFrom

type ContentMark = contentMark
type CursorSnapshot = cursorSnapshot
type DabbrevCycle = dabbrevCycle

const DefaultCodec = defaultCodec

type DiskState = diskState

var DiskStateOf = diskStateOf

type DocExt = docExt
type Edit = edit

var EncodingLabel = encodingLabel

const EolCRLF = eolCRLF
const EolLF = eolLF

var ErrFileChanged = errFileChanged
var ErrLargeAction = errLargeAction
var ErrNotLargeNow = errNotLargeNow
var FirstNonBlank = firstNonBlank
var FrontierSensitive = frontierSensitive
var HasText = hasText

type IndentRulesState = indentRules

var IndentRulesOf = indentRulesOf
var IndentString = indentString

type IndentView = indentView

const IndexBatchBytes = indexBatchBytes

var IndexUnfinished = indexUnfinished
var IsCloser = isCloser
var JoinsNextLine = joinsNextLine

type JumpEntry = jumpEntry

var JumpEntryOf = jumpEntryOf

const KeywordDedentReach = keywordDedentReach

type LargeError = largeError
type LargeSpec = largeSpec

var LastCharCol = lastCharCol
var LeadColumns = leadColumns

type LineEdit = lineEdit

var LineEnds = lineEnds
var LineIndexStatus = lineIndexStatus
var LoadDocument = loadDocument

type LoadSpec = loadSpec

var LowerCase = lowerCase
var MatchingBracket = matchingBracket

const MaxFullLoad = maxFullLoad
const MaxIndentScan = maxIndentScan

var MtimeNano = mtimeNano
var NewDocument = newDocument
var NewLineIndent = newLineIndent
var NewScanBudget = newScanBudget
var NextBoundary = nextBoundary
var NextPos = nextPos
var NormalizePaste = normalizePaste
var OnLastChar = onLastChar
var OpenSavedSource = openSavedSource
var OrDefault = orDefault
var PairFor = pairFor
var PairedBrackets = pairedBrackets
var PairedQuotes = pairedQuotes

type Pending = pending

const PendingAction = pendingAction
const PendingGoto = pendingGoto

type PlaceFn = placeFn

var PrevBoundary = prevBoundary
var PrevPos = prevPos
var ReadDocument = readDocument
var ReplaceEdit = replaceEdit
var RowBounds = rowBounds
var RowOf = rowOf
var SameFile = sameFile
var ScanningStatus = scanningStatus
var SelectionOrLines = selectionOrLines

type SourceOpener = sourceOpener

var SplitLineEnding = splitLineEnding
var TextChanging = textChanging
var UpperCase = upperCase
var ViLastLine = viLastLine
var Within = within
var WordEnd = wordEnd
var WordLeft = wordLeft
var WordRight = wordRight
var WrittenOf = writtenOf

// Find-selection limits the editor actions read.
const MaxSearchSelection = maxSearchSelection

// ErrSearchTextTooLarge is the refusal of find_selection for a selection too
// large to be the search text of a large document.
var ErrSearchTextTooLarge = errSearchTextTooLarge

// More names the editor tests use.
const BulkEditMin = bulkEditMin
const ChangePayloadMax = changePayloadMax
const ChangeRingBytes = changeRingBytes
const ChangeRingRecords = changeRingRecords

var ClusterCells = clusterCells

const ClusterWholeLine = clusterWholeLine

var ComparePos = comparePos
var ErrFileGrew = errFileGrew
var FormatSize = formatSize

const LargeLimitsHint = largeLimitsHint
const LargeSampleSize = largeSampleSize

var LibraryPath = libraryPath

const MaxLargeCarets = maxLargeCarets
const MaxLargeLines = maxLargeLines
const MaxVirtualName = maxVirtualName

type MultiEditRecord = multiEditRecord

var Normalize = normalize

type ScanBudget = scanBudget

var Unwritable = unwritable

const WalkChunk = walkChunk
