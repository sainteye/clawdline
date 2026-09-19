package transcript

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
)

// recordReadLimit is as much of an assistant's record as one info read looks
// at. The Swift app's `SessionInfo.recordReadLimit`: a request's cost must not
// grow with the lifetime size of the conversation it is about.
const recordReadLimit = 8 << 20

// Summary is one conversation's cumulative spend, in the Swift app's
// `Orchestrator.Usage` shape.
type Summary struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	Total      int64
	// Model is the model the last counted turn named, as written — which can
	// be `<synthetic>` for a turn the provider refused. Empty when none did.
	Model string
	// Cost is what the tokens would cost at list price, when the model has one.
	Cost    float64
	HasCost bool
}

// Facts is what an info read learns from the record itself.
type Facts struct {
	// Usage is absent when there is no honest total: a Claude transcript
	// larger than the read limit, or a record with no counted turn at all.
	Usage *Summary
	// Model is what the session is on now, or empty when nothing said.
	Model string
}

// RecordFacts reads the model and the spend out of an assistant's own record,
// once per version of the file.
//
// The same keying as the Swift app's `SessionInfo.recordFacts`: the file's
// size and modification time. An append changes the key, so a cached answer is
// a saved read and never a stale one.
type RecordFacts struct {
	mu    sync.Mutex
	held  map[factsKey]Facts
	order []factsKey
}

type factsKey struct {
	path      string
	assistant string
	size      int64
	mod       int64
}

const factsKept = 24

func NewRecordFacts() *RecordFacts { return &RecordFacts{held: map[factsKey]Facts{}} }

// Read answers for a record at path, written by assistant ("claude" or
// "codex").
func (r *RecordFacts) Read(path, assistant string) (Facts, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Facts{}, recordError(err)
	}
	key := factsKey{path: path, assistant: assistant, size: st.Size(), mod: st.ModTime().UnixNano()}
	r.mu.Lock()
	if hit, ok := r.held[key]; ok {
		r.touch(key)
		r.mu.Unlock()
		return hit, nil
	}
	r.mu.Unlock()

	data, complete, err := tailData(path, recordReadLimit)
	if err != nil {
		return Facts{}, recordError(err)
	}
	var facts Facts
	if assistant == "codex" {
		facts = codexFacts(data)
	} else {
		facts = claudeFacts(data, complete)
	}

	r.mu.Lock()
	r.held[key] = facts
	r.touch(key)
	for len(r.order) > factsKept {
		delete(r.held, r.order[0])
		r.order = r.order[1:]
	}
	r.mu.Unlock()
	return facts, nil
}

func (r *RecordFacts) touch(key factsKey) {
	for i, k := range r.order {
		if k == key {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	r.order = append(r.order, key)
}

// tailData is the last `limit` bytes of a file, starting at a whole line, and
// whether that was the whole file.
func tailData(path string, limit int64) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, false, err
	}
	start := int64(0)
	if size > limit {
		start = size - limit
	}
	data := make([]byte, size-start)
	if _, err := f.ReadAt(data, start); err != nil && err != io.EOF {
		return nil, false, err
	}
	if start > 0 {
		// The seek almost certainly landed inside a line.
		if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
			data = data[nl+1:]
		} else {
			data = nil
		}
	}
	return data, start == 0, nil
}

// reversedLines calls fn on each line of data from the last to the first,
// until fn says stop.
func reversedLines(data []byte, fn func(line []byte) (stop bool)) {
	end := len(data)
	for {
		start := bytes.LastIndexByte(data[:end], '\n') + 1
		if fn(data[start:end]) || start == 0 {
			return
		}
		end = start - 1
	}
}

// ---------- Claude ----------

// claudeFacts is `SessionInfo.recordFacts` for Claude: the model the file last
// named, and the whole conversation's usage only when the whole file was read.
// Per-turn counters are not cumulative, so a partial sum would be a smaller
// number labelled as the total.
func claudeFacts(data []byte, complete bool) Facts {
	out := Facts{Model: claudeStatedModel(data)}
	if complete {
		out.Usage = claudeSummary(data)
	}
	if out.Model == "" && out.Usage != nil && !strings.HasPrefix(out.Usage.Model, "<") {
		out.Model = out.Usage.Model
	}
	return out
}

