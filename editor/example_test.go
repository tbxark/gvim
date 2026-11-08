package editor_test

import (
	"fmt"

	"github.com/tbxark/gvim/editor"
)

func ExampleNew() {
	e := editor.New(editor.Options{Rows: 10, Cols: 40})
	fmt.Println(e.Active().Name(), len(e.Tabs()))
	// Output: [No Name] 1
}

func ExampleEditor_Do() {
	e := editor.New(editor.Options{})
	e.Do("insert_char", map[string]any{"text": "hi"})
	e.Do("insert_newline", nil)
	e.Do("insert_char", map[string]any{"text": "there"})
	e.Do("move_document_start", nil)
	e.Do("move_word_end", nil)
	fmt.Printf("%q cursor=%+v\n", e.Active().Buffer.Text(), e.Active().Cursor)
	// Output: "hi\nthere" cursor={Line:0 Col:2}
}
