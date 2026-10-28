package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type rcController struct {
	sends int
	fail  bool
	reads int
}

func (c *rcController) SendRemote(ctx context.Context, s session.Session, text string, check func() error) error {
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	c.sends++
	if c.fail {
		return errors.New("Remote Control outcome unknown")
	}
	return nil
}
func (c *rcController) Page(ctx context.Context, s session.Session, limit int, before, after int64, revision string) (contract.TranscriptPage, error) {
	c.reads++
	return contract.TranscriptPage{ID: s.ID, Evidence: contract.EvidenceTranscript, Signature: "rc-fixture", Entries: []contract.TranscriptEntry{{Role: "error", Text: "Weekly quota exhausted"}}}, nil
}
func rcServer(t *testing.T, c *rcController) (*Server, string) {
	row := session.Session{ID: "claude-remote:42:cse_fixture", RemoteSessionID: "cse_fixture", PID: 42, Backend: session.BackendClaudeRemote, Assistant: session.AssistantClaude}
	s := paneServer(t, &pane{})
	s.claudeRemote = c
	s.inventory = app.Inventory{Process: rcProcesses{row}}
	return s, row.ID
}

type rcProcesses struct{ row session.Session }

func (p rcProcesses) Scan(context.Context) (session.Inventory, error) {
	return session.Inventory{Sessions: []session.Session{p.row}, Complete: true, Provenance: "ps"}, nil
}
func TestRemoteControlHTTPPreservesAcceptedAndUncertainReceipts(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "uncertain"}[fail], func(t *testing.T) {
			c := &rcController{fail: fail}
			s, id := rcServer(t, c)
			first := act(t, s, "send", id, "rc-send-key", `{"text":"fixture"}`)
			again := act(t, s, "send", id, "rc-send-key", `{"text":"fixture"}`)
			if first.Code != again.Code || first.Body.String() != again.Body.String() || c.sends != 1 {
				t.Fatalf("first=%d again=%d sends=%d", first.Code, again.Code, c.sends)
			}
			if fail {
				if first.Code != 500 {
					t.Fatal(first.Body.String())
				}
			} else {
				var result contract.ActionResult
				json.Unmarshal(first.Body.Bytes(), &result)
				if first.Code != 200 || result.Action != "accepted" {
					t.Fatal(first.Body.String())
				}
			}
		})
	}
}
func TestRemoteControlHTTPReadsQuotaWithoutALocalRecord(t *testing.T) {
	c := &rcController{}
	s, id := rcServer(t, c)
	req := httptest.NewRequest("GET", "/v1/transcript?session="+id, nil)
	w := httptest.NewRecorder()
	s.transcriptRoute(w, req)
	if w.Code != 200 || c.reads != 1 || !strings.Contains(w.Body.String(), "Weekly quota exhausted") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestRemoteControlBrokerMessagesDoNotClaimHumanOrigin(t *testing.T) {
	c := &rcController{}
	s, id := rcServer(t, c)
	_, err := s.actions().Send(context.Background(), id, "automatic broker message")
	var refusal app.Refusal
	if !errors.As(err, &refusal) || refusal.Code != "backend_unsupported" || c.sends != 0 {
		t.Fatalf("err=%v sends=%d", err, c.sends)
	}
}
