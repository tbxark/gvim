package editor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"testing"
)

// materializeAllowed lists the functions of the editor and document packages that may
// call Buffer.Text or Buffer.Range, as "file.go:function". Every other call of them in
// those packages must use TextChecked, RangeChecked, TextPrefix or one of the helpers
// built on them (Document.ReadRange, planRange, contentMark), because in a large
// document Text and Range over 16 MiB return nothing, and an action that goes on with
// nothing loses or rewrites text. The reason is given with each entry; a new entry is
// a decision that the call cannot reach a large document, or reads a bounded range.
var materializeAllowed = map[string]string{
	// The small branch of the helpers that choose by Large.
	"large_guard.go:ReadRange":   "memory documents only; a large document uses RangeChecked",
	"large_guard.go:ContentMark": "memory documents only; a large document is marked by its version",
	"encoding.go:TextSample":     "memory documents only; a large document uses TextPrefix",
	// Reached only after the action refused a large document.
	"cursors_match.go:AddNextMatch": "large documents return ErrLargeFile first",
	"findsel.go:findSelection":      "large documents return ErrLargeFile first",
	"doc_format.go:beginFormat":     "large documents return ErrLargeFile first",
	"doc_revert.go:ApplyRevert":     "large documents are swapped or refused first",
	"doc_revert.go:RunRevert":       "only a document that is not large has its text read",
	"autosave_async.go:PlanSave":    "large documents take planSaveLarge first",
	// Ranges inside one line, which a large document reads whole at most up to its cap.
	"dabbrev.go:Shows":              "a word on one line",
	"dabbrev.go:Begin":              "a word on one line",
	"smart_lines.go:TransposeChars": "two characters of one line",
	// The application and its restore host.
	"files_recovery.go:Plan":  "skips large documents",
	"session.go:WriteSession": "skips large documents",
}

// TestNoUncheckedMaterialize walks the packages that hold the actions and finds every
// call of Buffer.Text and Buffer.Range, and fails for one that is not in the list above.
// It is the guard against a whole-document read that a large document would silently
// turn into an empty string. The restore host is scanned too: it reads the unsaved text
// of the tabs when it saves a session or a crash recovery.
func TestNoUncheckedMaterialize(t *testing.T) {
	t.Parallel()
	used := map[string]bool{}
	for _, dir := range []string{".", filepath.Join("..", "document"), filepath.Join("..", "internal", "app"), filepath.Join("..", "internal", "restore")} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				key := filepath.Base(path) + ":" + fn.Name.Name
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || !isBufferRead(sel, len(call.Args)) {
						return true
					}
					if _, allowed := materializeAllowed[key]; allowed {
						used[key] = true
						return true
					}
					t.Errorf("%s: %s.%s(...) reads the whole text or a range without a check; "+
						"use TextChecked, RangeChecked, TextPrefix or Document.ReadRange, "+
						"or list the function in materializeAllowed with the reason",
						fset.Position(call.Pos()), types.ExprString(sel.X), sel.Sel.Name)
					return true
				})
			}
		}
	}
	for key := range materializeAllowed {
		if !used[key] {
			t.Errorf("materializeAllowed lists %s, which has no such call: remove it", key)
		}
	}
}

// isBufferRead reports whether sel is a call of Text with no argument or Range with two
// on something that is a buffer by its name: a field or variable called Buffer, or a
// variable b, buf or buffer.
func isBufferRead(sel *ast.SelectorExpr, args int) bool {
	switch {
	case sel.Sel.Name == "Text" && args == 0, sel.Sel.Name == "Range" && args == 2:
	default:
		return false
	}
	recv := types.ExprString(sel.X)
	if recv == "b" || recv == "buf" || recv == "buffer" {
		return true
	}
	return strings.HasSuffix(recv, "Buffer")
}
