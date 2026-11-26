// Package fold computes indentation fold ranges over the lines of a document.
// A Range is inclusive and ends before the first later line that is not indented
// deeper than its opener; the caller measures indentation (textlayout.LineIndent).
package fold
