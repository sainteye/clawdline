package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/productcopy"
)

// The voice copilot's prepared briefing (docs/voice.md, "Voice copilot").
//
// A call start asks for it beside the client secret. The machine reads its
// own Sessions through the same handlers /v1/sessions and /v1/transcript
// answer with, so what the assistant is given is what a console would show,
// and has the chosen assistant write one spoken briefing in order of urgency.

const (
	// copilotBriefingSessions bounds how many Sessions one briefing reads.
	copilotBriefingSessions = 10
	// copilotBriefingFresh is how long a prepared briefing is handed out again.
	copilotBriefingFresh      = 60 * time.Second
	copilotBriefingAsks       = 4
	copilotBriefingDeliveries = 5
	copilotBriefingTextRunes  = 300
	// maxBriefingReplyRunes is how much of each Session's last reply the
	// briefing turn reads. Its beginning says what was found and its end what
	// the person is asked to decide, so a longer reply loses its middle.
	maxBriefingReplyRunes = 1500
)

// copilotBriefingSessionsOf reads the Sessions; tests replace it.
var copilotBriefingSessionsOf = func(s *Server, ctx context.Context) ([]copilotBriefingSession, error) {
	return s.copilotBriefingInput(ctx)
}

// copilotBriefRun is the turn; tests replace it.
var copilotBriefRun = func(ctx context.Context, input, assistant, language string) (string, error) {
	return planner.New().Briefing(ctx, input, assistant, language)
}

type copilotBriefingAnswer struct {
	Briefing   string `json:"briefing"`
	PreparedAt int64  `json:"prepared_at"`
	Sessions   int    `json:"sessions"`
	Assistant  string `json:"assistant"`
	Millis     int64  `json:"ms"`
	// Language is the console language it was written for; another console
	// gets its own.
	Language string `json:"language,omitempty"`
}

// copilotBriefingSession is what the assistant is told about one Session.
type copilotBriefingSession struct {
	Name       string   `json:"name"`
	Assistant  string   `json:"assistant,omitempty"`
	State      string   `json:"state"`
	WorkState  string   `json:"work_state,omitempty"`
	Waiting    string   `json:"waiting_question,omitempty"`
	Options    []string `json:"waiting_options,omitempty"`
	Requests   []string `json:"requests,omitempty"`
	Deliveries []string `json:"deliveries,omitempty"`
	LastReply  string   `json:"last_reply,omitempty"`
	// Recap is the Session's own plain recap of its whole conversation
	// (docs/session-recaps.md); RecapStale says the conversation moved on
	// after it was written.
	Recap      string `json:"recap,omitempty"`
	RecapStale bool   `json:"recap_stale,omitempty"`
	id         string
	LastActive string `json:"last_active,omitempty"`
	// Progress is the Session's Board steps when the briefing was prepared;
	// a delivery's own count may be hours old (copilot_recap.go).
	Progress *copilotRecapProgress `json:"progress,omitempty"`
}

var copilotReportCommand = regexp.MustCompile(`clawdline\s+session\s+report\b[^\n]*?--summary\s+(?:"((?:[^"\\]|\\.)*)"|'([^']*)')`)

var copilotUnescape = regexp.MustCompile(`\\(.)`)

// clipEnds keeps a text's beginning and end within n runes.
func clipEnds(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	head := (n + 1) / 2
	return string(r[:head]) + " … " + string(r[len(r)-(n-head):])
}

