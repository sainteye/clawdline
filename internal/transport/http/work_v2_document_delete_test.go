package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheOwningSessionDeletesAnOlderPlanOverTheAgentRoute(t *testing.T) {
	s, p, project := sessionItemServer(t)
	created := personItem(t, s, project, "feature", "doc-delete-created")
	if _, err := s.assignWorkV2(context.Background(), created.ID, "local", created.Version,
		"existing_session", p.s.ID, "", "", nil); err != nil {
		t.Fatal(err)
	}
	call := func(path, body, key string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodPost, path, body, key))
		return rec
	}
	base := "/v1/work/v2/agent/items/" + created.ID + "/documents"
	session := `"session_id":"` + p.s.ConversationID + `"`
	for n, key := range []string{"plan-one", "plan-two"} {
		rec := call(base, `{`+session+`,"role":"plan","title":"Plan","body":"round `+key+`","position":`+
			strings.Repeat("1", n+1)+`}`, key)
		if rec.Code != http.StatusCreated {
			t.Fatalf("plan %s: %d %s", key, rec.Code, rec.Body)
		}
	}
	full, err := s.workV2().Item(context.Background(), created.ID)
	if err != nil || len(full.Documents) != 2 {
		t.Fatalf("documents = %+v, %v", full.Documents, err)
	}
	older, latest := full.Documents[0].ID, full.Documents[1].ID
	if full.Documents[0].Body != "round plan-one" {
		older, latest = latest, older
	}

	kept := call(base+"/"+latest+"/delete", `{`+session+`}`, "delete-latest")
	if kept.Code == http.StatusOK || !strings.Contains(kept.Body.String(), "document_kept") {
		t.Fatalf("latest plan delete: %d %s", kept.Code, kept.Body)
	}
	first := call(base+"/"+older+"/delete", `{`+session+`}`, "delete-older")
	if first.Code != http.StatusOK {
		t.Fatalf("older plan delete: %d %s", first.Code, first.Body)
	}
	if again := call(base+"/"+older+"/delete", `{`+session+`}`, "delete-older"); again.Code != first.Code ||
		again.Body.String() != first.Body.String() {
		t.Fatalf("replay: %d %s; first %d %s", again.Code, again.Body, first.Code, first.Body)
	}
	after, err := s.workV2().Item(context.Background(), created.ID)
	if err != nil || len(after.Documents) != 1 || after.Documents[0].ID != latest {
		t.Fatalf("documents after delete = %+v, %v", after.Documents, err)
	}
	if gone := call(base+"/"+older+"/delete", `{`+session+`}`, "delete-older-again"); gone.Code == http.StatusOK ||
		!strings.Contains(gone.Body.String(), "document_not_found") {
		t.Fatalf("second delete with a new key: %d %s", gone.Code, gone.Body)
	}
}
