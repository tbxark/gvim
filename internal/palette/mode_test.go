package palette

import "testing"

// TestParseQueryTable checks the mode of each query. A query without a mode
// character is a file query, and the text keeps its spaces and any later mode
// characters.
func TestParseQueryTable(t *testing.T) {
	tests := []struct {
		query string
		mode  Mode
		text  string
	}{
		{"", ModeFile, ""},
		{"file", ModeFile, "file"},
		{"src/main.go", ModeFile, "src/main.go"},
		{" >save", ModeFile, " >save"}, // a leading space is not a mode character
		{"a>b", ModeFile, "a>b"},
		{">", ModeCommand, ""},
		{">add cursor", ModeCommand, "add cursor"},
		{">>x", ModeCommand, ">x"},
		{":", ModeLine, ""},
		{":12:3", ModeLine, "12:3"},
		{"#", ModeBuffer, ""},
		{"#notes draft", ModeBuffer, "notes draft"},
	}
	for _, tc := range tests {
		mode, text := ParseQuery(tc.query)
		if mode != tc.mode || text != tc.text {
			t.Errorf("ParseQuery(%q) = %v, %q; want %v, %q", tc.query, mode, text, tc.mode, tc.text)
		}
	}
}

// TestSearchIgnoresNonCommandQueries checks that file, line and buffer queries
// get no command results, so the caller can route them, and that a ">" query
// still ranks commands.
func TestSearchIgnoresNonCommandQueries(t *testing.T) {
	entries := Commands(nil)
	for _, q := range []string{"", "save", " >save", ":3", "#save"} {
		if got := Search(entries, q); got != nil {
			t.Errorf("Search(%q) returned %d entries, want nil", q, len(got))
		}
	}
	if got := Search(entries, ">save"); len(got) == 0 {
		t.Fatal("Search(\">save\") returned no commands")
	}
}
