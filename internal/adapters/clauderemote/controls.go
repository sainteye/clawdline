package clauderemote

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type controlResponse struct {
	Subtype   string            `json:"subtype"`
	RequestID string            `json:"request_id"`
	Response  json.RawMessage   `json:"response"`
	Pending   []json.RawMessage `json:"pending_permission_requests"`
}

func (c *Client) control(ctx context.Context, s session.Session, subtype string, check func() error) (controlResponse, error) {
	var answer controlResponse
	ctx, cancel := context.WithTimeout(ctx, RequestSecondsLimit*time.Second)
	defer cancel()
	id := uuid.NewString()
	ack, err := c.submitAck(ctx, s, map[string]any{"type": "control_request", "request_id": id, "request": map[string]string{"subtype": subtype}}, check)
	if err != nil {
		return answer, err
	}
	a, err := c.auth(ctx)
	if err != nil {
		return answer, errors.New("Remote Control accepted the request but its response could not be read")
	}
	// Replay starts after the acknowledgement, so old conversation history cannot
	// crowd a current control response out of the bounded read.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.origin+"/v1/code/sessions/"+s.RemoteSessionID+"/events/stream?from_sequence_num="+strconv.FormatInt(ack.sequence, 10), nil)
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("x-organization-uuid", a.organization)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := c.http.Do(req)
	if err != nil {
		return answer, errors.New("Remote Control request was accepted but worker confirmation is unknown")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return answer, errors.New("Remote Control worker confirmation is unavailable")
	}
	scan := bufio.NewScanner(io.LimitReader(resp.Body, ResponseBytesLimit+1))
	scan.Buffer(nil, ResponseBytesLimit)
	used := 0
	for scan.Scan() {
		line := scan.Bytes()
		used += len(line) + 1
		if used > ResponseBytesLimit {
			break
		}
		if !strings.HasPrefix(string(line), "data:") {
			continue
		}
		var ev event
		if json.Unmarshal(line[len("data:"):], &ev) != nil || ev.Source != "worker" {
			continue
		}
		var payload struct {
			Type     string          `json:"type"`
			Response controlResponse `json:"response"`
		}
		if json.Unmarshal(ev.Payload, &payload) != nil || payload.Type != "control_response" || payload.Response.RequestID != id {
			continue
		}
		if !c.valid(ctx, s) {
			return answer, errors.New("Remote Control worker changed during confirmation")
		}
		if payload.Response.Subtype != "success" {
			return answer, errors.New("Remote Control worker refused the control request")
		}
		return payload.Response, nil
	}
	return answer, errors.New("Remote Control request was accepted but worker confirmation was not observed; do not retry automatically")
}
func (c *Client) InterruptRemote(ctx context.Context, s session.Session, check func() error) error {
	_, err := c.control(ctx, s, "interrupt", check)
	return err
}

// CloseRemote archives exactly one bridge, then proves the original process left.
// It never signals the launcher, a process group, or another worker.
func (c *Client) CloseRemote(ctx context.Context, s session.Session, check func() error) error {
	ctx, cancel := context.WithTimeout(ctx, RequestSecondsLimit*time.Second)
	defer cancel()
	if !c.valid(ctx, s) {
		return terminal.Unsent{Why: "Remote Control worker identity changed before close"}
	}
	a, err := c.auth(ctx)
	if err != nil {
		return terminal.Unsent{Why: err.Error()}
	}
	if check != nil {
		if err := check(); err != nil {
			return terminal.Unsent{Why: "Remote Control close permission was revoked"}
		}
	}
	if !c.valid(ctx, s) {
		return terminal.Unsent{Why: "Remote Control worker identity changed before close"}
	}
	_, err = c.request(ctx, http.MethodPost, "/v1/code/sessions/"+s.RemoteSessionID+"/archive", []byte(`{}`), a)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(ClosePollMillisLimit * time.Millisecond)
	defer ticker.Stop()
	for {
		gone, err := c.gone(ctx, s)
		if err != nil {
			return errors.New("Remote Control archive was accepted but worker exit could not be verified")
		}
		if gone {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("Remote Control archive was accepted but the worker is still present; do not retry automatically")
		case <-ticker.C:
		}
	}
}

