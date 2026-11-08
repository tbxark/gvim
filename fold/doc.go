// Package fold computes indentation fold ranges over the lines of a document.
// A Range is inclusive and ends before the first later line that is not indented
// deeper than its opener; indentation is measured with textlayout.IndentWidth.
package fold
