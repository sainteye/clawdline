// Package board is the project board's domain: what is being worked on, and the
// rules that keep two writers from silently overwriting each other.
package board

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// Project is one place work happens. It is derived from what is on the machine
// rather than declared: a directory somebody is running an assistant in is a
// project, and one nobody is in has stopped being one.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayPath string `json:"displayPath"`
	// SessionCount is how many sessions are in it right now. It is a reading,
	// not a stored count, so it cannot drift from the thing it describes.
	SessionCount int `json:"sessionCount"`
}

// DeriveProjects turns a set of working directories into the board's projects.
func DeriveProjects(cwds []string) []Project {
	counts := map[string]int{}
	for _, cwd := range cwds {
		if cwd == "" {
			continue
		}
		counts[cwd]++
	}
	out := make([]Project, 0, len(counts))
	for path, n := range counts {
		out = append(out, Project{
			ID:           ID("project", path),
			Name:         name(path),
			DisplayPath:  path,
			SessionCount: n,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DisplayPath < out[j].DisplayPath })
	return out
}

func name(path string) string {
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	return parts[len(parts)-1]
}

// ID is a stable identity derived from a path, so the same directory is the
// same project across restarts without a stored mapping to go out of date.
func ID(prefix, path string) string {
	sum := sha256.Sum256([]byte(path))
	return prefix + "-" + hex.EncodeToString(sum[:])[:24]
}

// Command is a request to change the board.
type Command struct {
	Operation        string          `json:"operation"`
	RequestID        string          `json:"requestId"`
	ExpectedRevision int64           `json:"expectedRevision"`
	Actor            string          `json:"actor"`
	Body             json.RawMessage `json:"body,omitempty"`
}

// Fingerprint identifies a command by what it asks for, not by when it was
// asked. Two sends of the same request are the same command; the same id
// carrying different words is a different one wearing a used name.
func (c Command) Fingerprint() string {
	h := sha256.New()
	h.Write([]byte(c.Operation))
	h.Write([]byte{0})
	h.Write([]byte(c.Actor))
	h.Write([]byte{0})
	h.Write(c.Body)
	return hex.EncodeToString(h.Sum(nil))
}

// Refusal is a typed reason a command was not applied.
type Refusal struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (r Refusal) Error() string { return r.Code + ": " + r.Detail }

// Decide applies the two rules that let several writers share one board.
//
//   - A command is compared against the revision its author last saw. If the
//     board has moved, the command is refused rather than applied to a state
//     nobody wrote it for. This is the difference between "I am editing what I
//     read" and "I am overwriting whatever is there".
//   - A request id already seen replays its original outcome when it carries
//     the same words, and conflicts when it does not. Retries are then free,
//     and a reused id cannot smuggle a second change past the first one's
//     receipt.
func Decide(current int64, seen map[string]string, c Command) (applied bool, err error) {
	if c.RequestID == "" {
		return false, Refusal{"bad_request", "a board command needs a request id"}
	}
	if c.Operation == "" {
		return false, Refusal{"bad_request", "a board command needs an operation"}
	}
	if prior, ok := seen[c.RequestID]; ok {
		if prior == c.Fingerprint() {
			// The same command arriving twice changes nothing twice.
			return false, nil
		}
		return false, Refusal{"request_conflict",
			"that request id was already used for a different command"}
	}
	if c.ExpectedRevision != current {
		return false, Refusal{"revision_conflict",
			"the board has moved since you read it"}
	}
	return true, nil
}
