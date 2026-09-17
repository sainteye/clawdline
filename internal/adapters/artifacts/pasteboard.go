package artifacts

import (
	"encoding/json"
	"errors"
)

// ErrPasteboardUnsupported is what a platform with no pasteboard this daemon
// can drive answers. The send path treats it as "hand the picture over as a
// path", which is what the Swift app does whenever pasting is not possible.
var ErrPasteboardUnsupported = errors.New("this platform has no pasteboard Clawdline can lend a picture to")

// Borrowed is the pasteboard as it was before a send took it, by value — the
// bytes of every item, not references to them, because clearing the pasteboard
// invalidates every item that was on it and a snapshot of references would
// restore nothing while looking as though it had.
type Borrowed struct {
	items json.RawMessage
	// change is the pasteboard's change count after this daemon's own last
	// write. A different count at give-back time means somebody copied
	// something in between, and what they copied is not overwritten.
	change int64
}
