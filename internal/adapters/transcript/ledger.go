package transcript

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"strings"
)

// The token ledger's reading layer (docs/token-ledger.md): a transcript read,
// incrementally, into what each of its tokens was spent on. Nothing here keeps
// a prompt, a tool input or any transcript text — only sizes by category.

// Category is what a token was spent on. docs/token-ledger.md "Categories".
type Category string

const (
	CategoryBoard      Category = "board"
	CategoryProtocol   Category = "protocol"
	CategoryRules      Category = "rules"
	CategoryImpl       Category = "impl"
	CategoryDelegate   Category = "delegate"
	CategoryHarness    Category = "harness"
	CategoryTalk       Category = "talk"
	CategoryCompaction Category = "compaction"
	CategoryOther      Category = "other"
)

// Categories is every category, in the order a ledger is shown and — which
// matters more — the order every sum over them is taken, so that the same
// transcript read twice gives the same floating-point totals.
var Categories = []Category{
	CategoryBoard, CategoryProtocol, CategoryRules, CategoryImpl, CategoryDelegate,
	CategoryHarness, CategoryTalk, CategoryCompaction, CategoryOther,
}

const (
	// ledgerFeedLimit is as much of a transcript as one Feed reads. The rest
	// waits for the next Feed, which starts where this one stopped.
	ledgerFeedLimit = 64 << 20
	// ledgerLineLimit is the longest line a Feed decodes. A longer one — a
	// pasted screenshot's base64, a tool result nobody should have printed —
	// is skipped and counted in Overlong; the context growth it carried is
	// still measured by the next call, only its category is lost.
	ledgerLineLimit = 8 << 20

	// fingerprintBytes is how much of a file's start names it: an append
	// leaves it alone, a replaced file almost never does.
	fingerprintBytes = 4096
	// recentCalls is how many message ids are remembered to count a repeated
	// row once when it is not next to its first.
	recentCalls = 32
	// compactionRatio: a context below this share of the previous call's is
	// a compaction (docs/token-ledger.md "Attribution" 4).
	compactionRatio = 0.7
	// aboveContext is the context size past which a call is counted as a long
	// session's (docs/token-ledger.md "Long-running sessions").
	aboveContext = 200_000
	// imageTokens is what one image is taken to occupy in a context. Its
	// base64 length says nothing about that.
	imageTokens = 1600
)

// Openings of a first message that make the person's text not talk.
const (
	childOpening          = "You are a Clawdline CHILD agent for task"
	rootAssignmentOpening = "You are an independently owned Clawdline Feature Root for Root Assignment"
)

// Tokens is one category's share of a session, by part. The numbers are
// float64 because a measured count is divided pro rata; their sum over every
// category is the measured count to within rounding.
type Tokens struct {
	Input        float64 `json:"input"`
	CacheWrite1h float64 `json:"cache_write_1h"`
	CacheWrite5m float64 `json:"cache_write_5m"`
	CacheRead    float64 `json:"cache_read"`
	Output       float64 `json:"output"`
	// Cost is in US dollars at list price, for the tokens whose model has a
	// price. Unpriced counts the tokens of calls whose model has none: the
	// cost is known only when it is zero.
	Cost     float64 `json:"cost"`
	Unpriced float64 `json:"unpriced"`
}

// Total is every token of every part.
func (t Tokens) Total() float64 {
	return t.Input + t.CacheWrite1h + t.CacheWrite5m + t.CacheRead + t.Output
}

// CostKnown says whether Cost is the whole cost.
func (t Tokens) CostKnown() bool { return t.Unpriced == 0 }

func (t *Tokens) add(o Tokens) {
	t.Input += o.Input
	t.CacheWrite1h += o.CacheWrite1h
	t.CacheWrite5m += o.CacheWrite5m
	t.CacheRead += o.CacheRead
	t.Output += o.Output
	t.Cost += o.Cost
	t.Unpriced += o.Unpriced
}

func (t Tokens) scaled(f float64) Tokens {
	return Tokens{Input: t.Input * f, CacheWrite1h: t.CacheWrite1h * f, CacheWrite5m: t.CacheWrite5m * f,
		CacheRead: t.CacheRead * f, Output: t.Output * f}
}

// priced is t with its cost, or its tokens counted as unpriced.
func (t Tokens) priced(model string) Tokens {
	if cost, ok := usageCost(model, t.Input, t.CacheWrite1h, t.CacheWrite5m, t.CacheRead, t.Output); ok {
		t.Cost = cost
	} else {
		t.Unpriced = t.Total()
	}
	return t
}

// Sizes is an amount of context by category.
type Sizes map[Category]float64

func (s Sizes) sum() float64 {
	total := 0.0
	for _, c := range Categories {
		total += s[c]
	}
	return total
}

