package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// recordPath is where an assistant keeps its own account of one session.
//
// Empty means there is nothing to read, which is different from a file that
// could not be read: a session whose conversation id was never recovered from
// its command line has no record to point at, and saying "unreadable" there
// would blame the disk for a missing identifier.
func recordPath(item session.Session) string {
	if item.ConversationID == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch item.Assistant {
	case session.AssistantClaude:
		return transcript.ClaudePath(home, item.CWD, item.ConversationID)
	case session.AssistantCodex:
		return transcript.CodexPath(home, item.ConversationID)
	}
	return ""
}

// unlocatedNote is what a person reads when the session has no record to read
// because nothing named the conversation.
//
// "could not be located" is true of all three of those and useful about none
// of them: a session nobody has typed into yet, a machine that could not read
// its own table of open files, and two conversations sharing one process are
// three different things to do next. The scan already worked out which
// (session.Binding) and said why in a sentence, so that sentence is what is
// passed on rather than re-derived here.
func unlocatedNote(item session.Session) string {
	if item.BindingDetail != "" {
		return "this session's own record could not be located: " + item.BindingDetail
	}
	return "this session's own record could not be located"
}

// recordNote is what a person reads when a session's record could not be
// read: what went wrong, and never where. The transcript page reaches paired
// devices over Cloud, and the operating system's own error names the file
// under the person's home directory, so an error the
// transcript readers did not type is not passed on in its own words.
func recordNote(err error) string {
	var unreadable *transcript.UnreadableError
	switch {
	case errors.As(err, &unreadable):
		return unreadable.Error()
	case errors.Is(err, transcript.ErrNoRecord):
		return transcript.ErrNoRecord.Error()
	case errors.Is(err, transcript.ErrNotFound):
		return transcript.ErrNotFound.Error()
	}
	return "this session's record could not be read"
}

// transcriptRoute returns the newest entries of one session, in the shape the
// Swift app's transcript route writes them.
func (s *Server) transcriptRoute(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("session")
	if id == "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "name a session")
		return
	}
	// The Swift app's bounds: 200 unless asked, never fewer than 1 or more
	// than 1,000.
	limit := 200
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
		limit = min(max(n, 1), 1000)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	item, err := s.actions().Find(ctx, id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	writeJSON(w, s.transcriptPage(id, item, limit))
}

// transcriptPage is the newest `limit` entries of one session's own record.
func (s *Server) transcriptPage(id string, item session.Session, limit int) contract.TranscriptPage {
	page := contract.TranscriptPage{ID: id, Entries: []contract.TranscriptEntry{}}
	path := recordPath(item)
	if path == "" {
		page.Evidence = contract.EvidenceNone
		page.Note = unlocatedNote(item)
		return page
	}
	page.Path = path

	var read transcript.Page
	var err error
	if item.Assistant == session.AssistantCodex {
		read, err = transcript.ReadCodex(path, limit)
	} else {
		read, err = transcript.ReadClaude(path, limit)
	}
	if errors.Is(err, transcript.ErrNoRecord) {
		// A session that has just started has not written its record yet.
		// That is a conversation with nothing in it, answered as the Swift
		// app answers it: no entries and an empty signature, read from where
		// the record will be. The console draws its empty state for it.
		page.Evidence = contract.EvidenceTranscript
		return page
	}
	if err != nil {
		page.Evidence = contract.EvidenceNone
		page.Note = recordNote(err)
		return page
	}
	page.Evidence = contract.EvidenceTranscript
	page.Signature = read.Signature
	entries := make([]contract.TranscriptEntry, 0, len(read.Entries))
	now := time.Now()
	for _, e := range read.Entries {
		row := transcriptEntry(e)
		row.Artifacts = s.pictures.wireArtifacts(e, now)
		entries = append(entries, row)
	}
	kept, omitted := boundedTranscript(entries)
	page.Entries = kept
	if omitted > 0 {
		page.Truncation = &contract.TranscriptTruncation{
			Reason:              "transcript_byte_budget",
			EntriesOmittedCount: int64(omitted),
			BudgetBytes:         transcriptBudget,
		}
	}
	if read.Unread > 0 {
		// The read window ran out before the page was full: there is more
		// conversation before the first entry, and the page must not look
		// as if it began there (limits N17). The Swift app's payload has no
		// such key; it is this daemon's own, and additive.
		page.Unread = &contract.TranscriptUnread{
			Reason:      "transcript_read_window",
			Bytes:       read.Unread,
			WindowBytes: transcript.ReadBudget,
		}
	}
	return page
}

// transcriptBudget is what one transcript read may carry: the first-paint
// budget, which the Swift app applies to the same rows. `limit` counts rows,
// and one row can be a whole tool output.
const transcriptBudget = 150 * 1024

