package process

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"sort"
)

// RolloutHead answers what a Codex rollout's head record says about itself.
// `ok` is false when the file could not be read or does not start with a
// `session_meta` record.
//
// The kernel names the file; this says what is in it. Nothing else is read —
// not one line past the head, and never the conversation itself.
type RolloutHead func(path string) (RolloutMeta, bool)

// RolloutMeta is the part of that record this machine has a use for.
//
// It is one value rather than a widening list of results because the head is
// opened once and decoded once: the first line is already in memory and the
// decoder already walks the whole object, so a field taken from it costs a
// string and no I/O at all, while the same field fetched later would cost a
// second open of the same file. Anything the session list needs out of a
// rollout belongs here for that reason, and a reader that wants more is a
// field, never a second read.
type RolloutMeta struct {
	// Conversation is `session_id`: the thread tree this file belongs to.
	Conversation string
	// Thread is `id`: this file's own thread within that tree. A thread that
	// started the conversation has the two equal.
	Thread string
	// CWD is `cwd`: the directory the thread was started in, as Codex wrote
	// it. It is empty when the record did not carry one, and empty is read as
	// "this did not say", never as a directory.
	CWD string
	// ThreadSource is Codex's positive classification of this rollout. Missing
	// does not mean subagent: old records predate the field, and absence is only
	// absence (retired Codex.swift:94-101).
	ThreadSource string
	// AgentType is the stable key/value Codex put below source.subagent, when
	// it wrote one. It is a provider word, not a description inferred here.
	AgentType string
}

// readRolloutHead is that reader against the filesystem.
//
// Measured over the 1,195 rollouts on this Mac on 2026-09-20: every one of
// them starts with a `session_meta` record carrying both `session_id` and
// `id`, the file's name always carries `id`, and `session_id == id` on exactly
// the 781 that are not sub-threads. So `session_id` is the conversation and
// `id` is one thread of it — which is the whole reason this file is read at
// all rather than the name being parsed (openfiles.go).
//
// Read again over the 1,198 there on 2026-09-20 after the sessions of that
// day: all 1,198 also carry `cwd`, and all 1,198 of those are absolute. The
// head line they come out of is 18.5 KB at its shortest and 48.9 KB at its
// longest, which this already reads and already decodes whole — so the
// directory is a field on a record this machine had in its hands, not a
// reading anybody now pays for.
func readRolloutHead(path string) (RolloutMeta, bool) {
	f, err := os.Open(path)
	if err != nil {
		return RolloutMeta{}, false
	}
	// The head is one line, and one line of a rollout carries a whole tool
	// result: the reader is sized for the ordinary case and the limit is what
	// keeps a pathological first line from being pulled into memory whole.
	head, err := bufio.NewReaderSize(io.LimitReader(f, headLimit), 64<<10).ReadBytes('\n')
	f.Close()
	if err != nil && len(head) == 0 {
		return RolloutMeta{}, false
	}
	var meta struct {
		Type    string `json:"type"`
		Payload struct {
			SessionID    string `json:"session_id"`
			ID           string `json:"id"`
			CWD          string `json:"cwd"`
			ThreadSource string `json:"thread_source"`
			Source       any    `json:"source"`
		} `json:"payload"`
	}
	if json.Unmarshal(head, &meta) != nil || meta.Type != "session_meta" {
		return RolloutMeta{}, false
	}
	if meta.Payload.SessionID == "" && meta.Payload.ID == "" {
		return RolloutMeta{}, false
	}
	agentType := ""
	var subagent map[string]any
	if source, ok := meta.Payload.Source.(map[string]any); ok {
		subagent, _ = source["subagent"].(map[string]any)
	}
	if len(subagent) > 0 {
		keys := make([]string, 0, len(subagent))
		for key := range subagent {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if value, ok := subagent[key].(string); ok && value != "" {
				agentType = value
				break
			}
			if agentType == "" {
				agentType = key
			}
		}
	}
	return RolloutMeta{
		Conversation: meta.Payload.SessionID,
		Thread:       meta.Payload.ID,
		CWD:          meta.Payload.CWD,
		ThreadSource: meta.Payload.ThreadSource,
		AgentType:    agentType,
	}, true
}

// headLimit bounds the first line this will read before giving up on it.
const headLimit = 1 << 20