func (s Sizes) clone() Sizes {
	out := Sizes{}
	for _, c := range Categories {
		if s[c] != 0 {
			out[c] = s[c]
		}
	}
	return out
}

// scaledTo is s with the same proportions, summing to total.
func (s Sizes) scaledTo(total float64) Sizes {
	sum := s.sum()
	out := Sizes{}
	if sum <= 0 {
		return out
	}
	for _, c := range Categories {
		if s[c] != 0 {
			out[c] = s[c] * total / sum
		}
	}
	return out
}

// NamedSize is one named part of a session's base: a tool, an instruction file.
type NamedSize struct {
	Name   string  `json:"name"`
	Tokens float64 `json:"tokens"`
}

// Composition is what the first call's context was made of, when the
// transcript recorded the harness's prompt snapshot. The parts are estimated
// from their text and scaled so they sum to Measured, the first call's
// measured context: only the division is an estimate.
type Composition struct {
	Measured     int64       `json:"measured"`
	SystemPrompt float64     `json:"system_prompt"`
	Tools        []NamedSize `json:"tools,omitempty"`
	SkillListing float64     `json:"skill_listing"`
	Instructions []NamedSize `json:"instructions,omitempty"`
	MCP          float64     `json:"mcp_instructions"`
	Other        float64     `json:"other"`
}

func (c *Composition) estimated() float64 {
	total := c.SystemPrompt + c.SkillListing + c.MCP + c.Other
	for _, t := range c.Tools {
		total += t.Tokens
	}
	for _, f := range c.Instructions {
		total += f.Tokens
	}
	return total
}

func (c *Composition) scale(f float64) {
	c.SystemPrompt *= f
	c.SkillListing *= f
	c.MCP *= f
	c.Other *= f
	for i := range c.Tools {
		c.Tools[i].Tokens *= f
	}
	for i := range c.Instructions {
		c.Instructions[i].Tokens *= f
	}
}

// ledgerCall is one API call whose output is not charged yet: a Claude call
// can span several rows, and what it did is known only when its last row has
// been read.
type ledgerCall struct {
	ID      string `json:"id,omitempty"`
	Model   string `json:"model,omitempty"`
	Output  int64  `json:"output"`
	Actions Sizes  `json:"actions,omitempty"`
	Above   bool   `json:"above,omitempty"`
}

// LedgerState is everything a ledger knows about one transcript, and all it
// needs to go on reading it. It is plain data: it is kept as JSON between
// passes, and a Feed from a state read back is a Feed from the state written.
type LedgerState struct {
	// Assistant is "claude" or "codex"; empty until the first record says.
	Assistant string `json:"assistant,omitempty"`

	// Offset is the byte the next Feed starts at: the start of a line. The
	// file is named by its first HeadLen bytes' digest, so a file that
	// shrank below Offset, or whose start changed, is read again from zero.
	Offset   int64  `json:"offset"`
	HeadLen  int64  `json:"head_len,omitempty"`
	HeadSum  string `json:"head_sum,omitempty"`
	Skipping bool   `json:"skipping,omitempty"`

	Lines       int64 `json:"lines"`
	Overlong    int64 `json:"overlong,omitempty"`
	Undecodable int64 `json:"undecodable,omitempty"`
	Restarts    int64 `json:"restarts,omitempty"`

	Model          string `json:"model,omitempty"`
	Calls          int64  `json:"calls"`
	SidechainCalls int64  `json:"sidechain_calls,omitempty"`
	Compactions    int64  `json:"compactions,omitempty"`
	PeakContext    int64  `json:"peak_context"`
	// CallsAbove and Above are the calls made, and what they cost, with a
	// context past aboveContext.
	CallsAbove int64  `json:"calls_above,omitempty"`
	Above      Tokens `json:"above"`

	PrevContext int64 `json:"prev_context"`
	PrevOutput  int64 `json:"prev_output,omitempty"`
	PrevActions Sizes `json:"prev_actions,omitempty"`
	// Segments is the context as of the last call, by category; Base is the
	// first call's, kept for a compaction; Arrived is what came in since the
	// last call, by estimated size.
	Segments Sizes `json:"segments,omitempty"`
	Base     Sizes `json:"base,omitempty"`
	Arrived  Sizes `json:"arrived,omitempty"`
	// Tools names the category of each tool use whose result may still come.
	Tools     map[string]Category `json:"tools,omitempty"`
	Recent    []string            `json:"recent,omitempty"`
	Open      *ledgerCall         `json:"open,omitempty"`
	OpenSide  *ledgerCall         `json:"open_sidechain,omitempty"`
	SpokeOnce bool                `json:"spoke_once,omitempty"`

	CodexTotal   int64               `json:"codex_total,omitempty"`
	CodexActions Sizes               `json:"codex_actions,omitempty"`
	CodexPending map[string]Category `json:"codex_pending,omitempty"`

	// Measured is the session's own count, by part; Spent is the same tokens
	// by category. Spent's sum is Measured.
	Measured Tokens              `json:"measured"`
	Spent    map[Category]Tokens `json:"spent,omitempty"`
	// Composition is the first call's context by part, nil when the
	// transcript carried no prompt snapshot: unknown, not zero. Pre is it
	// being gathered before that call.
	Composition *Composition `json:"composition,omitempty"`
	Pre         *Composition `json:"pre,omitempty"`
	Snapshot    bool         `json:"snapshot,omitempty"`
}

