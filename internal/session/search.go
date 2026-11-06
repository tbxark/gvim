package session

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const searchFileName = "search.json"

// Search is the saved state of the find bar: the histories of the search and
// replacement fields, oldest first, and the option toggles.
type Search struct {
	Find    []string `json:"find,omitempty"`
	Replace []string `json:"replace,omitempty"`
	Case    bool     `json:"case,omitempty"`
	Word    bool     `json:"word,omitempty"`
	Regex   bool     `json:"regex,omitempty"`
	Confirm bool     `json:"confirm,omitempty"`
}

// SaveSearch writes s to dir/search.json. It is separate from the session, so a
// start with file arguments, which saves no session, still keeps the searches.
func SaveSearch(dir string, s Search) error {
	data, err := json.Marshal(s, jsontext.Multiline(true))
	if err != nil {
		return fmt.Errorf("session: encode: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := writeAtomic(dir, searchFileName, data); err != nil {
		return fmt.Errorf("session: write %s: %w", searchFileName, err)
	}
	return nil
}

// LoadSearch reads dir/search.json. A missing file returns false and no error.
func LoadSearch(dir string) (Search, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, searchFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return Search{}, false, nil
	}
	if err != nil {
		return Search{}, false, err
	}
	var s Search
	if err := json.Unmarshal(data, &s); err != nil {
		return Search{}, false, fmt.Errorf("session: decode %s: %w", searchFileName, err)
	}
	return s, true, nil
}