func clipRunes(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// whenPhrase says when something happened as a person says it, so the
// briefing never reads out a count of minutes: 剛剛, 12 分鐘前, 3 小時前,
// 昨天 18:20, 11/9 14:30. It is the browser's whenPhrase (copilot-tools.ts).
func whenPhrase(at, now time.Time) string {
	d := now.Sub(at)
	switch {
	case d < 90*time.Second:
		return "剛剛"
	case d < time.Hour:
		return fmt.Sprintf("%d 分鐘前", int(d.Round(time.Minute)/time.Minute))
	}
	at, now = at.Local(), now.Local()
	y1, m1, d1 := at.Date()
	y2, m2, d2 := now.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return fmt.Sprintf("%d 小時前", int((d+30*time.Minute)/time.Hour))
	}
	if yy, ym, yd := now.AddDate(0, 0, -1).Date(); y1 == yy && m1 == ym && d1 == yd {
		return "昨天 " + at.Format("15:04")
	}
	return fmt.Sprintf("%d/%d %s", int(m1), d1, at.Format("15:04"))
}

// copilotLanguage is the console language a voice request names, resolved to
// a shipped catalog, or the machine's product language when it names none.
func (s *Server) copilotLanguage(language string) string {
	if strings.TrimSpace(language) == "" {
		return s.productLanguage()
	}
	return productcopy.Resolve(language)
}

// copilotOwnRead answers one of this daemon's own read routes in-process.
func copilotOwnRead(ctx context.Context, handler http.HandlerFunc, target string, into any) bool {
	req := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec.Code == http.StatusOK && json.Unmarshal(rec.Body.Bytes(), into) == nil
}

type copilotRow struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Assistant   string `json:"assistant"`
	State       string `json:"state"`
	WorkState   string `json:"work_state"`
	Disposition *struct {
		Title string `json:"title"`
	} `json:"disposition"`
	Menu *struct {
		Question string `json:"question"`
		Options  []struct {
			Label string `json:"label"`
		} `json:"options"`
	} `json:"menu"`
	Activity *struct {
		At    int64 `json:"at"`
		Known bool  `json:"known"`
	} `json:"activity"`
	// Recap is the Session's forked recap (docs/session-recaps.md), when the
	// machine writes them.
	Recap *struct {
		Text    string `json:"text"`
		Current bool   `json:"current"`
	} `json:"recap"`
}

func copilotUrgency(r copilotRow) int {
	switch {
	case r.State == "waiting" || r.Menu != nil:
		return 0
	case r.State == "working":
		return 1
	case r.WorkState == "milestone_complete":
		return 2
	}
	return 3
}

// copilotBriefingInput reads the Sessions this briefing is about.
func (s *Server) copilotBriefingInput(ctx context.Context) ([]copilotBriefingSession, error) {
	sessions, err := copilotBriefingFrom(func(target string, into any) bool {
		handler := http.HandlerFunc(s.sessions)
		if strings.HasPrefix(target, "/v1/transcript") {
			handler = s.transcriptRoute
		}
		return copilotOwnRead(ctx, handler, target, into)
	})
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for i := range sessions {
		if p := s.copilotRecapProgressFor(ctx, sessions[i].id, "", now); p.Unavailable == "" && (p.TotalSteps > 0 || p.TodosDone+p.TodosOpen > 0) {
			sessions[i].Progress = &p
		}
	}
	return sessions, nil
}