// FeedResult is what one Feed did.
type FeedResult struct {
	Read      int64
	More      bool
	Restarted bool
}

// Feed reads path from where the state stopped, at most ledgerFeedLimit
// bytes, and folds every whole line into the state. A line still being
// written is left for the next Feed.
func (s *LedgerState) Feed(path string) (FeedResult, error) {
	return s.feed(path, ledgerFeedLimit, ledgerLineLimit)
}

func (s *LedgerState) feed(path string, feedLimit, lineLimit int64) (FeedResult, error) {
	var res FeedResult
	f, err := os.Open(path)
	if err != nil {
		return res, recordError(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return res, err
	}
	size := st.Size()
	head, err := fingerprint(f, size)
	if err != nil {
		return res, err
	}
	if size < s.Offset || (s.HeadLen > 0 && digest(head, s.HeadLen) != s.HeadSum) {
		assistant, restarts := s.Assistant, s.Restarts
		*s = LedgerState{Assistant: assistant, Restarts: restarts + 1}
		res.Restarted = true
	}
	if s.HeadLen == 0 && len(head) > 0 {
		s.HeadLen = int64(len(head))
		s.HeadSum = digest(head, s.HeadLen)
	}
	if _, err := f.Seek(s.Offset, io.SeekStart); err != nil {
		return res, err
	}
	// The limit is checked between lines: a Feed finishes the line it is in,
	// so it reads at most feedLimit plus one line — and a line past
	// lineLimit is skipped a lineLimit at a time, across Feeds if need be.
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		if res.Read >= feedLimit {
			res.More = s.Offset+res.Read < size
			break
		}
		line, n, complete, over, err := nextLine(r, lineLimit)
		if err != nil {
			s.Offset += res.Read
			return res, err
		}
		if n == 0 {
			break
		}
		if s.Skipping || over {
			// An overlong line is consumed and counted once, whether it ends
			// in this Feed or a later one.
			if !s.Skipping {
				s.Overlong++
			}
			res.Read += n
			s.Skipping = !complete
			continue
		}
		if !complete {
			break
		}
		res.Read += n
		s.Lines++
		s.line(line)
	}
	s.Offset += res.Read
	return res, nil
}

func fingerprint(f *os.File, size int64) ([]byte, error) {
	n := min(size, fingerprintBytes)
	head := make([]byte, n)
	if _, err := f.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return head, nil
}

func digest(head []byte, n int64) string {
	if int64(len(head)) < n {
		return ""
	}
	sum := sha256.Sum256(head[:n])
	return hex.EncodeToString(sum[:8])
}

// nextLine reads one line: its bytes without the newline (nil past
// lineLimit), how many bytes it took, whether it ended in a newline, and
// whether it was longer than lineLimit. A line past lineLimit is returned
// unfinished once that much of it has been read; the caller goes on skipping.
func nextLine(r *bufio.Reader, lineLimit int64) ([]byte, int64, bool, bool, error) {
	var line []byte
	var n int64
	over := false
	for {
		chunk, err := r.ReadSlice('\n')
		n += int64(len(chunk))
		if !over {
			if int64(len(line)+len(chunk)) > lineLimit+1 {
				over, line = true, nil
			} else {
				line = append(line, chunk...)
			}
		}
		switch {
		case err == nil:
			if !over {
				line = line[:len(line)-1]
			}
			return line, n, true, over, nil
		case errors.Is(err, bufio.ErrBufferFull):
			if over {
				return nil, n, false, true, nil
			}
		case errors.Is(err, io.EOF):
			return line, n, false, over, nil
		default:
			return nil, n, false, over, err
		}
	}
}

func (s *LedgerState) line(raw []byte) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return
	}
	rec, ok := decodeObject(raw)
	if !ok {
		s.Undecodable++
		return
	}
	if s.Assistant == "" {
		s.Assistant = "claude"
		if _, ok := rec.object("payload"); ok {
			s.Assistant = "codex"
		}
	}
	if s.Assistant == "codex" {
		s.codexLine(rec)
	} else {
		s.claudeLine(rec)
	}
}

