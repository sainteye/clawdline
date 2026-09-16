package transcript

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Usage is what one conversation has spent, as its own transcript records it.
//
// What this can and cannot see was measured rather than assumed, and the two
// assistants needed different readings for reasons that are properties of their
// files, not preferences:
//
// Codex keeps a running cumulative total on every `token_usage_record`, so the
// last one answers for the whole thread. That matters: a rollout file here is
// 918 MB, and the last line is reached in under a millisecond by reading
// backwards.
//
// Claude has no such total. It has a `cost-state` rollup with per-model figures
// and a dollar amount, which looked like the answer until five of the six
// largest transcripts on this machine turned out not to contain one at all —
// and the sixth's was written a fifth of the way in and never updated. So a
// field present in one file out of six, already stale in that one, is not a
// source. The per-message usage is, and it is summed forward.
//
// The limit that survives is stated rather than hidden: summing one
// conversation's own assistant messages cannot see work another model did on
// its behalf. In the one file that had both, a `cost-state` knew about Haiku
// and Sonnet usage that appears nowhere in that file's messages. So this is
// what the transcript accounts for, which is a smaller claim than what the
// session spent, and the contract says so in those words.
type Usage struct {
	SessionID string
	Assistant string
	Models    []ModelUsage
	Path      string
	// Messages is how many assistant turns were counted. Zero with no error
	// means the file exists and has no assistant turns yet, which is different
	// from a file that could not be read.
	Messages int64
}

// ModelUsage is one model's share. An empty Model means the record did not name
// one — Codex's cumulative total does not — and it is left blank rather than
// guessed from the session's current configuration, which can have changed
// partway through a thread.
type ModelUsage struct {
	Model         string
	InputTokens   int64
	OutputTokens  int64
	ThinkingTok   int64
	CacheReadTok  int64
	CacheWriteTok int64
}

// clone returns a Usage that shares nothing with this one.
func (u Usage) clone() Usage {
	out := u
	out.Models = append([]ModelUsage(nil), u.Models...)
	return out
}

func (u Usage) Total() int64 {
	var n int64
	for _, m := range u.Models {
		n += m.InputTokens + m.OutputTokens + m.CacheReadTok + m.CacheWriteTok
	}
	return n
}

// ClaudePath is where Claude Code keeps one conversation's record.
//
// The directory name is the working directory as ProjectSlug spells it. That
// is Claude's scheme, not ours, so it is reproduced as given.
func ClaudePath(home, cwd, conversationID string) string {
	return filepath.Join(home, ".claude", "projects", ProjectSlug(cwd), conversationID+".jsonl")
}

// CodexPath finds the rollout file for a thread.
//
// Codex files a session under the date it started and puts the id in the name,
// so there is nothing to compute: the tree is walked until the id is found.
func CodexPath(home, sessionID string) string {
	root := filepath.Join(home, ".codex", "sessions")
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, sessionID+".jsonl") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// ReadClaudeUsage sums what this conversation's own assistant turns used.
func ReadClaudeUsage(path string) (Usage, error) {
	u, _, err := readClaudeFrom(path, 0, Usage{Assistant: "claude", Path: path})
	return u, err
}

// readClaudeFrom adds everything after `from` to the totals it is given, and
// reports the offset it stopped at.
//
// Only whole lines are counted. A read that lands mid-line would either drop a
// turn or count a fragment, and both are silent: the number would simply be
// wrong, with nothing to notice. So the offset advances only past a newline.
func readClaudeFrom(path string, from int64, out Usage) (Usage, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return out, from, err
	}
	defer f.Close()
	if from > 0 {
		if _, err := f.Seek(from, io.SeekStart); err != nil {
			return out, from, err
		}
	}

	byModel := map[string]*ModelUsage{}
	order := []string{}
	for i := range out.Models {
		byModel[out.Models[i].Model] = &out.Models[i]
		order = append(order, out.Models[i].Model)
	}
	at := from

	sc := bufio.NewScanner(f)
	// A transcript line carries whole tool outputs, so the default 64 KiB limit
	// stops mid-file on any real session. Hitting it would look like a session
	// that went quiet rather than a reader that gave up.
	sc.Buffer(make([]byte, 0, 256<<10), 32<<20)
	for sc.Scan() {
		at += int64(len(sc.Bytes())) + 1
		var rec struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionId"`
			Message   struct {
				Model string `json:"model"`
				Usage struct {
					InputTokens         int64 `json:"input_tokens"`
					OutputTokens        int64 `json:"output_tokens"`
					CacheReadTokens     int64 `json:"cache_read_input_tokens"`
					CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
					OutputDetails       struct {
						ThinkingTokens int64 `json:"thinking_tokens"`
					} `json:"output_tokens_details"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		if rec.SessionID != "" && out.SessionID == "" {
			out.SessionID = rec.SessionID
		}
		if rec.Type != "assistant" {
			continue
		}
		u := rec.Message.Usage
		if u.InputTokens == 0 && u.OutputTokens == 0 && u.CacheReadTokens == 0 && u.CacheCreationTokens == 0 {
			continue
		}
		out.Messages++
		m, ok := byModel[rec.Message.Model]
		if !ok {
			m = &ModelUsage{Model: rec.Message.Model}
			byModel[rec.Message.Model] = m
			order = append(order, rec.Message.Model)
		}
		m.InputTokens += u.InputTokens
		m.OutputTokens += u.OutputTokens
		m.CacheReadTok += u.CacheReadTokens
		m.CacheWriteTok += u.CacheCreationTokens
		m.ThinkingTok += u.OutputDetails.ThinkingTokens
	}
	if err := sc.Err(); err != nil {
		return out, from, err
	}
	out.Models = out.Models[:0]
	for _, k := range order {
		out.Models = append(out.Models, *byModel[k])
	}
	return out, at, nil
}

// ReadCodexUsage reads Codex's cumulative thread total from the end of the file.
func ReadCodexUsage(path string) (Usage, error) {
	line, err := LastLineWith(path, []byte(`"type":"token_usage_record"`), 0)
	if err != nil {
		return Usage{}, err
	}
	var rec struct {
		Payload struct {
			SessionID string `json:"session_id"`
			Thread    struct {
				InputTokens     int64 `json:"input_tokens"`
				CachedInput     int64 `json:"cached_input_tokens"`
				CacheWriteInput int64 `json:"cache_write_input_tokens"`
				OutputTokens    int64 `json:"output_tokens"`
				ReasoningOutput int64 `json:"reasoning_output_tokens"`
			} `json:"thread_token_usage"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &rec); err != nil {
		return Usage{}, err
	}
	t := rec.Payload.Thread
	return Usage{
		SessionID: rec.Payload.SessionID,
		Assistant: "codex",
		Path:      path,
		Models: []ModelUsage{{
			InputTokens:   t.InputTokens,
			OutputTokens:  t.OutputTokens,
			ThinkingTok:   t.ReasoningOutput,
			CacheReadTok:  t.CachedInput,
			CacheWriteTok: t.CacheWriteInput,
		}},
	}, nil
}