type permission struct {
	ID      string      `json:"request_id"`
	Request toolRequest `json:"request"`
}
type toolRequest struct {
	Subtype   string          `json:"subtype"`
	Tool      string          `json:"tool_name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
}

func decodePermission(raw json.RawMessage) (permission, bool) {
	var p permission
	if json.Unmarshal(raw, &p) != nil {
		return p, false
	}
	if p.Request.Tool == "" {
		json.Unmarshal(raw, &p.Request)
	}
	if p.Request.Subtype == "" {
		p.Request.Subtype = "can_use_tool"
	}
	var input map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(p.Request.Input)))
	decoder.UseNumber()
	if p.ID == "" || p.Request.Subtype != "can_use_tool" || p.Request.Tool == "" || decoder.Decode(&input) != nil || input == nil || len(p.Request.Input) > PermissionBytesLimit {
		return p, false
	}
	p.Request.Input, _ = json.Marshal(input)
	return p, true
}
func permissionMenu(s session.Session, p permission) *session.Menu {
	sum := sha256.Sum256([]byte(s.RemoteSessionID + "\n" + s.ExpectedStart.Format(time.RFC3339Nano) + "\n" + p.ID + "\n" + p.Request.Tool + "\n" + string(p.Request.Input)))
	options := []session.MenuOption{{Number: 1, Label: "Allow once"}, {Number: 2, Label: "Deny"}}
	// AskUserQuestion needs structured answers, not an empty allow response.
	if p.Request.Tool == "AskUserQuestion" {
		options = options[1:]
	}
	return &session.Menu{Revision: "rcp1:" + hex.EncodeToString(sum[:]), Question: p.Request.Tool + "\n" + string(p.Request.Input), Options: options, Numbered: true}
}
func (c *Client) InspectRemote(ctx context.Context, s session.Session) (*session.Menu, error) {
	answer, err := c.control(ctx, s, "initialize", nil)
	if err != nil {
		return nil, err
	}
	for _, raw := range answer.Pending {
		p, ok := decodePermission(raw)
		if !ok {
			return nil, errors.New("Remote Control pending permission format is unsupported; use Claude")
		}
		return permissionMenu(s, p), nil
	}
	return nil, nil
}
func (c *Client) SelectRemote(ctx context.Context, s session.Session, digit int, expect string, check func() error) error {
	if digit != 1 && digit != 2 || !session.ValidFingerprint(expect) {
		return terminal.Unsent{Why: "Remote Control answer does not name an exact permission request"}
	}
	ctx, cancel := context.WithTimeout(ctx, RequestSecondsLimit*time.Second)
	defer cancel()
	answer, err := c.control(ctx, s, "initialize", check)
	if err != nil {
		return terminal.Unsent{Why: err.Error()}
	}
	for _, raw := range answer.Pending {
		p, ok := decodePermission(raw)
		if !ok {
			continue
		}
		menu := permissionMenu(s, p)
		if session.MenuFingerprint(*menu) != expect {
			continue
		}
		if digit == 1 && p.Request.Tool == "AskUserQuestion" {
			return terminal.Unsent{Why: "Use Claude to answer this structured question"}
		}
		response := map[string]any{"behavior": "deny", "message": "Denied by the user in Clawdline"}
		if digit == 1 {
			response = map[string]any{"behavior": "allow", "updatedInput": p.Request.Input}
		}
		wire := map[string]any{"subtype": "success", "request_id": p.ID, "response": response}
		if p.Request.ToolUseID != "" {
			wire["tool_use_id"] = p.Request.ToolUseID
		}
		return c.submit(ctx, s, map[string]any{"type": "control_response", "response": wire}, check)
	}
	return terminal.Unsent{Why: "Remote Control permission changed or was already answered; refresh before choosing again"}
}