// boundedTranscript keeps the newest entries that fit and says how many older
// ones were left out. One entry larger than the whole budget is still sent,
// alone, because a page with nothing on it is not something a reader can act on.
//
// Each row is measured as the Swift app's serializer writes it — slashes
// escaped, an explicit `imageCount` on every user row — so both servers cut
// the same conversation at the same place.
func boundedTranscript(entries []contract.TranscriptEntry) ([]contract.TranscriptEntry, int) {
	used := 0
	first := len(entries)
	for first > 0 {
		size := legacyRowBytes(entries[first-1])
		if first < len(entries) && used+size > transcriptBudget {
			break
		}
		used += size
		first--
	}
	return entries[first:], first
}

func legacyRowBytes(e contract.TranscriptEntry) int {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if enc.Encode(e) != nil {
		return transcriptBudget
	}
	body := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	size := len(body) + bytes.Count(body, []byte("/"))
	if e.Role == "user" && e.ImageCount == 0 {
		size += len(`,"imageCount":0`)
	}
	if e.Activity != nil && e.Activity.DurationMs < 0 {
		size -= len(`,"durationMs":-1`)
	}
	if n := e.Notice; n != nil && n.Kind == "task_finished" {
		if n.Outstanding == 0 {
			size += len(`,"outstanding":0`)
		}
		if !n.ClaimsReleased {
			size += len(`,"claims_released":false`)
		}
		if !n.ChildMayStillWrite {
			size += len(`,"child_may_still_write":false`)
		}
	}
	return size
}

