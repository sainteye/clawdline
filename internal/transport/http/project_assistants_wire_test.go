package http

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestProjectAssistantPermissionPublishesTheLiteralMemoryProposal(t *testing.T) {
	const fact = "The maintenance window starts on Wednesday."
	wire, err := paWirePermissionOffer(pa.PermissionOffer{
		Kind: pa.DecisionOfferKind, State: pa.PermissionResolved, SelectedOptionID: pa.MemorySaveOptionID,
		Decision: &pa.DecisionRequest{Question: pa.MemoryProposalQuestion, Memory: fact},
	})
	if err != nil {
		t.Fatal(err)
	}
	if wire.Decision == nil || wire.Decision.Memory != fact {
		t.Fatalf("permission omitted its proposed memory: %+v", wire.Decision)
	}
	if wire.State != "answered" || wire.SelectedOptionID != pa.MemorySaveOptionID {
		t.Fatalf("permission lost its saved outcome: %+v", wire)
	}
}

func TestProjectAssistantListPublishesDispatchedExecutionCount(t *testing.T) {
	f := newPAHTTPFixture(t)
	f.create(t)
	for _, tc := range []struct {
		executing int64
		want      string
	}{
		{0, `"executing":0`},
		{1, `"executing":1`},
	} {
		f.repo.list = pa.BotPage{Items: []pa.BotSummary{{
			Bot: f.repo.bot, ActivityAt: f.now,
			ReadCursor:     pa.ReadCursor{Key: f.key, ConversationID: f.repo.conversation.ID, ReaderID: f.authority.ReaderID},
			DispatchedWork: &pa.DispatchedWork{Running: 2, Executing: tc.executing},
		}}}
		rec := f.request(t, http.MethodGet, "/v1/project-assistants?machine_id="+f.key.MachineID+"&query=&cursor=&page_size=25", nil)
		paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantListResponse")
		if !bytes.Contains(rec.Body.Bytes(), []byte(tc.want)) {
			t.Errorf("list response omitted %s: %s", tc.want, rec.Body.Bytes())
		}
	}
}

func TestProjectAssistantRosterPublishesBackgroundWorkSeparatelyFromChat(t *testing.T) {
	f := newPAHTTPFixture(t)
	f.create(t)
	row := pa.BotSummary{
		Bot: f.repo.bot, Status: "idle", ActivityAt: f.now, ActiveWork: 4,
		ReadCursor: pa.ReadCursor{Key: f.key, ConversationID: f.repo.conversation.ID, ReaderID: f.authority.ReaderID},
	}
	read := func(work *pa.BackgroundWork) contract.ProjectAssistantBotSummary {
		t.Helper()
		row.BackgroundWork = work
		f.repo.list = pa.BotPage{Items: []pa.BotSummary{row}}
		rec := f.request(t, http.MethodGet, "/v1/project-assistants?machine_id="+f.key.MachineID+"&query=&cursor=&page_size=25", nil)
		paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantListResponse")
		items := paHTTPDecode[contract.ProjectAssistantListResponse](t, rec).Items
		if len(items) != 1 || items[0].Status != "idle" || items[0].ActiveWork != 4 {
			t.Fatalf("roster = %+v", items)
		}
		return items[0]
	}
	want := contract.ProjectAssistantBackgroundWork{Working: 1, Waiting: 1, NeedsInput: 1}
	if got := read(&pa.BackgroundWork{Working: 1, Waiting: 1, NeedsInput: 1}).BackgroundWork; got == nil || *got != want {
		t.Fatalf("background work = %+v, want %+v", got, want)
	}
	if got := read(nil).BackgroundWork; got != nil {
		t.Fatalf("Board work should not become background Bot activity: %+v", got)
	}
}