// claudeSummary sums every assistant record's `message.usage`, as
// `Orchestrator.claudeUsage` does — every record, sidechains included, and the
// model is whatever the last of them named.
func claudeSummary(data []byte) *Summary {
	var u Summary
	found := false
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if !bytes.Contains(line, []byte(`"usage"`)) {
			continue
		}
		rec, ok := decodeObject(line)
		if !ok {
			continue
		}
		if t, _ := rec.str("type"); t != "assistant" {
			continue
		}
		message, ok := rec.object("message")
		if !ok {
			continue
		}
		counts, ok := message.object("usage")
		if !ok {
			continue
		}
		found = true
		u.Input += intOrZero(counts, "input_tokens")
		u.Output += intOrZero(counts, "output_tokens")
		u.CacheRead += intOrZero(counts, "cache_read_input_tokens")
		u.CacheWrite += intOrZero(counts, "cache_creation_input_tokens")
		if model, ok := message.str("model"); ok {
			u.Model = model
		}
	}
	if !found {
		return nil
	}
	u.Total = u.Input + u.Output + u.CacheRead + u.CacheWrite
	u.Cost, u.HasCost = Cost(u)
	return &u
}

func intOrZero(o object, key string) int64 {
	n, _ := o.integer(key)
	return n
}

// claudeStatedModel is the newest thing the transcript says about the model,
// read from the end: the last assistant turn's model, or a `/model` switch that
// came after it. `SessionInfo.claudeLimits(transcript:)`'s model half.
//
// A `/model` row and the `Set model to …` line it printed are both user rows,
// and the printed one comes after the command in the file — so, reading
// backwards, it is met first and held until its command turns up.
func claudeStatedModel(data []byte) string {
	model := ""
	printed := ""
	reversedLines(data, func(line []byte) bool {
		if !bytes.Contains(line, []byte(`"model"`)) && !bytes.Contains(line, []byte("/model")) &&
			!bytes.Contains(line, []byte("Set model to")) {
			return false
		}
		rec, ok := decodeObject(line)
		if !ok {
			return false
		}
		kind, _ := rec.str("type")
		if kind == "assistant" {
			if message, ok := rec.object("message"); ok {
				if named, ok := message.str("model"); ok && named != "" && !strings.HasPrefix(named, "<") {
					model = named
					return true
				}
			}
		}
		if kind == "user" {
			said, ok := userText(rec)
			if !ok {
				return false
			}
			if word, ok := modelSwitch(said); ok {
				if id, ok := claudeModelID(word, printed); ok {
					model = id
					return true
				}
			} else if name, ok := modelPrinted(said); ok {
				printed = name
			}
		}
		return false
	})
	return model
}

// userText is a user row's text in either shape a build writes it.
func userText(rec object) (string, bool) {
	message, ok := rec.object("message")
	if !ok {
		return "", false
	}
	if text, ok := message.str("content"); ok {
		return text, true
	}
	blocks, ok := message.objects("content")
	if !ok {
		return "", false
	}
	texts := []string{}
	for _, b := range blocks {
		if t, ok := b.str("text"); ok {
			texts = append(texts, t)
		}
	}
	if len(texts) == 0 {
		return "", false
	}
	return strings.Join(texts, "\n"), true
}

// between is what sits between two markers; the rest of the text when the
// closing one is missing, because a row cut off mid-write still says it.
func between(open, close, text string) (string, bool) {
	i := strings.Index(text, open)
	if i < 0 {
		return "", false
	}
	rest := text[i+len(open):]
	if j := strings.Index(rest, close); j >= 0 {
		return rest[:j], true
	}
	return rest, true
}

