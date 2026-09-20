package process

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
)

// RolloutHead answers which conversation a Codex rollout belongs to: the
// thread tree's own id, and this file's thread within it. `ok` is false when
// the file could not be read or does not start with a `session_meta` record.
//
// The kernel names the file; this says what is in it. Nothing else is read —
// not one line past the head, and never the conversation itself.
type RolloutHead func(path string) (conversation, thread string, ok bool)

// readRolloutHead is that reader against the filesystem.
//
// Measured over the 1,195 rollouts on this Mac on 2026-09-20: every one of
// them starts with a `session_meta` record carrying both `session_id` and
// `id`, the file's name always carries `id`, and `session_id == id` on exactly
// the 781 that are not sub-threads. So `session_id` is the conversation and
// `id` is one thread of it — which is the whole reason this file is read at
// all rather than the name being parsed (openfiles.go).
func readRolloutHead(path string) (string, string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	// The head is one line, and one line of a rollout carries a whole tool
	// result: the reader is sized for the ordinary case and the limit is what
	// keeps a pathological first line from being pulled into memory whole.
	head, err := bufio.NewReaderSize(io.LimitReader(f, headLimit), 64<<10).ReadBytes('\n')
	f.Close()
	if err != nil && len(head) == 0 {
		return "", "", false
	}
	var meta struct {
		Type    string `json:"type"`
		Payload struct {
			SessionID string `json:"session_id"`
			ID        string `json:"id"`
		} `json:"payload"`
	}
	if json.Unmarshal(head, &meta) != nil || meta.Type != "session_meta" {
		return "", "", false
	}
	if meta.Payload.SessionID == "" && meta.Payload.ID == "" {
		return "", "", false
	}
	return meta.Payload.SessionID, meta.Payload.ID, true
}

// headLimit bounds the first line this will read before giving up on it.
const headLimit = 1 << 20