// Totals is the session by category and its measured count, with the output
// of a call still open charged as it stands.
func (s *LedgerState) Totals() (map[Category]Tokens, Tokens) {
	spent := map[Category]Tokens{}
	for c, t := range s.Spent {
		spent[c] = t
	}
	measured := s.Measured
	for _, call := range []*ledgerCall{s.Open, s.OpenSide} {
		if call == nil {
			continue
		}
		weights := call.Actions
		if call == s.OpenSide {
			weights = Sizes{CategoryDelegate: 1}
		}
		out := Tokens{Output: float64(call.Output)}
		measured.Output += out.Output
		charge(spent, actionWeights(weights), nil, out, call.Model)
	}
	return spent, measured
}

// ---------- attribution ----------

func actionWeights(actions Sizes) Sizes {
	if actions.sum() <= 0 {
		return Sizes{CategoryTalk: 1}
	}
	return actions
}

// charge divides amount over weights, or over fallback when weights is
// empty, or gives it to other when both are, and prices each share.
func charge(spent map[Category]Tokens, weights, fallback Sizes, amount Tokens, model string) Tokens {
	if amount.Total() == 0 {
		return Tokens{}
	}
	if weights.sum() <= 0 {
		weights = fallback
	}
	if weights.sum() <= 0 {
		weights = Sizes{CategoryOther: 1}
	}
	sum := weights.sum()
	var charged Tokens
	for _, c := range Categories {
		w := weights[c]
		if w <= 0 {
			continue
		}
		share := amount.scaled(w / sum).priced(model)
		t := spent[c]
		t.add(share)
		spent[c] = t
		charged.add(share)
	}
	return charged
}

func (s *LedgerState) charge(weights, fallback Sizes, amount Tokens, model string) Tokens {
	if s.Spent == nil {
		s.Spent = map[Category]Tokens{}
	}
	return charge(s.Spent, weights, fallback, amount, model)
}

func (s *LedgerState) arrive(c Category, tokens float64) {
	if tokens <= 0 {
		return
	}
	if s.Arrived == nil {
		s.Arrived = Sizes{}
	}
	s.Arrived[c] += tokens
}

// pre is the composition being gathered before the first call.
func (s *LedgerState) pre() *Composition {
	if s.Calls > 0 {
		return &Composition{}
	}
	if s.Pre == nil {
		s.Pre = &Composition{}
	}
	return s.Pre
}

// openCall charges one call's input side: cache reads to what the context
// held, cache writes and uncached input to what is new in it.
func (s *LedgerState) openCall(model string, input, write1h, write5m, read int64) bool {
	ctx := input + write1h + write5m + read
	if ctx <= 0 {
		return false
	}
	s.Calls++
	if model != "" && !strings.HasPrefix(model, "<") {
		s.Model = model
	}
	s.Measured.Input += float64(input)
	s.Measured.CacheWrite1h += float64(write1h)
	s.Measured.CacheWrite5m += float64(write5m)
	s.Measured.CacheRead += float64(read)

	var held, fresh Sizes
	switch {
	case s.Calls == 1:
		fresh = s.firstContext(ctx)
		s.Base = fresh.clone()
	case float64(ctx) < compactionRatio*float64(s.PrevContext):
		s.Compactions++
		fresh = s.compacted(ctx)
	case ctx >= s.PrevContext:
		arrivals := s.Arrived.clone()
		for c, w := range actionWeights(s.PrevActions).scaledTo(float64(s.PrevOutput)) {
			arrivals[c] += w
		}
		if arrivals.sum() <= 0 {
			arrivals = Sizes{CategoryOther: 1}
		}
		held = s.Segments.clone()
		fresh = arrivals.scaledTo(float64(ctx - s.PrevContext))
	default:
		// A little smaller: something was dropped from every part alike.
		held = s.Segments.scaledTo(float64(ctx))
	}
	charged := s.charge(held, fresh, Tokens{CacheRead: float64(read)}, model)
	charged.add(s.charge(fresh, held, Tokens{Input: float64(input), CacheWrite1h: float64(write1h),
		CacheWrite5m: float64(write5m)}, model))

	next := held.clone()
	for c, v := range fresh {
		next[c] += v
	}
	s.Segments = next
	s.Arrived, s.PrevOutput, s.PrevActions = nil, 0, nil
	s.PrevContext = ctx
	s.PeakContext = max(s.PeakContext, ctx)
	if ctx > aboveContext {
		s.CallsAbove++
		s.Above.add(charged)
	}
	return true
}

