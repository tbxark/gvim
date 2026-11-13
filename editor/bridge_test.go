package editor

import "github.com/tbxark/gvim/document"

// The document names the editor tests use; production code does not.
const ScanBytesBudget = document.ScanBytesBudget
const ScanLinesCap = document.ScanLinesCap
const bulkEditMin = document.BulkEditMin
const changePayloadMax = document.ChangePayloadMax
const changeRingBytes = document.ChangeRingBytes
const changeRingRecords = document.ChangeRingRecords

var clusterCells = document.ClusterCells

const clusterWholeLine = document.ClusterWholeLine

var comparePos = document.ComparePos
var diskStateOf = document.DiskStateOf
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
