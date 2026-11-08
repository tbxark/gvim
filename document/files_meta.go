package document

import (
	"github.com/tbxark/gvim/encoding"
)

// markMetaChanged records that the encoding or the line ending of d changed, when
// changed is true. The text stays as it is, but the file will differ, so the
// document counts as modified until it is saved or reloaded. A read-only or binary
// document is never saved, so the change would leave it modified with no way to
// clear it; the choice still applies to a save_as.
func (d *Document) MarkMetaChanged(changed bool) {
	if changed && !d.readOnly && !d.binary {
		d.MetaDirty = true
	}
}

// EncodingChoice is an encoding a document can be read or saved with: Name is what
// set_encoding and reload_with_encoding take, and Label is how Encoding shows it.
type EncodingChoice struct {
	Name, Label string
}

// EncodingChoices lists the encodings of internal/encoding, with their labels.
func EncodingChoices() []EncodingChoice {
	names := encoding.Names()
	choices := make([]EncodingChoice, len(names))
	for i, n := range names {
		label := labels[n]
		if n == defaultCodec {
			label = "UTF-8"
		}
		choices[i] = EncodingChoice{Name: n, Label: label}
	}
	return choices
}