// copilotBriefingFrom builds the briefing's input from two reads: the
// Session list and each Session's newest transcript page.
func copilotBriefingFrom(read func(target string, into any) bool) ([]copilotBriefingSession, error) {
	var list struct {
		Sessions []copilotRow `json:"sessions"`
	}
	if !read("/v1/sessions", &list) {
		return nil, errors.New("the Session list could not be read")
	}
	rows := list.Sessions
	sort.SliceStable(rows, func(i, j int) bool { return copilotUrgency(rows[i]) < copilotUrgency(rows[j]) })
	if len(rows) > copilotBriefingSessions {
		rows = rows[:copilotBriefingSessions]
	}
	now := time.Now().Unix()
	out := make([]copilotBriefingSession, 0, len(rows))
	for _, r := range rows {
		b := copilotBriefingSession{Name: r.Label, Assistant: r.Assistant, State: r.State, WorkState: r.WorkState, id: r.ID}
		if b.Name == "" {
			b.Name = r.ID
		}
		if r.Recap != nil && strings.TrimSpace(r.Recap.Text) != "" {
			b.Recap, b.RecapStale = r.Recap.Text, !r.Recap.Current
		}
		if r.Menu != nil {
			b.Waiting = clipRunes(r.Menu.Question, copilotBriefingTextRunes)
			for _, o := range r.Menu.Options {
				b.Options = append(b.Options, clipRunes(o.Label, 80))
			}
		}
		if r.Activity != nil && r.Activity.Known && r.Activity.At > 0 && r.Activity.At <= now {
			b.LastActive = whenPhrase(time.Unix(r.Activity.At, 0), time.Unix(now, 0))
		}
		var page struct {
			Entries []struct {
				Role string `json:"role"`
				Text string `json:"text"`
			} `json:"entries"`
		}
		if read("/v1/transcript?session="+url.QueryEscape(r.ID), &page) {
			for _, e := range page.Entries {
				switch e.Role {
				case "user":
					t := strings.TrimSpace(e.Text)
					if t != "" && !strings.HasPrefix(t, "<") {
						b.Requests = append(b.Requests, clipRunes(t, copilotBriefingTextRunes))
					}
				case "tool":
					if m := copilotReportCommand.FindStringSubmatch(e.Text); m != nil {
						summary := m[1]
						if summary == "" {
							summary = m[2]
						}
						summary = copilotUnescape.ReplaceAllString(summary, "$1")
						if strings.TrimSpace(summary) != "" {
							b.Deliveries = append(b.Deliveries, clipRunes(summary, copilotBriefingTextRunes))
						}
					}
				}
			}
			for i := len(page.Entries) - 1; i >= 0; i-- {
				if page.Entries[i].Role == "assistant" && strings.TrimSpace(page.Entries[i].Text) != "" {
					b.LastReply = clipEnds(page.Entries[i].Text, maxBriefingReplyRunes)
					break
				}
			}
		}
		if r.Disposition != nil && strings.TrimSpace(r.Disposition.Title) != "" {
			title := clipRunes(r.Disposition.Title, copilotBriefingTextRunes)
			if len(b.Deliveries) == 0 || b.Deliveries[len(b.Deliveries)-1] != title {
				b.Deliveries = append(b.Deliveries, title)
			}
		}
		if len(b.Requests) > copilotBriefingAsks {
			b.Requests = b.Requests[len(b.Requests)-copilotBriefingAsks:]
		}
		if len(b.Deliveries) > copilotBriefingDeliveries {
			b.Deliveries = b.Deliveries[len(b.Deliveries)-copilotBriefingDeliveries:]
		}
		out = append(out, b)
	}
	return out, nil
}

// copilotBriefingWait is how long a request waits for a briefing being
// prepared before it is answered `pending` and asks again. It is shorter than
// a Cloud action's wait, so a slow turn never looks like a lost request.
var copilotBriefingWait = 10 * time.Second

type copilotBriefingPending struct {
	Pending bool  `json:"pending"`
	Since   int64 `json:"since"`
	Retry   int   `json:"retry_after_ms"`
}

var copilotBriefing struct {
	sync.Mutex
	held     copilotBriefingAnswer
	running  chan struct{}
	started  time.Time
	lastErr  error
	lastCode string
}

