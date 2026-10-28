package clauderemote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/process"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Client uses the controller surface, never the worker's ingress credential.
// The origin is fixed; neither a browser request nor provider data supplies it.
type Client struct {
	http   *http.Client
	auth   func(context.Context) (credentials, error)
	verify func(context.Context, session.Session) bool
	origin string
}

func New() *Client {
	home, _ := os.UserHomeDir()
	return &Client{http: &http.Client{Timeout: RequestSecondsLimit * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		auth: func(ctx context.Context) (credentials, error) { return localCredentials(ctx, home) }, verify: process.VerifyRemoteControl, origin: "https://api.anthropic.com"}
}
func (c *Client) valid(ctx context.Context, s session.Session) bool {
	return s.RemoteSessionID != "" && process.NormalizeRemoteID(s.RemoteSessionID) == s.RemoteSessionID && c.verify != nil && c.verify(ctx, s)
}
func (c *Client) request(ctx context.Context, method, path string, body []byte, a credentials) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.origin+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("Remote Control request could not be prepared")
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("x-organization-uuid", a.organization)
	req.Header.Set("anthropic-version", "2023-06-01")
	if method == http.MethodPost {
		// Required by the native controller protocol to distinguish a person's
		// submitted prompt from a peer-agent message held by the peer approval gate.
		req.Header.Set("anthropic-client-platform", "web_claude_ai")
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("Remote Control request outcome is unknown; check the conversation before sending again")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("Remote Control returned HTTP " + strconv.Itoa(resp.StatusCode) + "; no automatic retry was attempted")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, ResponseBytesLimit+1))
	if err != nil || len(raw) > ResponseBytesLimit {
		return nil, errors.New("Remote Control response could not be verified; no automatic retry was attempted")
	}
	return raw, nil
}

// SendRemote accepts one text event. An acknowledgement proves server acceptance,
// not worker delivery or model execution. All ambiguous outcomes forbid replay.
func (c *Client) SendRemote(ctx context.Context, s session.Session, text string, check func() error) error {
	ctx, cancel := context.WithTimeout(ctx, RequestSecondsLimit*time.Second)
	defer cancel()
	unsent := func(why string) error { return terminal.Unsent{Why: why} }
	if text == "" || !c.valid(ctx, s) {
		return unsent("Remote Control worker identity changed or is unavailable")
	}
	a, err := c.auth(ctx)
	if err != nil {
		return unsent(err.Error())
	}
	id := uuid.NewString()
	body, _ := json.Marshal(map[string]any{"session_id": s.RemoteSessionID, "events": []any{map[string]any{"payload": map[string]any{"type": "user", "uuid": id, "message": map[string]any{"role": "user", "content": []any{map[string]string{"type": "text", "text": text}}}}}}})
	if check != nil {
		if err := check(); err != nil {
			return unsent("Remote Control send permission is no longer available")
		}
	}
	if !c.valid(ctx, s) {
		return unsent("Remote Control worker identity changed before submission")
	}
	raw, err := c.request(ctx, http.MethodPost, "/v1/code/sessions/"+s.RemoteSessionID+"/events", body, a)
	if err != nil {
		return err
	}
	var ack struct {
		Results []struct {
			EventID  string `json:"event_id"`
			Sequence string `json:"sequence_num"`
		} `json:"results"`
		Attestation json.RawMessage `json:"attestation_feedback"`
	}
	if json.Unmarshal(raw, &ack) != nil || len(ack.Results) != 1 || ack.Results[0].EventID != id {
		return errors.New("Remote Control acceptance could not be verified; check the conversation before sending again")
	}
	seq, e := strconv.ParseInt(ack.Results[0].Sequence, 10, 64)
	if e != nil || seq <= 0 || len(ack.Attestation) > 0 && string(ack.Attestation) != "null" {
		return errors.New("Remote Control acceptance needs verification in Claude; do not resend automatically")
	}
	return nil
}

type event struct {
	Sequence string          `json:"sequence_num"`
	ID       string          `json:"event_id"`
	Source   string          `json:"source"`
	Created  string          `json:"created_at"`
	Payload  json.RawMessage `json:"payload"`
}