// firstContext is the resident base: what arrived before the first call at
// its estimated size, and the rest — the system prompt, the tools — harness.
func (s *LedgerState) firstContext(ctx int64) Sizes {
	arrivals := s.Arrived.clone()
	if pre := s.Pre; pre != nil && s.Snapshot {
		comp := *pre
		comp.Measured = ctx
		if est := comp.estimated(); est > 0 {
			comp.scale(float64(ctx) / est)
			s.Composition = &comp
		}
	}
	s.Pre = nil
	if est := arrivals.sum(); est >= float64(ctx) {
		return arrivals.scaledTo(float64(ctx))
	} else {
		arrivals[CategoryHarness] += float64(ctx) - est
	}
	return arrivals
}

// compacted is the context after a compaction: the base at its proportions,
// and everything above it compaction.
func (s *LedgerState) compacted(ctx int64) Sizes {
	base := s.Base.clone()
	if sum := base.sum(); sum > float64(ctx) {
		return base.scaledTo(float64(ctx))
	} else {
		base[CategoryCompaction] += float64(ctx) - sum
	}
	return base
}

// closeCall charges a call's output to what it did.
func (s *LedgerState) closeCall(call *ledgerCall, weights Sizes) {
	out := Tokens{Output: float64(call.Output)}
	s.Measured.Output += out.Output
	charged := s.charge(actionWeights(weights), nil, out, call.Model)
	if call.Above {
		s.Above.add(charged)
	}
}

func (s *LedgerState) closeOpen() {
	if s.Open == nil {
		return
	}
	call := s.Open
	s.Open = nil
	s.closeCall(call, call.Actions)
	s.PrevOutput = call.Output
	s.PrevActions = actionWeights(call.Actions).clone()
}

func (s *LedgerState) closeSide() {
	if s.OpenSide == nil {
		return
	}
	call := s.OpenSide
	s.OpenSide = nil
	s.closeCall(call, Sizes{CategoryDelegate: 1})
}

func (s *LedgerState) seen(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range s.Recent {
		if r == id {
			return true
		}
	}
	s.Recent = append(s.Recent, id)
	if len(s.Recent) > recentCalls {
		s.Recent = s.Recent[len(s.Recent)-recentCalls:]
	}
	return false
}

// ---------- Claude ----------

type claudeUsage struct {
	input, write1h, write5m, read, output int64
}

// readClaudeUsage is one row's `message.usage`. Writes the row does not split
// are 1-hour: every Claude Code cache write measured on 2026-09-25 was.
func readClaudeUsage(u object) claudeUsage {
	out := claudeUsage{
		input:  intOrZero(u, "input_tokens"),
		read:   intOrZero(u, "cache_read_input_tokens"),
		output: intOrZero(u, "output_tokens"),
	}
	write := intOrZero(u, "cache_creation_input_tokens")
	if split, ok := u.object("cache_creation"); ok {
		out.write1h = intOrZero(split, "ephemeral_1h_input_tokens")
		out.write5m = intOrZero(split, "ephemeral_5m_input_tokens")
	}
	if rest := write - out.write1h - out.write5m; rest > 0 {
		out.write1h += rest
	}
	return out
}

func (s *LedgerState) claudeLine(rec object) {
	kind, _ := rec.str("type")
	side, _ := rec.boolean("isSidechain")
	switch kind {
	case "assistant":
		message, ok := rec.object("message")
		if !ok {
			return
		}
		id, _ := message.str("id")
		model, _ := message.str("model")
		counts, hasUsage := message.object("usage")
		var u claudeUsage
		if hasUsage {
			u = readClaudeUsage(counts)
		}
		if side {
			s.claudeSidechain(id, model, u, hasUsage)
			return
		}
		blocks, _ := message.objects("content")
		if s.Open != nil && id != "" && s.Open.ID == id {
			s.Open.Output = max(s.Open.Output, u.output)
			s.claudeActions(s.Open, blocks)
			return
		}
		if !hasUsage || s.seen(id) {
			return
		}
		s.closeOpen()
		call := &ledgerCall{ID: id, Model: model, Output: u.output}
		if !s.openCall(model, u.input, u.write1h, u.write5m, u.read) {
			return
		}
		call.Above = s.PrevContext > aboveContext
		s.Tools = nil
		s.claudeActions(call, blocks)
		s.Open = call
	case "user":
		if !side {
			s.claudeUser(rec)
		}
	case "attachment":
		if !side {
			s.claudeAttachment(rec)
		}
	}
}

// claudeSidechain is a subagent's call: its whole bill is delegate, and it is
// not part of the session's own context.
func (s *LedgerState) claudeSidechain(id, model string, u claudeUsage, hasUsage bool) {
	if s.OpenSide != nil && id != "" && s.OpenSide.ID == id {
		s.OpenSide.Output = max(s.OpenSide.Output, u.output)
		return
	}
	if !hasUsage || s.seen(id) {
		return
	}
	s.closeSide()
	s.SidechainCalls++
	s.Measured.Input += float64(u.input)
	s.Measured.CacheWrite1h += float64(u.write1h)
	s.Measured.CacheWrite5m += float64(u.write5m)
	s.Measured.CacheRead += float64(u.read)
	s.charge(Sizes{CategoryDelegate: 1}, nil, Tokens{Input: float64(u.input), CacheWrite1h: float64(u.write1h),
		CacheWrite5m: float64(u.write5m), CacheRead: float64(u.read)}, model)
	s.OpenSide = &ledgerCall{ID: id, Model: model, Output: u.output}
}