// copilotBriefingRoute is POST /v1/voice/copilot-briefing. The first request
// starts one briefing in the background; every request waits for it at most
// copilotBriefingWait and is then answered `pending` with when to ask again.
// A briefing prepared in the last copilotBriefingFresh is handed out again.
func (s *Server) copilotBriefingRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use POST.")
		return
	}
	settings, err := s.readCopilot()
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "copilot_settings_unreadable", "The voice copilot settings on this machine cannot be read.")
		return
	}
	var body struct {
		Language string `json:"language"`
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
	_ = json.Unmarshal(raw, &body) // an older phone sends {}; the machine's language answers it
	language := s.copilotLanguage(body.Language)
	e := settings.Effective()
	if e.BriefingOff {
		writeRefusal(w, http.StatusConflict, "copilot_briefing_off", "The prepared briefing is turned off in Settings › Voice copilot.")
		return
	}
	copilotBriefing.Lock()
	if held := copilotBriefing.held; held.Briefing != "" && held.Assistant == e.BriefingAssistant && held.Language == language &&
		time.Since(time.Unix(held.PreparedAt, 0)) < copilotBriefingFresh {
		copilotBriefing.Unlock()
		writeJSON(w, held)
		return
	}
	if copilotBriefing.running == nil {
		done := make(chan struct{})
		copilotBriefing.running, copilotBriefing.started = done, time.Now()
		copilotBriefing.lastErr, copilotBriefing.lastCode = nil, ""
		go s.prepareCopilotBriefing(e.BriefingAssistant, language, done)
	}
	done, started := copilotBriefing.running, copilotBriefing.started
	copilotBriefing.Unlock()

	select {
	case <-done:
	case <-time.After(copilotBriefingWait):
		writeJSONStatus(w, http.StatusAccepted, copilotBriefingPending{Pending: true, Since: started.Unix(), Retry: 4000})
		return
	case <-r.Context().Done():
		return
	}
	copilotBriefing.Lock()
	held, err, code := copilotBriefing.held, copilotBriefing.lastErr, copilotBriefing.lastCode
	copilotBriefing.Unlock()
	if err != nil {
		switch code {
		case "copilot_briefing_no_assistant":
			writeRefusal(w, http.StatusConflict, code, "The assistant chosen for the briefing is not installed on this machine.")
		case "copilot_briefing_timeout":
			writeRefusal(w, http.StatusGatewayTimeout, code, "The briefing took too long; the copilot reads the Sessions itself.")
		case "copilot_briefing_unread":
			writeRefusal(w, http.StatusServiceUnavailable, code, "This machine could not read its Sessions for the briefing.")
		default:
			writeRefusal(w, http.StatusBadGateway, "copilot_briefing_failed", "The briefing could not be prepared.")
		}
		return
	}
	writeJSON(w, held)
}

// prepareCopilotBriefing runs one briefing; it outlives the request that
// started it, so a phone that asks again is handed the answer.
func (s *Server) prepareCopilotBriefing(assistant, language string, done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), planner.MaxBriefingDuration+10*time.Second)
	defer cancel()
	started := time.Now()
	answer, code, err := func() (copilotBriefingAnswer, string, error) {
		sessions, err := copilotBriefingSessionsOf(s, ctx)
		if err != nil {
			return copilotBriefingAnswer{}, "copilot_briefing_unread", err
		}
		input, err := planner.BriefingInput(sessions)
		if err != nil {
			return copilotBriefingAnswer{}, "copilot_briefing_failed", err
		}
		text, err := copilotBriefRun(ctx, input, assistant, language)
		switch {
		case errors.Is(err, planner.ErrNoPlanner):
			return copilotBriefingAnswer{}, "copilot_briefing_no_assistant", err
		case errors.Is(err, planner.ErrBriefingTimeout):
			return copilotBriefingAnswer{}, "copilot_briefing_timeout", err
		case err != nil:
			return copilotBriefingAnswer{}, "copilot_briefing_failed", err
		}
		return copilotBriefingAnswer{Briefing: text, PreparedAt: time.Now().Unix(), Sessions: len(sessions),
			Assistant: assistant, Millis: time.Since(started).Milliseconds(), Language: language}, "", nil
	}()
	copilotBriefing.Lock()
	if err == nil {
		copilotBriefing.held = answer
	}
	copilotBriefing.lastErr, copilotBriefing.lastCode = err, code
	copilotBriefing.running = nil
	copilotBriefing.Unlock()
	close(done)
}
