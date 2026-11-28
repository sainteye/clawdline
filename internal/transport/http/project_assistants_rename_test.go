package http

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"net/http"
	"reflect"
	"strings"
	"testing"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestProjectAssistantArtifactRename(t *testing.T) {
	f := libraryUIFixture(t)
	ctx := context.Background()
	repo := store.New(f.s.store.ProjectAssistantDatabase())
	before, err := repo.Export(ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	got := f.get(t, f.local)
	q := contract.ProjectAssistantRenameArtifactRequest{Command: f.command(pa.RenameArtifactOperation, "rename-one"), Fence: contract.ProjectAssistantFence{Key: got.Bot.Key, Project: got.Bot.Project, ConversationID: got.Conversation.ID, BindingID: got.Conversation.BindingID, Generation: got.Conversation.Generation}, ArtifactID: "library-artifact", ExpectedOutputID: "artifact-v2", ExpectedVersion: 1, Title: "  新作品名稱  "}
	path := "/v1/project-assistants/" + f.key.BotID + "/artifacts/" + q.ArtifactID + "/rename"
	send := func(q contract.ProjectAssistantRenameArtifactRequest, token string) contract.ProjectAssistantReceiptResponse {
		rec := f.request(t, "POST", path, token, q)
		paServerOK(t, rec)
		return paHTTPDecode[contract.ProjectAssistantReceiptResponse](t, rec)
	}
	for _, token := range []string{f.readonly} {
		rec := f.request(t, "POST", path, token, q)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("read-only: %d %s", rec.Code, rec.Body)
		}
	}
	first := send(q, f.local)
	if first.Receipt.State != "delivered" || first.Receipt.ResourceID != q.ArtifactID {
		t.Fatalf("receipt: %+v", first)
	}
	same := q
	same.Title = "新作品名稱"
	if replay := send(same, f.local); !reflect.DeepEqual(first, replay) {
		t.Fatal("canonical replay changed receipt")
	}
	conflict := q
	conflict.Title = "Different"
	paAssertHTTPFailure(t, f.request(t, "POST", path, f.local, conflict), http.StatusConflict, pa.IdempotencyConflict)
	after, err := repo.Export(ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Messages, after.Messages) {
		t.Fatal("immutable publications changed")
	}
	if after.ArtifactTitles[q.ArtifactID].Version != 2 {
		t.Fatal("title revision")
	}
	out, err := repo.Messages(ctx, app.HistoryQuery{Key: f.key, ConversationID: got.Conversation.ID, PageSize: 100, BeforeSeq: 5})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, m := range out.Items {
		if m.Output != nil && m.Output.ArtifactID == q.ArtifactID {
			count++
			if m.Output.Name != "新作品名稱" || m.Output.TitleVersion != 2 {
				t.Fatal("history title")
			}
		}
	}
	if count != 2 {
		t.Fatalf("history cards=%d", count)
	}
	read := f.request(t, "GET", paOutputFixturePath(f, before.Messages[2], 0, true), f.readonly, nil)
	paServerOK(t, read)
	value := paHTTPDecode[contract.ProjectAssistantOutputResponse](t, read)
	if value.Output.Name != "新作品名稱" || value.Output.ID != "artifact-v2" || value.Output.TitleVersion != 2 {
		t.Fatalf("read %+v", value.Output)
	}
	initial := paHTTPDecode[contract.ProjectAssistantReadViewResponse](t, f.request(t, "GET", paViewURL(f.key, got.Conversation.ID, pa.ReadViewSnapshot, "", "", 0, 100), f.local, nil))
	page := paHTTPDecode[contract.ProjectAssistantReadViewResponse](t, f.request(t, "GET", paViewURL(f.key, got.Conversation.ID, pa.ReadViewHistory, initial.SourceEpoch, initial.NextCursor, 0, 100), f.local, nil))
	for _, row := range page.Items {
		if row.Message != nil && row.Message.Output != nil && row.Message.Output.ArtifactID == q.ArtifactID && row.Message.Output.Name != "新作品名稱" {
			t.Fatal("read-view title")
		}
	}
	events := paHTTPDecode[contract.ProjectAssistantReadViewResponse](t, f.request(t, "GET", paViewURL(f.key, got.Conversation.ID, pa.ReadViewEvents, initial.SourceEpoch, "", 0, 100), f.local, nil))
	seen := false
	for _, row := range events.Items {
		if row.Event != nil && row.Event.Kind == "artifact_renamed" {
			seen = true
		}
	}
	if !seen {
		t.Fatal("missing replayable title event")
	}
	for _, tc := range []struct {
		name   string
		change func(*contract.ProjectAssistantRenameArtifactRequest)
		code   pa.ErrorCode
	}{
		{"stale title", func(r *contract.ProjectAssistantRenameArtifactRequest) {}, pa.StaleVersion},
		{"stale publication", func(r *contract.ProjectAssistantRenameArtifactRequest) {
			r.ExpectedVersion = 2
			r.ExpectedOutputID = "artifact-v1"
		}, pa.StaleVersion},
		{"scope", func(r *contract.ProjectAssistantRenameArtifactRequest) { r.Fence.Project.CanonicalID = "foreign" }, pa.ScopeDenied},
		{"generation", func(r *contract.ProjectAssistantRenameArtifactRequest) { r.Fence.Generation++ }, pa.StaleGeneration},
		{"empty", func(r *contract.ProjectAssistantRenameArtifactRequest) { r.Title = "  " }, pa.Invalid},
		{"bytes", func(r *contract.ProjectAssistantRenameArtifactRequest) { r.Title = strings.Repeat("字", 67) }, pa.Invalid},
		{"control", func(r *contract.ProjectAssistantRenameArtifactRequest) { r.Title = "a\nb" }, pa.Invalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := q
			q.Command = f.command(pa.RenameArtifactOperation, tc.name)
			tc.change(&q)
			rec := f.request(t, "POST", path, f.local, q)
			var answer contract.ProjectAssistantErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
				t.Fatal(err)
			}
			if string(answer.Error.Code) != string(tc.code) {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
		})
	}
	archive, err := repo.ExportArchive(ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	if archive.Data.ArtifactTitles[q.ArtifactID].Title != "新作品名稱" {
		t.Fatal("archive lost title")
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("rename invoked provider")
	}
}