func (s *LedgerState) claudeActions(call *ledgerCall, blocks []object) {
	for _, b := range blocks {
		if t, _ := b.str("type"); t != "tool_use" {
			continue
		}
		name, _ := b.str("name")
		c := Classify(name, b["input"])
		if call.Actions == nil {
			call.Actions = Sizes{}
		}
		call.Actions[c]++
		if id, ok := b.str("id"); ok {
			if s.Tools == nil {
				s.Tools = map[string]Category{}
			}
			s.Tools[id] = c
		}
	}
}

func (s *LedgerState) claudeUser(rec object) {
	message, ok := rec.object("message")
	if !ok {
		return
	}
	meta, _ := rec.boolean("isMeta")
	summary, _ := rec.boolean("isCompactSummary")
	said := func(text string) {
		c := s.personCategory(text)
		switch {
		case summary:
			c = CategoryCompaction
		case meta || strings.HasPrefix(strings.TrimSpace(text), "<system-reminder>"):
			c = CategoryHarness
		}
		s.arrive(c, estimate(text))
		s.pre().Other += estimate(text)
	}
	if text, ok := message.str("content"); ok {
		said(text)
		return
	}
	blocks, _ := message.objects("content")
	for _, b := range blocks {
		switch t, _ := b.str("type"); t {
		case "text":
			text, _ := b.str("text")
			said(text)
		case "image":
			s.arrive(CategoryTalk, imageTokens)
			s.pre().Other += imageTokens
		case "tool_result":
			id, _ := b.str("tool_use_id")
			c, ok := s.Tools[id]
			if !ok {
				c = CategoryOther
			}
			s.arrive(c, resultTokens(b["content"]))
		}
	}
}

// personCategory is what the person's text is: talk, except the first
// message of a Clawdline child (protocol) or of a Root Assignment (board).
func (s *LedgerState) personCategory(text string) Category {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.HasPrefix(trimmed, "<") {
		return CategoryTalk
	}
	first := !s.SpokeOnce
	s.SpokeOnce = true
	switch {
	case first && strings.HasPrefix(trimmed, childOpening):
		return CategoryProtocol
	case first && strings.HasPrefix(trimmed, rootAssignmentOpening):
		return CategoryBoard
	}
	return CategoryTalk
}

// claudeAttachment is something the harness put in the context. The prompt
// snapshot is not an arrival — it is the resident base itself — but it and
// the listings name the base's parts.
func (s *LedgerState) claudeAttachment(rec object) {
	att, ok := rec.object("attachment")
	if !ok {
		return
	}
	size := estimate(string(rec["attachment"]))
	switch t, _ := att.str("type"); t {
	case "prompt_snapshot":
		if s.Calls > 0 {
			return
		}
		pre := s.pre()
		s.Snapshot = true
		pre.SystemPrompt = estimate(textOf(att["systemPrompt"]))
		pre.Tools = nil
		if tools, ok := rawArray(att["tools"]); ok {
			for _, raw := range tools {
				name := "tool"
				if o, ok := rawObject(raw); ok {
					if n, ok := o.str("name"); ok {
						name = n
					}
				}
				pre.Tools = append(pre.Tools, NamedSize{Name: name, Tokens: estimate(string(raw))})
			}
		}
	case "instructions":
		s.arrive(CategoryRules, size)
		pre := s.pre()
		files := instructionFiles(att, 0)
		if len(files) == 0 {
			files = []NamedSize{{Name: "instructions", Tokens: size}}
		}
		pre.Instructions = append(pre.Instructions, files...)
	case "skill_listing":
		s.arrive(CategoryHarness, size)
		s.pre().SkillListing += size
	case "mcp_instructions_delta":
		s.arrive(CategoryHarness, size)
		s.pre().MCP += size
	default:
		s.arrive(CategoryHarness, size)
		s.pre().Other += size
	}
}

