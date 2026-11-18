package http

import (
	"net/url"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestTopicMemoryStoredHTTPKeepsBodyScopedAndRevisionChecked(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	got := f.get(t, f.local)
	entry := contract.ProjectAssistantMemoryEntry{ID: "topic", Key: got.Bot.Key, Project: got.Bot.Project, Kind: pa.TopicMemory, Title: "Release checklist", Summary: "Deployment readiness", Text: "BODY-ONLY-TOPIC-DETAIL", Revision: 1, Sources: []contract.ProjectAssistantMemorySource{}}
	write := contract.ProjectAssistantMemoryWriteRequest{Command: f.command(pa.MemorySaveOperation, "save-topic"), Entry: entry, ExpectedRevision: 1}
	path := "/v1/project-assistants/" + f.key.BotID + "/memory"
	if denied := f.request(t, "POST", path, f.readonly, write); denied.Code != 403 {
		t.Fatal("read-only outer gate admitted a memory write", denied.Code, denied.Body)
	}
	paAssertHTTPContract(t, f.request(t, "POST", path, f.local, write), 200, "ProjectAssistantMemoryResponse")
	list := f.request(t, "GET", memoryViewURL(f, got.Conversation.ID, nil), f.readonly, nil)
	paAssertHTTPContract(t, list, 200, "ProjectAssistantMemoryViewResponse")
	view := paHTTPDecode[contract.ProjectAssistantMemoryViewResponse](t, list)
	local := view.View.Layers[1]
	if len(local.Entries) != 1 || local.Entries[0].Text != entry.Summary || local.Entries[0].Title != entry.Title || local.Entries[0].Kind != pa.TopicMemory {
		t.Fatal("HTTP list did not expose a note index", local)
	}
	q := url.Values{"layer": {"bot"}, "entry_id": {"topic"}, "revision": {local.Revision}, "source_epoch": {view.SourceEpoch}}
	response := f.request(t, "GET", memoryViewURL(f, got.Conversation.ID, q), f.readonly, nil)
	paAssertHTTPContract(t, response, 200, "ProjectAssistantMemoryViewResponse")
	detail := paHTTPDecode[contract.ProjectAssistantMemoryViewResponse](t, response).Detail
	if detail == nil || detail.Text != entry.Text || detail.Title != entry.Title || detail.Kind != pa.TopicMemory {
		t.Fatal("HTTP detail did not return the scoped body", detail)
	}
	stale := write
	stale.Command = f.command(pa.MemorySaveOperation, "stale-topic")
	paAssertHTTPFailure(t, f.request(t, "POST", path, f.local, stale), 409, pa.StaleVersion)
	update := write
	update.Command = f.command(pa.MemorySaveOperation, "update-topic")
	update.ExpectedRevision = 2
	update.Entry.Text = "Updated body"
	paServerOK(t, f.request(t, "POST", path, f.local, update))
	paAssertHTTPFailure(t, f.request(t, "GET", memoryViewURL(f, got.Conversation.ID, q), f.readonly, nil), 409, pa.ContextStale)
	forget := contract.ProjectAssistantForgetRequest{Command: f.command(pa.ForgetOperation, "forget-topic"), Project: got.Bot.Project, ExpectedRevision: 3, SourceIds: []string{"topic"}}
	paServerOK(t, f.request(t, "POST", path+"/forget", f.local, forget))
	paAssertHTTPFailure(t, f.request(t, "GET", memoryViewURL(f, got.Conversation.ID, q), f.readonly, nil), 404, pa.NotFound)
}
