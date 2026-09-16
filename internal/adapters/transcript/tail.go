package transcript

import (
	"bytes"
	"errors"
	"io"
	"os"
)

// DefaultTailBudget is how far back a tail read is willing to look.
//
// It is a budget rather than a line count because the files differ by three
// orders of magnitude: a Claude transcript here is 11 MB and a Codex rollout is
// 918 MB. Reading either from the front to answer "what happened most recently"
// would be absurd, and a line count cannot bound the work when one line can be
// a megabyte of instructions.
const DefaultTailBudget = 4 << 20 // 4 MiB

// chunk is how much is read per step backwards. Large enough that a typical
// answer needs one read, small enough that a miss is cheap.
const chunk = 256 << 10

// ErrNotFound means the budget was spent without finding what was asked for.
//
// It is deliberately distinct from "the file is not there" and from "the file
// has no such record". A caller that cannot tell those apart will report a
// session with an unusually long tail as a session with no usage at all.
var ErrNotFound = errors.New("not within the tail budget")

// LastLineWith returns the last line of a file that contains needle.
//
// The file is read backwards in chunks, so the cost is set by where the answer
// is rather than by how large the file is. Both assistants put their running
// totals near the end — Claude a `cost-state` rollup, Codex a cumulative
// `token_usage_record` — which is why this is enough to answer for either.
func LastLineWith(path string, needle []byte, budget int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	if budget <= 0 {
		budget = DefaultTailBudget
	}

	// tail holds what has been read so far, always ending at the file's end, so
	// a line split across two chunk boundaries is rejoined rather than lost.
	var tail []byte
	pos := size
	for size-pos < budget && pos > 0 {
		step := int64(chunk)
		if pos < step {
			step = pos
		}
		pos -= step
		buf := make([]byte, step)
		if _, err := f.ReadAt(buf, pos); err != nil && err != io.EOF {
			return nil, err
		}
		tail = append(buf, tail...)

		// Only complete lines are considered. The first line of `tail` may be a
		// fragment until the next step brings its beginning, so it is skipped
		// unless this read reached the start of the file.
		lines := bytes.Split(tail, []byte{'\n'})
		first := 1
		if pos == 0 {
			first = 0
		}
		for i := len(lines) - 1; i >= first; i-- {
			if bytes.Contains(lines[i], needle) {
				return lines[i], nil
			}
		}
	}
	return nil, ErrNotFound
}

// LastLines returns up to n complete lines from the end of a file, oldest
// first, skipping any that are empty.
func LastLines(path string, n int, budget int64) ([][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	if budget <= 0 {
		budget = DefaultTailBudget
	}

	var tail []byte
	pos := size
	for size-pos < budget && pos > 0 {
		step := int64(chunk)
		if pos < step {
			step = pos
		}
		pos -= step
		buf := make([]byte, step)
		if _, err := f.ReadAt(buf, pos); err != nil && err != io.EOF {
			return nil, err
		}
		tail = append(buf, tail...)

		lines := completeLines(tail, pos == 0)
		if len(lines) >= n || pos == 0 {
			if len(lines) > n {
				lines = lines[len(lines)-n:]
			}
			return lines, nil
		}
	}
	return completeLines(tail, pos == 0), nil
}

func completeLines(tail []byte, fromStart bool) [][]byte {
	parts := bytes.Split(tail, []byte{'\n'})
	if !fromStart && len(parts) > 0 {
		parts = parts[1:]
	}
	out := make([][]byte, 0, len(parts))
	for _, p := range parts {
		if len(bytes.TrimSpace(p)) > 0 {
			out = append(out, p)
		}
	}
	return out
}