// instructionFiles finds the files an instructions attachment carries: any
// object with a path and a text, at any depth a few levels down. Only the
// file's name is kept.
func instructionFiles(o object, depth int) []NamedSize {
	if depth > 4 {
		return nil
	}
	name := ""
	for _, k := range []string{"path", "filePath", "file"} {
		if p, ok := o.str(k); ok && p != "" {
			name = path.Base(strings.ReplaceAll(p, "\\", "/"))
			break
		}
	}
	for _, k := range []string{"content", "text"} {
		if text, ok := o.str(k); ok && name != "" {
			return []NamedSize{{Name: name, Tokens: estimate(text)}}
		}
	}
	var out []NamedSize
	for _, k := range sortedKeys(o) {
		raw := o[k]
		if child, ok := rawObject(raw); ok {
			out = append(out, instructionFiles(child, depth+1)...)
		} else if list, ok := rawArray(raw); ok {
			for _, item := range list {
				if child, ok := rawObject(item); ok {
					out = append(out, instructionFiles(child, depth+1)...)
				}
			}
		}
	}
	return out
}

// estimate is a text's size in tokens, at four bytes a token. Only
// proportions are taken from it: every total is scaled to a measured one.
func estimate(text string) float64 { return float64(len(text)) / 4 }

// textOf is the text in a string or a list of text blocks.
func textOf(raw json.RawMessage) string {
	if s, ok := rawString(raw); ok {
		return s
	}
	var b strings.Builder
	if blocks, ok := rawObjects(raw); ok {
		for _, block := range blocks {
			if t, ok := block.str("text"); ok {
				b.WriteString(t)
			}
		}
		return b.String()
	}
	return string(raw)
}

// resultTokens is a tool result's estimated size: its text, and a fixed size
// for each image.
func resultTokens(raw json.RawMessage) float64 {
	if s, ok := rawString(raw); ok {
		return estimate(s)
	}
	blocks, ok := rawObjects(raw)
	if !ok {
		return estimate(string(raw))
	}
	total := 0.0
	for _, b := range blocks {
		if t, _ := b.str("type"); t == "image" {
			total += imageTokens
			continue
		}
		if text, ok := b.str("text"); ok {
			total += estimate(text)
		}
	}
	return total
}

// ---------- the classifier ----------

// Classify is what a tool call is spent on, decided by what the call does and
// not by what it mentions (docs/token-ledger.md "Categories"). tool is the
// tool's name; input is its arguments as the transcript recorded them.
func Classify(tool string, input json.RawMessage) Category {
	in, _ := decodeObject(input)
	switch tool {
	case "Agent", "Task", "Workflow", "SendMessage":
		return CategoryDelegate
	case "ToolSearch":
		return CategoryHarness
	case "AskUserQuestion":
		return CategoryTalk
	case "Skill":
		if name, _ := in.str("skill"); name == "clawdline" || strings.HasPrefix(name, "clawdline:") {
			return CategoryProtocol
		}
		return CategoryHarness
	case "Read", "NotebookRead":
		return classifyPath(firstString(in, "file_path", "path", "notebook_path"))
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		if c := classifyPath(firstString(in, "file_path", "path", "notebook_path")); c == CategoryProtocol {
			return c
		}
		return CategoryImpl
	case "Grep", "Glob", "LS", "WebFetch", "WebSearch", "apply_patch", "view_image":
		return CategoryImpl
	case "Bash", "exec_command", "shell", "shell_command", "local_shell", "container.exec":
		command := firstString(in, "command", "cmd")
		if command == "" {
			if words, ok := stringList(in["command"]); ok {
				command = shellOf(words)
			} else if words, ok := stringList(in["cmd"]); ok {
				command = shellOf(words)
			}
		}
		if command == "" {
			return CategoryOther
		}
		return classifyCommand(command)
	}
	return CategoryOther
}

func firstString(o object, keys ...string) string {
	for _, k := range keys {
		if s, ok := o.str(k); ok && s != "" {
			return s
		}
	}
	return ""
}

func stringList(raw json.RawMessage) ([]string, bool) {
	var words []string
	if !isArray(raw) || json.Unmarshal(raw, &words) != nil || len(words) == 0 {
		return nil, false
	}
	return words, true
}

// shellOf is the command line an argument vector runs: `bash -lc "…"` is
// its script, anything else the words joined.
func shellOf(words []string) string {
	if len(words) >= 3 && isShell(words[0]) && strings.HasPrefix(words[1], "-") && strings.Contains(words[1], "c") {
		return words[len(words)-1]
	}
	return strings.Join(words, " ")
}

func isShell(word string) bool {
	switch path.Base(word) {
	case "bash", "sh", "zsh", "dash":
		return true
	}
	return false
}

// categoryRank orders what one command's parts can be: the most specific
// part names the whole command.
var categoryRank = map[Category]int{CategoryImpl: 1, CategoryRules: 2, CategoryProtocol: 3, CategoryBoard: 4}

func stronger(a, b Category) Category {
	if categoryRank[b] > categoryRank[a] {
		return b
	}
	return a
}