// modelSwitch is the word a `/model` row asked for. An empty word is still a
// switch: `/model` alone opens a picker.
func modelSwitch(text string) (string, bool) {
	name, ok := between("<command-name>", "</command-name>", text)
	if !ok {
		return "", false
	}
	name = strings.TrimSpace(name)
	if name != "/model" && name != "model" {
		return "", false
	}
	args, _ := between("<command-args>", "</command-args>", text)
	fields := strings.Split(strings.TrimSpace(args), " ")
	return fields[0], true
}

// modelPrinted is the model name a `/model` printed, with the terminal's
// styling taken back out.
func modelPrinted(text string) (string, bool) {
	out, ok := between("<local-command-stdout>", "</local-command-stdout>", text)
	if !ok {
		return "", false
	}
	shown := plain(out)
	i := strings.Index(shown, "Set model to ")
	if i < 0 {
		return "", false
	}
	rest := shown[i+len("Set model to "):]
	if j := strings.IndexByte(rest, '\n'); j >= 0 {
		rest = rest[:j]
	}
	if j := strings.Index(rest, " and "); j >= 0 {
		rest = rest[:j]
	}
	name := strings.TrimSpace(rest)
	return name, name != ""
}

// claudeModelID is the id an assistant row would have carried for a model
// named the way `/model` names it, so the two sources say the same thing. A
// name this build does not know passes through as written.
func claudeModelID(word, printed string) (string, bool) {
	if word != "" {
		for _, m := range ClaudeModels {
			if m.Command == word {
				return m.ID, true
			}
		}
	}
	if printed != "" {
		for _, m := range ClaudeModels {
			if m.Name == printed {
				return m.ID, true
			}
		}
		return printed, true
	}
	return word, word != ""
}

// ---------- Codex ----------

// codexFacts reads Codex's own cumulative total and the model it is on from
// the end of its rollout: the last `token_count` event, and the last
// `turn_context` model — or, failing that, the last model anything named.
func codexFacts(data []byte) Facts {
	var usage *Summary
	model, fallback := "", ""
	reversedLines(data, func(line []byte) bool {
		wantsTokens := usage == nil && bytes.Contains(line, []byte("token_count"))
		wantsModel := model == "" && bytes.Contains(line, []byte(`"model"`))
		if !wantsTokens && !wantsModel {
			return false
		}
		rec, ok := decodeObject(line)
		if !ok {
			return false
		}
		payload, ok := rec.object("payload")
		if !ok {
			payload = rec
		}
		kind, _ := payload.str("type")
		if wantsTokens && kind == "token_count" {
			if outer, _ := rec.str("type"); outer == "event_msg" {
				if info, ok := payload.object("info"); ok {
					if totals, ok := info.object("total_token_usage"); ok {
						u := Summary{
							Input:      looseInt(totals, "input_tokens"),
							Output:     looseInt(totals, "output_tokens"),
							CacheRead:  looseInt(totals, "cached_input_tokens"),
							CacheWrite: looseInt(totals, "cache_write_input_tokens"),
						}
						if t, ok := looseIntOK(totals, "total_tokens"); ok {
							u.Total = t
						} else {
							u.Total = u.Input + u.Output
						}
						usage = &u
					}
				}
			}
		}
		if wantsModel {
			if named, ok := payload.str("model"); ok && named != "" {
				if kind == "turn_context" {
					model = named
				} else if fallback == "" {
					fallback = named
				}
			}
		}
		return usage != nil && model != ""
	})
	if model == "" {
		model = fallback
	}
	if usage != nil {
		usage.Model = model
		usage.Cost, usage.HasCost = Cost(*usage)
	}
	return Facts{Usage: usage, Model: model}
}

// looseInt is a number the way `SessionInfo.int` reads one: an integer, a
// finite float cut towards zero, or a numeric string.
func looseInt(o object, key string) int64 {
	n, _ := looseIntOK(o, key)
	return n
}

func looseIntOK(o object, key string) (int64, bool) {
	if s, ok := o.str(key); ok {
		n, err := strconv.ParseInt(s, 10, 64)
		return n, err == nil
	}
	raw := o[key]
	if len(raw) == 0 || !(raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9')) {
		return 0, false
	}
	return o.truncated(key), true
}
