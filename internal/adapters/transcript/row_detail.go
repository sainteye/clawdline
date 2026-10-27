package transcript

import (
	"bufio"
	"bytes"
	"io"
	"os"
)

// ReadRowEntries never searches another record or accepts a partial row. A
// caller verifies the projected content hash before exposing any candidate.
func ReadRowEntries(path, assistant string, offset int64) ([]Entry, error) {
	if offset < 0 {
		return nil, ErrCursorStale
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, recordError(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, recordError(err)
	}
	if offset >= info.Size() {
		return nil, ErrCursorStale
	}
	if offset > 0 {
		var previous [1]byte
		if _, err = f.ReadAt(previous[:], offset-1); err != nil || previous[0] != '\n' {
			return nil, ErrCursorStale
		}
	}
	reader := bufio.NewReader(io.NewSectionReader(f, offset, ReadBudget))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, ErrCursorStale
	}
	line = bytes.TrimSuffix(line, []byte{'\n'})
	var entries []Entry
	switch assistant {
	case "codex":
		entries = codexEntries(line)
	case "claude":
		entries = claudeEntries(line, false)
	default:
		return nil, ErrCursorStale
	}
	for i := range entries {
		entries[i].Before = offset
		entries[i].RowOrdinal = i
	}
	return entries, nil
}
