package capacity

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// OverrideEnv names the variable a daemon reads its overrides from:
//
//	CLAWDLINE_NEXT_CAPACITY=audit.security=4KiB,store.db=200KiB,board.receipts=4
//
// It exists so a full row can be made on purpose — in a test, on a
// throwaway daemon — and seen to say so (limits §4.7).
const OverrideEnv = "CLAWDLINE_NEXT_CAPACITY"

var sizePattern = regexp.MustCompile(`^([0-9]+)(B|KiB|MiB|GiB)?$`)

// Resolve is the register with spec's overrides applied.
//
// **An override may only lower a limit.** One that would raise it — or names
// no row, or cannot be read, or is not positive — is refused: the default
// stands, and the refusal is in the answer for the caller to log and publish.
// A limit raised to quiet an alarm is exactly the alarm nobody hears, and that
// includes one raised by us.
func Resolve(entries []Entry, spec string) ([]Resolved, []string) {
	out := make([]Resolved, len(entries))
	index := map[string]int{}
	for i, e := range entries {
		out[i] = Resolved{Entry: e, Limit: e.Limit}
		index[e.Name] = i
	}
	var problems []string
	seen := map[string]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, "=")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || name == "" || value == "" {
			problems = append(problems, fmt.Sprintf("%q is not name=limit", part))
			continue
		}
		i, known := index[name]
		if !known {
			problems = append(problems, fmt.Sprintf("%s is not a registered row", name))
			continue
		}
		if seen[name] {
			problems = append(problems, fmt.Sprintf("%s is overridden twice; the first stands", name))
			continue
		}
		seen[name] = true
		limit, err := parseLimit(value, entries[i].Unit)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s=%s: %v", name, value, err))
			continue
		}
		if limit > entries[i].Limit {
			problems = append(problems, fmt.Sprintf("%s=%s would raise the limit above %d; an override may only lower it", name, value, entries[i].Limit))
			continue
		}
		out[i].Limit = limit
		out[i].Overridden = limit != entries[i].Limit
	}
	return out, problems
}

// parseLimit reads a positive count, with a binary size suffix when the row
// counts bytes.
func parseLimit(v string, unit Unit) (int64, error) {
	m := sizePattern.FindStringSubmatch(v)
	if m == nil {
		return 0, fmt.Errorf("not a count or a size such as 4KiB")
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, err
	}
	scale := int64(1)
	switch m[2] {
	case "KiB":
		scale = 1 << 10
	case "MiB":
		scale = 1 << 20
	case "GiB":
		scale = 1 << 30
	}
	if m[2] != "" && unit != Bytes {
		return 0, fmt.Errorf("this row counts %s, not bytes", unit)
	}
	if n <= 0 {
		return 0, fmt.Errorf("a limit must be positive")
	}
	if n > math.MaxInt64/scale {
		return 0, fmt.Errorf("too large")
	}
	return n * scale, nil
}

// Limit is the limit of name in resolved, and 0 when it is not there.
func Limit(resolved []Resolved, name string) int64 {
	for _, r := range resolved {
		if r.Entry.Name == name {
			return r.Limit
		}
	}
	return 0
}
