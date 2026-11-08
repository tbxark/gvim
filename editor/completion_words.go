package editor

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Buffer-word completion: the words already in the buffer, offered when the user
// triggers completion without a language server (and after the server's own
// results when one is running). This is the editor half of Fresh's buffer_words
// provider (services/completion/buffer_words.rs): a bounded snapshot of lines
// around the caret is captured by the caller (the TUI, on its update goroutine)
// and scored here without touching the document again.
//
// The provider is deliberately pure and TUI-free: it never reads a file, starts a
// process or mutates anything, so the caller can run it off the update loop.

const (
	// WordCompletionLimit is the most candidates one scan returns (Fresh's 40).
	WordCompletionLimit = 40
	// WordCompletionDetail labels the candidates in the completion popup.
	WordCompletionDetail = "Buffer Words"
	// WordCompletionMinRunes is the shortest word offered (Fresh's 2 graphemes; a
	// rune is the editor's unit here).
	WordCompletionMinRunes = 2
	// WordScanBytes is the scan window around the caret for normal documents.
	WordScanBytes = 512 << 10
	// WordScanLargeBytes is the smaller window for a large file.
	WordScanLargeBytes = 32 << 10
)

// WordSnapshot is the completion scan input: a contiguous line window around the
// caret, captured on the update goroutine. Lines[0] is buffer line StartLine.
type WordSnapshot struct {
	Lines          []string
	StartLine      int // absolute buffer line of Lines[0]
	CursorLine     int // absolute buffer line of the caret
	CursorCol      int // rune column of the caret in the cursor line
	Prefix         string
	WordChars      string // languages.<id>.word_characters: extra identifier runes
	PrefixHasUpper bool   // smart case: an uppercase prefix matches case-sensitively
	ViewportTop    int    // first visible buffer line
	ViewportBottom int    // last visible buffer line, inclusive
}

// WordCandidate is one buffer-word completion.
type WordCandidate struct {
	Text   string
	Detail string
	Score  int
}

// IsWordRune reports whether r is part of an identifier word: a letter, digit or
// underscore, or one of the language's extra word characters.
func IsWordRune(r rune, extra string) bool {
	if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}
	for _, c := range extra {
		if c == r {
			return true
		}
	}
	return false
}

// CompletionPrefix returns the word before the caret: the runes of line ending at
// col that are word runes. A col past the end is clamped. It is the prefix the
// buffer-word scan matches, and the text a completion replaces.
func CompletionPrefix(line string, col int, wordChars string) string {
	runes := []rune(line)
	if col < 0 {
		return ""
	}
	if col > len(runes) {
		col = len(runes)
	}
	start := col
	for start > 0 && IsWordRune(runes[start-1], wordChars) {
		start--
	}
	return string(runes[start:col])
}

// HasUpper reports whether text contains an uppercase rune (smart-case trigger).
func HasUpper(text string) bool {
	for _, r := range text {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// wordStat accumulates one lowercased word's occurrences in the snapshot.
type wordStat struct {
	text        string // the casing of the first occurrence
	count       int
	nearestLine int
	nearestCol  int
	nearestDist int
	inViewport  bool
}

// BufferWordCompletions scores the words in the snapshot that start with the
// caret's prefix: proximity to the caret first, then visible-on-screen, then
// frequency, with a bonus for matching the prefix's case. The word being typed is
// not offered. It returns at most WordCompletionLimit candidates, best first.
func BufferWordCompletions(s WordSnapshot) []WordCandidate {
	prefix := s.Prefix
	if prefix == "" || len(s.Lines) == 0 {
		return nil
	}
	lowerPrefix := strings.ToLower(prefix)
	stats := make(map[string]*wordStat)
	for i, line := range s.Lines {
		lineNo := s.StartLine + i
		runes := []rune(line)
		for col := 0; col < len(runes); {
			if !IsWordRune(runes[col], s.WordChars) {
				col++
				continue
			}
			start := col
			for col < len(runes) && IsWordRune(runes[col], s.WordChars) {
				col++
			}
			word := string(runes[start:col])
			if !wordMatchesPrefix(word, prefix, lowerPrefix, s.PrefixHasUpper) {
				continue
			}
			key := strings.ToLower(word)
			if key == lowerPrefix || utf8.RuneCountInString(word) < WordCompletionMinRunes {
				continue
			}
			dist := lineDistance(lineNo, s.CursorLine)*1000 + absInt(start-s.CursorCol)
			stat := stats[key]
			if stat == nil {
				stat = &wordStat{text: word, nearestLine: lineNo, nearestCol: start, nearestDist: dist}
				stats[key] = stat
			} else if dist < stat.nearestDist {
				stat.nearestDist = dist
				stat.nearestLine = lineNo
				stat.nearestCol = start
			}
			stat.count++
			if lineNo >= s.ViewportTop && lineNo <= s.ViewportBottom {
				stat.inViewport = true
			}
		}
	}
	candidates := make([]WordCandidate, 0, len(stats))
	for _, stat := range stats {
		score := 500_000 - stat.nearestDist
		if stat.inViewport {
			score += 100_000
		}
		if stat.count > 1 {
			score += 5_000 * (min(stat.count, 10) - 1)
		}
		if strings.HasPrefix(stat.text, prefix) {
			score += 2_000 // the prefix's own casing: the likeliest continuation
		}
		candidates = append(candidates, WordCandidate{
			Text: stat.text, Detail: WordCompletionDetail, Score: score,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		li, lj := strings.ToLower(candidates[i].Text), strings.ToLower(candidates[j].Text)
		if li != lj {
			return li < lj
		}
		return candidates[i].Text < candidates[j].Text
	})
	if len(candidates) > WordCompletionLimit {
		candidates = candidates[:WordCompletionLimit]
	}
	return candidates
}

// wordMatchesPrefix applies Fresh's smart case: an uppercase prefix must match
// case-sensitively; otherwise the match is case-insensitive (the score then favors
// the exact casing).
func wordMatchesPrefix(word, prefix, lowerPrefix string, prefixHasUpper bool) bool {
	if prefixHasUpper {
		return strings.HasPrefix(word, prefix)
	}
	return strings.HasPrefix(strings.ToLower(word), lowerPrefix)
}

// lineDistance is the absolute difference of two buffer line numbers.
func lineDistance(a, b int) int {
	if a < b {
		return b - a
	}
	return a - b
}

// absInt returns the absolute value of n.
func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