func TestProjectAssistantArtifactRenameCloud(t *testing.T) {
	f := libraryUIFixture(t)
	got := f.get(t, f.local)
	pin := f.pin(t, "rename-controller", adaptercloud.ScopeOperate, false)
	public, _ := base64.StdEncoding.DecodeString(pin.PublicKey)
	bridge := cloudops.Bridge{MachineID: f.key.MachineID, Router: f.cloudRouter(), AllowCommands: func() bool { return true }}
	q := contract.ProjectAssistantRenameArtifactRequest{Command: f.command(pa.RenameArtifactOperation, "cloud-rename"), Fence: contract.ProjectAssistantFence{Key: got.Bot.Key, Project: got.Bot.Project, ConversationID: got.Conversation.ID, BindingID: got.Conversation.BindingID, Generation: got.Conversation.Generation}, ArtifactID: "library-artifact", ExpectedOutputID: "artifact-v2", ExpectedVersion: 1, Title: "Cloud title"}
	raw, _ := json.Marshal(q)
	fields := map[string]any{}
	_ = json.Unmarshal(raw, &fields)
	fields["type"] = "project-assistants.rename-artifact"
	fields["session"] = cloudops.MachineReplySession
	fields["request"] = q.Command.IdempotencyKey
	raw, _ = json.Marshal(fields)
	answer := bridge.Handle(context.Background(), cloudops.Command{Channel: "ctl/" + f.key.MachineID, Class: cloudops.ClassCtl, Sender: "rename-controller", VerifiedKey: ed25519.PublicKey(public), Sequence: 1, Plaintext: raw})
	if !answer.OK() {
		t.Fatalf("Cloud rename %d %s", answer.Status, answer.Payload)
	}
	x, err := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	if x.ArtifactTitles[q.ArtifactID].Title != q.Title {
		t.Fatal("Cloud rename not committed")
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("Cloud rename called provider")
	}
}