// transcriptEntry is one entry on the wire. A tool's result is filed under
// `tool` with no name, which is how the console tells it from the call.
func transcriptEntry(e transcript.Entry) contract.TranscriptEntry {
	role := e.Kind
	if role == transcript.KindToolResult {
		role = transcript.KindTool
	}
	out := contract.TranscriptEntry{
		Role:            role,
		Text:            e.Text,
		Tool:            e.Tool,
		At:              e.At,
		ImageCount:      int64(e.ImageCount),
		Source:          e.Source,
		SourceMode:      e.SourceMode,
		SourceAssistant: e.SourceAssistant,
	}
	if e.Kind == transcript.KindToolResult {
		out.Tool = ""
	}
	for _, c := range e.FileChanges {
		out.FileChanges = append(out.FileChanges, contract.TranscriptFileChange{
			Path: c.Path, Kind: c.Kind,
			UnifiedDiff: deref(c.UnifiedDiff), Content: deref(c.Content), MovePath: deref(c.MovePath),
		})
	}
	for _, p := range e.Plan {
		out.Plan = append(out.Plan, contract.TranscriptPlanStep{Step: p.Step, Status: p.Status})
	}
	if a := e.Activity; a != nil {
		activity := &contract.TranscriptActivity{
			Kind: a.Kind, Title: deref(a.Title), Status: deref(a.Status), Result: deref(a.Result),
			DurationMs: -1, Actions: []contract.TranscriptAction{},
		}
		if a.DurationMs != nil {
			activity.DurationMs = *a.DurationMs
		}
		for _, x := range a.Actions {
			activity.Actions = append(activity.Actions, contract.TranscriptAction{
				Kind: x.Kind, Command: deref(x.Command), Name: deref(x.Name),
				Path: deref(x.Path), Query: deref(x.Query),
			})
		}
		out.Activity = activity
	}
	if n := e.Notice; n != nil {
		notice := &contract.TranscriptNotice{
			Kind: n.Kind, Audience: n.Audience, State: n.State, ResultPath: n.ResultPath,
			Outstanding: n.Outstanding, ClaimsReleased: n.ClaimsReleased,
			ChildMayStillWrite: n.ChildMayStillWrite, NoticeID: n.NoticeID, AckPath: n.AckPath,
			WaitID: n.WaitID, Repository: n.Repository, Paths: n.Paths,
			WaiterSessionID: n.WaiterSessionID, Reason: n.Reason, ReleaseCondition: n.ReleaseCondition,
			Commit: n.Commit, Note: n.Note, HandoffID: n.HandoffID, Assistant: n.Assistant,
			ProjectDir: n.ProjectDir, Title: n.Title,
		}
		if n.Task != nil {
			notice.Task = &contract.TranscriptNoticeTask{ID: n.Task.ID, Title: n.Task.Title}
		}
		for _, o := range n.Overlaps {
			notice.Overlaps = append(notice.Overlaps, contract.TranscriptNoticeOverlap{
				Path: o.Path, Task: contract.TranscriptNoticeTask{ID: o.Task.ID, Title: o.Task.Title},
			})
		}
		out.Notice = notice
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// infoPath recognises GET /v1/sessions/{id}/info and returns the id, decoded.
func infoPath(r *http.Request) (string, bool) {
	return sessionVerbIs(r, "info", http.MethodGet)
}

// sessionInfoRoute answers the status line under an open session: which model
// it is on and what it has spent, in the shape the Swift app's `/info` route
// writes them.
//
// It follows /v1/sessions: whichever daemon answers the list answers this, so
// an id always means the same session to both reads.
func (s *Server) sessionInfoRoute(w http.ResponseWriter, r *http.Request, id string) {
	if !s.ownsSessions() {
		s.forwardUpstream(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	item, err := s.actions().Find(ctx, id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	home, _ := os.UserHomeDir()
	info := contract.SessionInfo{
		Session: contract.SessionInfoSession{
			ID:        item.ID,
			Title:     item.Label,
			Assistant: contract.Assistant(item.Assistant),
			SessionID: item.ConversationID,
			CWD:       item.CWD,
		},
		Models: []contract.SessionModel{},
	}
	for _, m := range transcript.Models(home, string(item.Assistant)) {
		info.Models = append(info.Models, contract.SessionModel{ID: m.ID, Name: m.Name, Command: m.Command})
	}
	// Claude Code's status line writes one small file per session, and it
	// carries both the cost and the exact context window. It is read once
	// here: twice would be two answers about one moment.
	var status transcript.ClaudeStatusLine
	if item.Assistant == session.AssistantClaude {
		status = transcript.ReadClaudeStatusLine(home, item.ConversationID)
	}
	// No record, or one that cannot be read, leaves the model, the usage and
	// the context absent: a session whose record was not found has not spent
	// nothing, and it is not 0% full.
	if path := recordPath(item); path != "" {
		if facts, err := s.facts.Read(path, string(item.Assistant)); err == nil {
			info.Session.Model = facts.Model
			info.Usage = wireSessionUsage(facts.Usage, item, status)
			info.Context = wireSessionContext(facts, item.Assistant, status)
		}
	}
	info.Limits = s.sessionLimits(item.Assistant, time.Now())
	// Where this project can be opened, from the same maintained projection
	// the Links sheet reads (links.go), so the card, the sheet and the status
	// line cannot show one project three ways. The Swift app deferred these
	// out of `?parts=summary` because the walk cost it 350 ms on every
	// request; here the walk happens once per directory and a held reading is
	// handed back, so the summary carries them too — which is what puts the
	// chip under the conversation rather than only inside an opened card.
	if cwd := strings.TrimSpace(item.CWD); cwd != "" {
		reading, at := s.projectLinks(ctx, cwd)
		info.Links = wireLinks(reading.Links)
		info.Deploy = deployRows(info.Links)
		info.LinksObservedAt = seconds(at)
		info.Repository = contract.ProjectRepository(reading.Repo)
		info.RepositoryUnreadable = contract.ProjectGitFailure(reading.Unreadable)
		info.DeployQuiet = wireDeployQuiet(reading.DeployQuiet)
	}
	writeJSON(w, contract.SessionInfoReply{Info: info})
}

// wireSessionUsage carries a spend across, with the cost the Swift app would
// show: Claude Code's own session total when its status line wrote one down,
// otherwise the tokens at list price. The usage has to be there for either,
// as it does there — a total nobody could count is not replaced by a cost.
func wireSessionUsage(u *transcript.Summary, item session.Session, status transcript.ClaudeStatusLine) *contract.SessionInfoUsage {
	if u == nil {
		return nil
	}
	out := &contract.SessionInfoUsage{
		Input:      u.Input,
		Output:     u.Output,
		CacheRead:  u.CacheRead,
		CacheWrite: u.CacheWrite,
		Total:      u.Total,
		Model:      u.Model,
	}
	cost, known := u.Cost, u.HasCost
	if item.Assistant == session.AssistantClaude && status.HasCost {
		cost, known = status.CostUsd, true
	}
	if known {
		out.CostUsd = cost
	}
	return out
}

// wireSessionContext carries the context reading across, and is where a
// guessed window stops.
//
// `usedPercent` always goes: a percentage a client draws as one number is read
// as an approximation. `windowTokens` goes only when the assistant itself said
// how much fits, because `162,277 / 1,000,000 tokens` in a tooltip is read as
// a measurement, and only one of those two survives being wrong. The Swift
// app's `SessionInfo.infoPayload` draws the same line in the same place.
//
// Codex carries both sides in its own rollout; Claude's window comes from its
// status line's cache, and when that is absent from this build's estimate for
// the model — which is exactly the case this gate exists for.
func wireSessionContext(facts transcript.Facts, assistant session.Assistant, status transcript.ClaudeStatusLine) *contract.SessionInfoContext {
	at := facts.Context
	if assistant == session.AssistantClaude {
		if found, ok := transcript.ClaudeContext(facts.Fill, status, facts.Model); ok {
			at = &found
		}
	}
	if at == nil {
		return nil
	}
	out := &contract.SessionInfoContext{UsedPercent: at.UsedPercent}
	if at.HasUsedTokens {
		out.UsedTokens = at.UsedTokens
	}
	if at.WindowIsExact {
		out.WindowTokens = at.WindowTokens
	}
	return out
}
