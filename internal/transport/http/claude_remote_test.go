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
	sends      int
	fail       bool
	reads      int
	images     int
	interrupts int
	selections int
	expect     string
	menu       *session.Menu
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

func (c *rcController) SendRemoteImages(ctx context.Context, row session.Session, text string, images []string, check func() error) error {
	c.images += len(images)
	return c.SendRemote(ctx, row, text, check)
}
func (c *rcController) InspectRemote(context.Context, session.Session) (*session.Menu, error) {
	c.reads++
	if c.fail {
		return nil, errors.New("unknown")
	}
	return c.menu, nil
}
func (c *rcController) SelectRemote(ctx context.Context, row session.Session, n int, expect string, check func() error) error {
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	c.selections++
	c.expect = expect
	return nil
}
func (c *rcController) InterruptRemote(ctx context.Context, row session.Session, check func() error) error {
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	c.interrupts++
	return nil
}
func (c *rcController) CloseRemote(context.Context, session.Session, func() error) error {
	return errors.New("not requested")
}
func TestRemoteControlHTTPImagesAndPermissionUseDurableReceipts(t *testing.T) {
	c := &rcController{menu: &session.Menu{Question: "Bash\nfixture", Revision: "fixture-native-permission", Numbered: true, Options: []session.MenuOption{{Number: 1, Label: "Allow once"}, {Number: 2, Label: "Deny"}}}}
	s, id := rcServer(t, c)
	for i := 0; i < 2; i++ {
		w := act(t, s, "send", id, "image-once", `{"text":"fixture","images":["fixture-image"]}`)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "accepted") {
			t.Fatal(w.Body.String())
		}
	}
	if c.images != 1 || c.sends != 1 {
		t.Fatal("image was retried or required pasteboard")
	}
	read := func(query string) contract.SessionInfoReply {
		w := httptest.NewRecorder()
		s.sessionInfoRoute(w, httptest.NewRequest("GET", "/v1/sessions/"+id+"/info"+query, nil), id)
		var reply contract.SessionInfoReply
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil {
			t.Fatal(w.Body.String())
		}
		return reply
	}
	if read("").Info.RemotePermissionKnown || c.reads != 0 {
		t.Fatal("routine info request posted a control event")
	}
	reply := read("?parts=remote-permission")
	if !reply.Info.RemotePermissionKnown || reply.Info.RemotePermission == nil || reply.Info.RemotePermissionExpect == "" {
		t.Fatal("permission offer missing")
	}
	body, _ := json.Marshal(contract.KeyRequest{Key: "1", Expect: reply.Info.RemotePermissionExpect})
	for i := 0; i < 2; i++ {
		w := act(t, s, "key", id, "permission-once", string(body))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "accepted") {
			t.Fatal(w.Body.String())
		}
	}
	if c.selections != 1 || c.expect != session.MenuFingerprint(*c.menu) {
		t.Fatal("answer lost fingerprint or was retried")
	}
	for i := 0; i < 2; i++ {
		w := act(t, s, "interrupt", id, "interrupt-once", `{}`)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	if c.interrupts != 1 {
		t.Fatal("interrupt was retried")
	}
	c.fail = true
	if read("?parts=remote-permission").Info.RemotePermissionKnown {
		t.Fatal("unknown permission treated as no pending request")
	}
}
func TestRemoteControlAutomationCannotApproveOrInterrupt(t *testing.T) {
	c := &rcController{}
	s, id := rcServer(t, c)
	_, err := s.actions().Key(context.Background(), id, "1", strings.Repeat("a", 64))
	if err == nil || c.selections != 0 {
		t.Fatal("automatic approval claimed human consent")
	}
	_, err = s.actions().Interrupt(context.Background(), id)
	if err == nil || c.interrupts != 0 {
		t.Fatal("automatic interrupt claimed human origin")
	}
	_, err = s.actions().SendWithPictures(context.Background(), id, "", []string{"fixture"})
	if err == nil || c.images != 0 {
		t.Fatal("automatic image claimed human origin")
	}
}

func TestRemoteControlListPresentationRetainsBackend(t *testing.T) {
	s, id := rcServer(t, &rcController{})
	w := httptest.NewRecorder()
	s.sessions(w, httptest.NewRequest("GET", "/v1/sessions?parts=list", nil))
	var body struct {
		Sessions []struct {
			ID      string `json:"id"`
			Backend string `json:"backend"`
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatal(w.Body.String())
	}
	for _, row := range body.Sessions {
		if row.ID == id {
			if row.Backend != "claude_remote" {
				t.Fatal("fallback lost Remote Control backend")
			}
			return
		}
	}
	t.Fatal("RC worker missing from fallback list")
}