// classifyPath is what reading a file is for.
func classifyPath(p string) Category {
	p = strings.ReplaceAll(p, "\\", "/")
	base := path.Base(p)
	switch {
	case p == "":
		return CategoryImpl
	case strings.Contains(p, "root-assignments/") && base == "ASSIGNMENT.md":
		return CategoryBoard
	case base == "CHILD.md":
		return CategoryProtocol
	case strings.Contains(p, "/tasks/") && (base == "task.json" || strings.HasPrefix(base, "result.json") ||
		base == "accepted.json" || base == "progress.json"):
		return CategoryProtocol
	case base == "AGENTS.md" || base == "CLAUDE.md" || base == "MEMORY.md" || base == "dispatch-policy.md" ||
		strings.Contains(p, "/memory/"):
		return CategoryRules
	}
	return CategoryImpl
}

// classifyCommand is what a shell command line does: each simple command is
// classified by its verb, and the most specific names the line.
func classifyCommand(line string) Category {
	out := CategoryImpl
	for _, segment := range splitCommands(line) {
		out = stronger(out, classifySimple(shellWords(segment)))
	}
	return out
}

var readers = map[string]bool{"cat": true, "head": true, "tail": true, "less": true, "more": true, "bat": true,
	"sed": true, "nl": true, "awk": true}

func classifySimple(words []string) Category {
	for len(words) > 0 {
		w := words[0]
		if strings.Contains(w, "=") && !strings.HasPrefix(w, "-") && !strings.Contains(w, "/") {
			words = words[1:] // an environment assignment
			continue
		}
		if w == "sudo" || w == "env" || w == "time" || w == "command" || w == "exec" || w == "nohup" {
			words = words[1:]
			continue
		}
		break
	}
	if len(words) == 0 {
		return CategoryImpl
	}
	verb := words[0]
	base := path.Base(strings.ReplaceAll(verb, "\\", "/"))
	switch {
	case isShell(verb) && len(words) > 1:
		if strings.HasPrefix(words[1], "-") && strings.Contains(words[1], "c") && len(words) > 2 {
			return classifyCommand(words[2])
		}
		return classifySimple(words[1:])
	case base == "curl" || base == "wget" || base == "http" || base == "xh":
		joined := strings.Join(words, " ")
		if !talksToDaemon(joined) {
			return CategoryImpl
		}
		if strings.Contains(joined, "/v1/work/") || strings.Contains(joined, "/v1/work?") {
			return CategoryBoard
		}
		return CategoryProtocol
	case base == "clawdline":
		for _, w := range words[1:] {
			if strings.HasPrefix(w, "-") {
				continue
			}
			if w == "item" || w == "items" || w == "work" || w == "board" {
				return CategoryBoard
			}
			break
		}
		return CategoryProtocol
	case strings.HasPrefix(base, "check-") && strings.HasSuffix(base, ".sh"):
		return CategoryRules
	case readers[base]:
		out := CategoryImpl
		for _, w := range words[1:] {
			if !strings.HasPrefix(w, "-") {
				out = stronger(out, classifyPath(w))
			}
		}
		return out
	}
	return CategoryImpl
}

// talksToDaemon: the daemon's port on a loopback address, or its headers.
func talksToDaemon(command string) bool {
	lower := strings.ToLower(command)
	for _, host := range []string{"127.0.0.1:7727", "localhost:7727", "[::1]:7727"} {
		if strings.Contains(lower, host) {
			return true
		}
	}
	return strings.Contains(lower, "x-clawdline")
}

// splitCommands cuts a command line at unquoted separators: newlines, `;`,
// `&&`, `||` and pipes.
func splitCommands(line string) []string {
	var out []string
	var cur strings.Builder
	quote := byte(0)
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			} else if ch == '\\' && quote == '"' && i+1 < len(line) {
				cur.WriteByte(ch)
				i++
				ch = line[i]
			}
		case ch == '\'' || ch == '"':
			quote = ch
		case ch == '\\' && i+1 < len(line):
			cur.WriteByte(ch)
			i++
			ch = line[i]
		case ch == '\n' || ch == ';' || ch == '|' || ch == '&':
			flush()
			continue
		}
		cur.WriteByte(ch)
	}
	flush()
	return out
}

// shellWords is a simple command's words with their quotes taken off.
func shellWords(segment string) []string {
	var out []string
	var cur strings.Builder
	quote := byte(0)
	inWord := false
	for i := 0; i < len(segment); i++ {
		ch := segment[i]
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
				continue
			}
			if ch == '\\' && quote == '"' && i+1 < len(segment) {
				i++
				ch = segment[i]
			}
			cur.WriteByte(ch)
		case ch == '\'' || ch == '"':
			quote, inWord = ch, true
		case ch == ' ' || ch == '\t':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		case ch == '\\' && i+1 < len(segment):
			i++
			cur.WriteByte(segment[i])
			inWord = true
		default:
			cur.WriteByte(ch)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out
}
