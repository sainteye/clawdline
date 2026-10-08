package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestPeerInboxHTTPRejectsUnpinnedExecutionBeforeReadingStoredBody(t *testing.T) {
	s := paneServer(t, &pane{s: session.Session{ID: "%19", Backend: session.BackendTmux,
		Assistant: session.AssistantClaude, State: session.StateIdle}})
	s.cfg.Dir = t.TempDir()
	s.executionMachineID = "target-machine"
	st, err := peerstore.Open(filepath.Join(s.cfg.Dir, cloudkeys.DirName))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const secret = "private peer inbox body"
	if err := st.PutInbox(context.Background(), peerstore.InboxItem{
		Request: agenthandoff.Request{RequestID: "peer-request", Target: agenthandoff.Endpoint{
			MachineID: "target-machine", SessionID: "%19",
			ExecutionGeneration: strings.Repeat("a", 32)}},
		Body: secret, At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	endpoint := "/v1/cloud/peer/inbox?machine_id=target-machine&session_id=" +
		url.QueryEscape("%19") + "&execution_generation=" + strings.Repeat("a", 32)
	for _, tc := range []struct {
		name, token string
		status      int
	}{
		{"unpaired viewer", "", http.StatusForbidden},
		{"paired reader with stale execution", "read", http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, endpoint, nil)
			if tc.token == "read" {
				req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{
					verdict: auth.Verdict{Allowed: true, Device: "reader", Caps: auth.NewCaps(auth.Read)},
				}))
			}
			rec := httptest.NewRecorder()
			s.cloudPeerInboxRoute(rec, req)
			if rec.Code != tc.status || strings.Contains(rec.Body.String(), secret) {
				t.Fatalf("inbox refusal: status=%d body=%q", rec.Code, rec.Body.String())
			}
		})
	}
}