// Page reads persisted native events when a worker has no local JSONL. Cursors
// are provider sequence numbers, scoped to this bridge and process generation.
// This is one bounded read, with no polling, retry, credential refresh or cache.
func (c *Client) Page(ctx context.Context, s session.Session, limit int, before, after int64, revision string) (contract.TranscriptPage, error) {
	page := contract.TranscriptPage{ID: s.ID, Entries: []contract.TranscriptEntry{}, Signature: "rc1:" + s.RemoteSessionID + ":" + s.ExpectedStart.Format(time.RFC3339Nano)}
	if revision != "" && revision != page.Signature {
		return page, transcript.ErrCursorStale
	}
	ctx, cancel := context.WithTimeout(ctx, RequestSecondsLimit*time.Second)
	defer cancel()
	if !c.valid(ctx, s) {
		return page, errors.New("Remote Control worker identity changed or is unavailable")
	}
	a, err := c.auth(ctx)
	if err != nil {
		return page, err
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}, "sort_order": {"desc"}}
	if before > 0 {
		q.Set("cursor", strconv.FormatInt(before, 10))
	}
	if after >= 0 {
		q.Set("sort_order", "asc")
		q.Set("cursor", strconv.FormatInt(after, 10))
	}
	raw, err := c.request(ctx, http.MethodGet, "/v1/code/sessions/"+s.RemoteSessionID+"/events?"+q.Encode(), nil, a)
	if err != nil {
		return page, err
	}
	var wire struct {
		Data []event         `json:"data"`
		Next json.RawMessage `json:"next_cursor"`
	}
	if json.Unmarshal(raw, &wire) != nil || wire.Data == nil || len(wire.Data) > limit {
		return page, errors.New("Remote Control history response is incompatible")
	}
	sort.Slice(wire.Data, func(i, j int) bool {
		x, _ := strconv.ParseInt(wire.Data[i].Sequence, 10, 64)
		y, _ := strconv.ParseInt(wire.Data[j].Sequence, 10, 64)
		return x < y
	})
	last := int64(0)
	for _, ev := range wire.Data {
		seq, e := strconv.ParseInt(ev.Sequence, 10, 64)
		if e != nil || seq <= last || before > 0 && seq >= before || after >= 0 && seq <= after {
			return page, errors.New("Remote Control history cursor is incompatible")
		}
		last = seq
		// A client cannot author an assistant reply in this projection.
		var payload struct {
			Type     string `json:"type"`
			APIError bool   `json:"is_api_error_message"`
		}
		if json.Unmarshal(ev.Payload, &payload) != nil {
			return page, errors.New("Remote Control event is incompatible")
		}
		if payload.Type != "user" && ev.Source != "worker" {
			continue
		}
		for i, e := range transcript.ClaudeEventEntries(ev.Payload) {
			if len(page.Entries) >= limit {
				return page, errors.New("Remote Control page exceeds its row budget; read this conversation in Claude")
			}
			role := e.Kind
			if role == transcript.KindToolResult {
				role = transcript.KindTool
			}
			if payload.APIError {
				role = transcript.KindError
			}
			at := e.At
			if at == 0 {
				if t, err := time.Parse(time.RFC3339Nano, ev.Created); err == nil {
					at = t.Unix()
				}
			}
			page.Entries = append(page.Entries, contract.TranscriptEntry{ID: "rc:" + ev.ID + ":" + strconv.Itoa(i), Role: role, Text: e.Text, Tool: e.Tool, At: at, ImageCount: int64(e.ImageCount), Source: e.Source, SourceMode: e.SourceMode, SourceAssistant: e.SourceAssistant})
		}
	}
	if !c.valid(ctx, s) {
		return page, errors.New("Remote Control worker identity changed during history read")
	}
	page.Evidence = contract.EvidenceTranscript
	if len(wire.Data) > 0 {
		page.NextAfter = last
	}
	if after < 0 && len(wire.Next) > 0 && string(wire.Next) != "null" {
		var cursor string
		if json.Unmarshal(wire.Next, &cursor) != nil {
			cursor = string(wire.Next)
		}
		n, e := strconv.ParseInt(cursor, 10, 64)
		if e != nil || n <= 0 || len(wire.Data) == 0 {
			return page, errors.New("Remote Control older-history cursor is incompatible")
		}
		page.NextBefore = n
	}
	return page, nil
}
