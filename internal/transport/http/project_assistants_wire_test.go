package http

import (
	"net/http"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

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
