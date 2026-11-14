package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type paHTTPRepo struct {
	app.Repository
	bot            pa.Bot
	conversation   pa.Conversation
	receipts       map[string]pa.Receipt
	commits        int
	reads          int
	botReads       int
	historyReads   int
	observations   int
	exports        int
	reader         string
	query          pa.ListBots
	history        app.HistoryQuery
	read           pa.ReadCursor
	observeCommand pa.Command
	snapshot       *app.Export
	page           app.MessagePage
	list           pa.BotPage
	err            error
	afterRead      func()
}

func (r *paHTTPRepo) Bot(context.Context, pa.BotKey) (pa.Bot, error) {
	r.botReads++
	if r.afterRead != nil {
		r.afterRead()
	}
	return r.bot, r.err
}
func (r *paHTTPRepo) Conversation(context.Context, pa.BotKey, string) (pa.Conversation, error) {
	return r.conversation, r.err
}
func (r *paHTTPRepo) Receipt(_ context.Context, c pa.Command) (*pa.Receipt, error) {
	if prior, ok := r.receipts[string(c.Operation)+c.IdempotencyKey]; ok {
		return &prior, r.err
	}
	return nil, r.err
}
func (r *paHTTPRepo) Commit(_ context.Context, mutation app.Mutation) (pa.Receipt, error) {
	if r.err != nil {
		return pa.Receipt{}, r.err
	}
	r.commits++
	if mutation.Bot != nil {
		r.bot = *mutation.Bot
	}
	if mutation.Conversation != nil {
		r.conversation = *mutation.Conversation
	}
	receipt := mutation.Receipt
	receipt.Command = mutation.Command
	receipt.EventSeq = int64(r.commits)
	r.receipts[string(mutation.Command.Operation)+mutation.Command.IdempotencyKey] = receipt
	return receipt, nil
}
func (r *paHTTPRepo) ListForReader(_ context.Context, query pa.ListBots, reader string) (pa.BotPage, error) {
	r.reader, r.query = reader, query
	return r.list, r.err
}
func (r *paHTTPRepo) Messages(_ context.Context, query app.HistoryQuery) (app.MessagePage, error) {
	r.historyReads++
	r.history = query
	return r.page, r.err
}
func (r *paHTTPRepo) MarkRead(_ context.Context, command pa.Command, cursor pa.ReadCursor) (pa.ReadCursor, error) {
	r.reads++
	r.read = cursor
	return cursor, r.err
}
func (r *paHTTPRepo) Observe(_ context.Context, command pa.Command, receipt string, seq int64) (pa.Receipt, error) {
	r.observations++
	r.observeCommand = command
	return pa.Receipt{Command: command, ID: receipt, EventSeq: seq, State: pa.EffectObserved}, r.err
}
func (r *paHTTPRepo) Export(context.Context, pa.BotKey) (app.Export, error) {
	r.exports++
	if r.snapshot != nil {
		return *r.snapshot, r.err
	}
	memory := pa.MemoryRevision{Key: r.bot.Key, Project: r.bot.Project, Revision: 1}
	return app.Export{ContractVersion: pa.ContractVersion, SchemaVersion: pa.SchemaVersion, Bot: r.bot, Conversation: r.conversation, Memory: memory, Context: pa.ContextSnapshot{Memory: memory}}, r.err
}

type paHTTPRuntime struct {
	app.Runtime
	submissions []app.Submission
	stops       []app.StopRequest
	answers     []app.AnswerRequest
	err         error
}

func (r *paHTTPRuntime) Submit(_ context.Context, request app.Submission) (pa.Receipt, error) {
	r.submissions = append(r.submissions, request)
	return pa.Receipt{Command: request.Command, ID: "receipt-send", State: pa.EffectAccepted}, r.err
}
func (r *paHTTPRuntime) Stop(_ context.Context, request app.StopRequest) (pa.Receipt, error) {
	r.stops = append(r.stops, request)
	return pa.Receipt{Command: request.Command, ID: "receipt-stop", State: pa.EffectAccepted}, r.err
}
func (r *paHTTPRuntime) Answer(_ context.Context, request app.AnswerRequest) (pa.Receipt, error) {
	r.answers = append(r.answers, request)
	return pa.Receipt{Command: request.Command, ID: "receipt-answer", State: pa.EffectAccepted}, r.err
}

type paHTTPMemoryPort struct {
	app.Memory
	saves   []app.MemoryWrite
	forgets []app.ForgetRequest
	err     error
}

func (m *paHTTPMemoryPort) Save(_ context.Context, request app.MemoryWrite) (pa.MemoryRevision, error) {
	m.saves = append(m.saves, request)
	return pa.MemoryRevision{Key: request.Command.Key, Project: request.Entry.Project, Revision: request.ExpectedRevision + 1}, m.err
}
func (m *paHTTPMemoryPort) Forget(_ context.Context, request app.ForgetRequest) (pa.MemoryRevision, error) {
	m.forgets = append(m.forgets, request)
	return pa.MemoryRevision{Key: request.Command.Key, Project: request.Project, Revision: request.ExpectedRevision + 1, ForgottenThrough: request.ExpectedRevision, ForgottenSourceIDs: request.SourceIDs}, m.err
}

type paHTTPEventPort struct {
	app.EventFeed
	query pa.EventQuery
	page  pa.EventPage
	err   error
	reads int
}

func (e *paHTTPEventPort) Read(_ context.Context, query pa.EventQuery) (pa.EventPage, error) {
	e.reads++
	e.query = query
	return e.page, e.err
}

type paHTTPProjects struct {
	app.ProjectResolver
	resolution pa.ProjectResolution
	calls      int
}

func (p *paHTTPProjects) Resolve(context.Context, pa.ProjectSelection) (pa.ProjectResolution, error) {
	p.calls++
	return p.resolution, nil
}

type paHTTPFixture struct {
	service     *app.Service
	handler     http.Handler
	repo        *paHTTPRepo
	runtime     *paHTTPRuntime
	memory      *paHTTPMemoryPort
	events      *paHTTPEventPort
	projects    *paHTTPProjects
	authority   app.Authority
	source      pa.SourceStatus
	statusErr   error
	statusCalls int
	key         pa.BotKey
	project     pa.ProjectScope
	now         time.Time
	ids         int
}

func newPAHTTPFixture(t *testing.T) *paHTTPFixture {
	t.Helper()
	f := &paHTTPFixture{repo: &paHTTPRepo{receipts: map[string]pa.Receipt{}}, runtime: &paHTTPRuntime{}, memory: &paHTTPMemoryPort{}, events: &paHTTPEventPort{}, projects: &paHTTPProjects{}, key: pa.BotKey{MachineID: "machine-a", BotID: "bot-a"}, now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	f.project = pa.ProjectScope{MachineID: f.key.MachineID, ProjectID: "project-a", CanonicalID: "repo-a", Kind: pa.RepositoryProject}
	f.source = pa.SourceStatus{MachineID: f.key.MachineID, State: pa.SourceOnline, Readable: true, Writable: true, Fresh: true, Access: pa.Interactive, ObservedAt: f.now}
	f.authority = app.Authority{PrincipalID: "trusted-principal", ReaderID: "trusted-reader", AuthorizationRevision: "grant-a", Feature: pa.FeatureState{Enabled: true}, Source: f.source}
	f.projects.resolution = pa.ProjectResolution{Scope: f.project, Directory: "/synthetic/project", Current: true, Available: true}
	var err error
	f.service, err = app.NewService(app.ServiceOptions{Repository: f.repo, Memory: f.memory, Events: f.events, Runtime: f.runtime, Projects: f.projects, Authorize: func(context.Context, pa.BotKey, bool) (app.Authority, error) { return f.authority, nil }, Clock: func() time.Time { return f.now }, ID: func() string { f.ids++; return fmt.Sprintf("id-%d", f.ids) }})
	if err != nil {
		t.Fatal(err)
	}
	f.handler = newProjectAssistantHTTP(projectAssistantHTTPOptions{Service: f.service, MachineID: f.key.MachineID, Status: func(context.Context) (pa.SourceStatus, error) { f.statusCalls++; return f.source, f.statusErr }})
	return f
}
func (f *paHTTPFixture) command(operation pa.Operation) contract.ProjectAssistantCommand {
	return contract.ProjectAssistantCommand{Key: paWireBotKey(f.key), Operation: contract.ProjectAssistantOperation(operation), IdempotencyKey: "command-" + string(operation), ContentHash: "untrusted-client-hash"}
}
func (f *paHTTPFixture) createRequest() contract.ProjectAssistantCreateRequest {
	return contract.ProjectAssistantCreateRequest{Command: f.command(pa.CreateOperation), Settings: contract.ProjectAssistantCreateSettings{Target: paWireProjectSelection(pa.ProjectSelection{MachineID: f.key.MachineID, ProjectID: f.project.ProjectID}), Name: "Synthetic Bot", Appearance: paWireAppearance(pa.Appearance{Shape: pa.Blob, Color: pa.EntryColor}), Agent: contract.ProjectAssistantAgentCodex, PermissionPolicy: contract.ProjectAssistantPermissionPolicyAsk, Instructions: "Synthetic instructions"}}
}
func (f *paHTTPFixture) request(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
		var command struct {
			Command contract.ProjectAssistantCommand `json:"command"`
		}
		if err = json.Unmarshal(raw, &command); err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Idempotency-Key", command.Command.IdempotencyKey)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	return rec
}
func (f *paHTTPFixture) create(t *testing.T) {
	t.Helper()
	rec := f.request(t, http.MethodPost, "/v1/project-assistants", f.createRequest())
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantBotResponse")
}
func (f *paHTTPFixture) fence() contract.ProjectAssistantFence {
	return paWireFence(pa.Fence{Key: f.key, Project: f.project, ConversationID: f.repo.conversation.ID, BindingID: f.repo.conversation.BindingID, Generation: f.repo.conversation.Generation})
}
func (f *paHTTPFixture) effects() int {
	return f.repo.commits + f.repo.reads + f.repo.observations + len(f.runtime.submissions) + len(f.runtime.stops) + len(f.runtime.answers) + len(f.memory.saves) + len(f.memory.forgets)
}
func paHTTPDecode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	decoder := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}
	return value
}

// This checks observed wire values against the public schema, not source spelling.
func paAssertHTTPContract(t *testing.T, rec *httptest.ResponseRecorder, status int, name string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("HTTP=%d want %d: %s", rec.Code, status, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(rec.Header())
	}
	data, err := os.ReadFile("../../../api/v1/project-assistants.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Definitions map[string]map[string]any `json:"$defs"`
	}
	if err = json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	decoder.UseNumber()
	if err = decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	var check func(any, map[string]any, string)
	check = func(v any, s map[string]any, path string) {
		t.Helper()
		if ref, ok := s["$ref"].(string); ok {
			check(v, schema.Definitions[strings.TrimPrefix(ref, "#/$defs/")], path)
			return
		}
		switch s["type"] {
		case "object":
			object, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("%s is not an object: %#v", path, v)
			}
			props, _ := s["properties"].(map[string]any)
			for _, required := range s["required"].([]any) {
				if _, ok := object[required.(string)]; !ok {
					t.Fatalf("%s missing %s", path, required)
				}
			}
			for key, item := range object {
				property, ok := props[key]
				if !ok {
					t.Fatalf("%s unexpected %s", path, key)
				}
				check(item, property.(map[string]any), path+"."+key)
			}
		case "array":
			array, ok := v.([]any)
			if !ok {
				t.Fatalf("%s is not an array: %#v", path, v)
			}
			for i, item := range array {
				check(item, s["items"].(map[string]any), fmt.Sprintf("%s[%d]", path, i))
			}
		case "string":
			if _, ok := v.(string); !ok {
				t.Fatalf("%s is not string", path)
			}
		case "integer":
			n, ok := v.(json.Number)
			if !ok {
				t.Fatalf("%s is not number", path)
			}
			if _, err := n.Int64(); err != nil {
				t.Fatal(err)
			}
		case "boolean":
			if _, ok := v.(bool); !ok {
				t.Fatalf("%s is not bool", path)
			}
		default:
			t.Fatalf("unhandled schema at %s: %#v", path, s)
		}
		if values, ok := s["enum"].([]any); ok {
			found := false
			for _, allowed := range values {
				if reflect.DeepEqual(v, allowed) {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s enum value %#v", path, v)
			}
		}
	}
	check(value, schema.Definitions[name], name)
}

func paAssertHTTPFailure(t *testing.T, rec *httptest.ResponseRecorder, status int, code pa.ErrorCode) contract.ProjectAssistantFailure {
	t.Helper()
	paAssertHTTPContract(t, rec, status, "ProjectAssistantErrorResponse")
	answer := paHTTPDecode[contract.ProjectAssistantErrorResponse](t, rec)
	if answer.ContractVersion != pa.ContractVersion || string(answer.Error.Code) != string(code) {
		t.Fatalf("failure=%+v", answer)
	}
	return answer.Error
}

func TestProjectAssistantHTTPMetadataAndSnapshotRoutes(t *testing.T) {
	f := newPAHTTPFixture(t)
	rec := f.request(t, http.MethodGet, "/v1/project-assistants/status", nil)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantSourcesResponse")
	status := paHTTPDecode[contract.ProjectAssistantSourcesResponse](t, rec)
	if status.Sources[0].ObservedAt != "2026-01-02T03:04:05Z" || status.Sources[0].Capabilities == nil {
		t.Fatal(status)
	}
	f.create(t)
	rec = f.request(t, http.MethodGet, "/v1/project-assistants/bot-a?machine_id=machine-a", nil)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantGetResponse")
	get := paHTTPDecode[contract.ProjectAssistantGetResponse](t, rec)
	if get.Bot.CreatedAt != "2026-01-02T03:04:05Z" || get.Conversation.ID != f.repo.bot.ConversationID {
		t.Fatal(get)
	}
	f.repo.list = pa.BotPage{Items: []pa.BotSummary{{Bot: f.repo.bot, ActivityAt: f.now, ReadCursor: pa.ReadCursor{Key: f.key, ConversationID: f.repo.conversation.ID, ReaderID: f.authority.ReaderID}}}, NextCursor: "store-cursor"}
	rec = f.request(t, http.MethodGet, "/v1/project-assistants?machine_id=machine-a&query=needle&cursor=&page_size=25", nil)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantListResponse")
	list := paHTTPDecode[contract.ProjectAssistantListResponse](t, rec)
	if f.repo.reader != f.authority.ReaderID || f.repo.query.Query != "needle" || f.repo.query.PageSize != 25 || list.NextCursor == "store-cursor" || list.Items[0].Source.MachineID != f.key.MachineID {
		t.Fatal(list, f.repo.query)
	}
	if strings.Contains(rec.Body.String(), "native_session_id") {
		t.Fatal("roster leaked native identity")
	}
	rec = f.request(t, http.MethodPatch, "/v1/project-assistants/bot-a", contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: 1, Name: "Edited Bot", Appearance: f.createRequest().Settings.Appearance, Instructions: "Edited instructions"}})
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantBotResponse")
	if f.repo.bot.Version != 2 || f.repo.bot.Name != "Edited Bot" {
		t.Fatal(f.repo.bot)
	}
	f.repo.page = app.MessagePage{Items: []pa.Message{{ID: "message-a", Fence: paDomainFence(f.fence()), Seq: 9, Kind: pa.AssistantMessage, Text: "Synthetic reply", At: f.now}}, NextBeforeSeq: 9, HasMore: true}
	rec = f.request(t, http.MethodGet, "/v1/project-assistants/bot-a/messages?machine_id=machine-a&conversation_id="+f.repo.conversation.ID+"&before_seq=10&page_size=25", nil)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantHistoryResponse")
	history := paHTTPDecode[contract.ProjectAssistantHistoryResponse](t, rec)
	if history.NextBeforeSeq != 9 || !history.HasMore || history.Items[0].At != "2026-01-02T03:04:05Z" || f.repo.history.BeforeSeq != 10 {
		t.Fatal(history)
	}
	f.events.page = pa.EventPage{Items: []pa.Event{{Version: pa.ContractVersion, ID: "event-a", Seq: 12, Fence: paDomainFence(f.fence()), Kind: pa.FeatureDisabled, At: f.now}}, NextSeq: 12, HasMore: true}
	rec = f.request(t, http.MethodGet, "/v1/project-assistants/bot-a/events?machine_id=machine-a&after_seq=8&page_size=256", nil)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantEventsResponse")
	events := paHTTPDecode[contract.ProjectAssistantEventsResponse](t, rec)
	if events.NextSeq != 12 || !events.HasMore || f.events.query.AfterSeq != 8 || f.events.query.PageSize != pa.MaxEventPageRows {
		t.Fatal(events)
	}
	if len(f.runtime.submissions)+len(f.runtime.stops)+len(f.runtime.answers) != 0 {
		t.Fatal("metadata started runtime")
	}
	if f.projects.calls != 1 {
		t.Fatal("non-create metadata resolved an execution project")
	}
	// Projects belongs to the host's existing route and is not reimplemented.
	paAssertHTTPFailure(t, f.request(t, http.MethodGet, "/v1/projects", nil), http.StatusBadRequest, pa.Invalid)
}

func TestProjectAssistantHTTPWritesUseServiceAuthorityAndPorts(t *testing.T) {
	f := newPAHTTPFixture(t)
	f.create(t)
	send := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation), Fence: f.fence(), Text: "Synthetic input", QuoteMessageID: "quote-a"}
	rec := f.request(t, http.MethodPost, "/v1/project-assistants/bot-a/messages", send)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantReceiptResponse")
	if len(f.runtime.submissions) != 1 || f.runtime.submissions[0].Message.Text != send.Text || f.runtime.submissions[0].Message.QuoteMessageID != "quote-a" || f.runtime.submissions[0].Command.ContentHash == send.Command.ContentHash {
		t.Fatal(f.runtime.submissions)
	}
	rec = f.request(t, http.MethodPost, "/v1/project-assistants/bot-a/stop", contract.ProjectAssistantStopRequest{Command: f.command(pa.StopOperation), Fence: f.fence(), TurnID: "turn-a"})
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantReceiptResponse")
	rec = f.request(t, http.MethodPost, "/v1/project-assistants/bot-a/permissions/offer-a/answer", contract.ProjectAssistantPermissionAnswerRequest{Command: f.command(pa.AnswerOperation), Fence: f.fence(), OfferID: "offer-a", OptionID: "allow-a", TurnID: "turn-a"})
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantReceiptResponse")
	if len(f.runtime.stops) != 1 || f.runtime.stops[0].TurnID != "turn-a" || len(f.runtime.answers) != 1 || f.runtime.answers[0].Answer.OfferID != "offer-a" || f.runtime.answers[0].Answer.OptionID != "allow-a" {
		t.Fatal(f.runtime)
	}
	rec = f.request(t, http.MethodPost, "/v1/project-assistants/bot-a/read", contract.ProjectAssistantReadRequest{Command: f.command(pa.ReadOperation), Cursor: contract.ProjectAssistantReadCursor{Key: paWireBotKey(f.key), ConversationID: f.repo.conversation.ID, ReaderID: "forged-reader", MessageSeq: 9}})
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantReadResponse")
	read := paHTTPDecode[contract.ProjectAssistantReadResponse](t, rec)
	if read.Cursor.ReaderID != f.authority.ReaderID || f.repo.read.ReaderID != f.authority.ReaderID {
		t.Fatal(read, f.repo.read)
	}
	rec = f.request(t, http.MethodPost, "/v1/project-assistants/bot-a/observed", contract.ProjectAssistantObserveRequest{Command: f.command(pa.ObserveOperation), ReceiptID: "receipt-send", EventSeq: 12})
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantReceiptResponse")
	if f.repo.observations != 1 || f.repo.observeCommand.ContentHash == f.command(pa.ObserveOperation).ContentHash {
		t.Fatal("observation skipped canonical service")
	}
	entry := contract.ProjectAssistantMemoryEntry{ID: "memory-a", Key: paWireBotKey(f.key), Project: paWireProjectScope(f.project), Text: "Literal memory", Revision: 999, Forgotten: true, Sources: []contract.ProjectAssistantMemorySource{{ID: "forged-provenance", Key: paWireBotKey(f.key), Project: paWireProjectScope(f.project), Kind: "native", Revision: 999}}}
	rec = f.request(t, http.MethodPost, "/v1/project-assistants/bot-a/memory", contract.ProjectAssistantMemoryWriteRequest{Command: f.command(pa.MemorySaveOperation), ExpectedRevision: 1, Entry: entry})
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantMemoryResponse")
	saved := f.memory.saves[0].Entry
	if saved.Forgotten || saved.Revision != 1 || len(saved.Sources) != 1 || saved.Sources[0].ID != "manual:memory-a" || saved.Sources[0].Kind != "human" {
		t.Fatal(saved)
	}
	rec = f.request(t, http.MethodPost, "/v1/project-assistants/bot-a/memory/forget", contract.ProjectAssistantForgetRequest{Command: f.command(pa.ForgetOperation), Project: paWireProjectScope(f.project), ExpectedRevision: 2, SourceIds: []string{"manual:memory-a"}})
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantMemoryResponse")
	forgotten := paHTTPDecode[contract.ProjectAssistantMemoryResponse](t, rec)
	if forgotten.Memory.Revision != 3 || len(f.memory.forgets) != 1 || forgotten.Memory.ForgottenSourceIds[0] != "manual:memory-a" {
		t.Fatal(forgotten)
	}
	if len(f.runtime.submissions) != 1 || len(f.runtime.stops) != 1 || len(f.runtime.answers) != 1 {
		t.Fatal("non-runtime operation invoked runtime")
	}
}

func TestProjectAssistantHTTPExportKeepsOneCompleteSnapshot(t *testing.T) {
	f := newPAHTTPFixture(t)
	f.create(t)
	fence := paDomainFence(f.fence())
	failure := &pa.Failure{Code: pa.AdapterNotFound, Field: "adapter"}
	permission := pa.PermissionOffer{ID: "offer-a", Fence: fence, TurnID: "turn-a", NativeRequestID: "rpc-a", Options: []pa.PermissionOption{{ID: "once", Label: "Once", Allow: true, Kind: pa.AllowOnce}}, ExpiresAt: f.now, State: pa.PermissionPending}
	memory := pa.MemoryRevision{Key: f.key, Project: f.project, Revision: 4, ForgottenThrough: 2}
	f.repo.snapshot = &app.Export{ContractVersion: pa.ContractVersion, SchemaVersion: pa.SchemaVersion, Bot: f.repo.bot, Conversation: f.repo.conversation,
		Messages: []pa.Message{{ID: "message-a", Fence: fence, Kind: pa.AssistantMessage, Seq: 1, At: f.now, Text: "Synthetic export"}},
		Turns:    []pa.Turn{{ID: "turn-a", Fence: fence, State: pa.TurnFailed, Failure: failure}}, Bindings: []pa.ExecutionBinding{{Fence: fence, Agent: pa.Codex, NativeSessionID: "native-export-only"}}, Permissions: []pa.PermissionOffer{permission}, WorkLinks: []pa.WorkLink{{ID: "work-a", Fence: fence, State: pa.WorkLanded, LandingCommit: "synthetic-commit"}},
		Memory: memory, MemoryEntries: []pa.MemoryEntry{{ID: "entry-a", Key: f.key, Project: f.project, Text: "Synthetic memory"}}, Context: pa.ContextSnapshot{Memory: memory, Text: "Snapshot context", ProvenanceComplete: true},
		Events:   []pa.Event{{Version: pa.ContractVersion, ID: "event-offer", Seq: 2, Fence: fence, Kind: pa.PermissionOffered, Permission: &permission, At: f.now}, {Version: pa.ContractVersion, ID: "event-turn", Seq: 3, Fence: fence, Kind: pa.TurnUpdated, Turn: &pa.Turn{ID: "turn-a", Fence: fence, State: pa.TurnFailed, Failure: failure}, At: f.now}},
		Receipts: []pa.Receipt{{ID: "receipt-a", Command: paDomainCommand(f.command(pa.SendOperation)), State: pa.EffectUnknown}}, ReadCursors: []pa.ReadCursor{{Key: f.key, ReaderID: f.authority.ReaderID, ConversationID: f.repo.conversation.ID, MessageSeq: 1}}}
	f.repo.snapshot.Conversation.ID = "snapshot-conversation"
	f.repo.snapshot.Conversation.Version = 12
	before := f.repo.botReads
	rec := f.request(t, http.MethodGet, "/v1/project-assistants/bot-a/export?machine_id=machine-a", nil)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantExport")
	export := paHTTPDecode[contract.ProjectAssistantExport](t, rec)
	if export.ContractVersion != pa.ContractVersion || export.SchemaVersion != int64(pa.SchemaVersion) || export.Conversation.ID != "snapshot-conversation" || export.Conversation.Version != 12 || f.repo.botReads != before || f.repo.exports != 1 {
		t.Fatal(export.Conversation, "export was overwritten by Get")
	}
	if len(export.Turns) != 1 || len(export.Permissions) != 1 || len(export.WorkLinks) != 1 || len(export.Messages) != 1 || len(export.MemoryEntries) != 1 || len(export.Events) != 2 || len(export.Receipts) != 1 || len(export.ReadCursors) != 1 || export.Bindings[0].NativeSessionID != "native-export-only" || export.Context.Text != "Snapshot context" {
		t.Fatal("export lost data")
	}
	if export.Memory.ForgottenSourceIds == nil || export.MemoryEntries[0].Sources == nil || export.Context.Sources == nil || export.Events[0].Permission.Options == nil {
		t.Fatal("export contains null arrays")
	}
	if len(f.runtime.submissions)+len(f.runtime.answers)+len(f.runtime.stops) != 0 {
		t.Fatal("export invoked runtime")
	}
	f.repo.snapshot = nil
	rec = f.request(t, http.MethodGet, "/v1/project-assistants/bot-a/export?machine_id=machine-a", nil)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantExport")
	if strings.Contains(rec.Body.String(), ":null") {
		t.Fatal(rec.Body.String())
	}
}

func TestProjectAssistantHTTPStatusWithoutServiceAndHistoryOnly(t *testing.T) {
	f := newPAHTTPFixture(t)
	f.source.Access = pa.Hidden
	f.source.Readable = false
	f.source.Writable = false
	f.handler = newProjectAssistantHTTP(projectAssistantHTTPOptions{MachineID: f.key.MachineID, Status: func(context.Context) (pa.SourceStatus, error) { return f.source, nil }})
	rec := f.request(t, http.MethodGet, "/v1/project-assistants/status", nil)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantSourcesResponse")
	status := paHTTPDecode[contract.ProjectAssistantSourcesResponse](t, rec)
	if status.Sources[0].Access != contract.ProjectAssistantAccessHidden || status.Sources[0].Writable || status.Sources[0].Readable {
		t.Fatal(status)
	}
	paAssertHTTPFailure(t, f.request(t, http.MethodGet, "/v1/project-assistants?machine_id=machine-a&query=&cursor=&page_size=1", nil), http.StatusForbidden, pa.Disabled)
	paAssertHTTPFailure(t, f.request(t, http.MethodPost, "/v1/project-assistants", f.createRequest()), http.StatusForbidden, pa.Disabled)
	f = newPAHTTPFixture(t)
	f.create(t)
	f.authority.Feature = pa.FeatureState{HasData: true}
	f.authority.Source.Access = pa.HistoryOnly
	f.source.Access = pa.HistoryOnly
	rec = f.request(t, http.MethodGet, "/v1/project-assistants/bot-a/messages?machine_id=machine-a&conversation_id="+f.repo.conversation.ID+"&before_seq=0&page_size=1", nil)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantHistoryResponse")
	rec = f.request(t, http.MethodGet, "/v1/project-assistants/bot-a/export?machine_id=machine-a", nil)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantExport")
	paAssertHTTPFailure(t, f.request(t, http.MethodPost, "/v1/project-assistants/bot-a/messages", contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation), Fence: f.fence(), Text: "blocked"}), http.StatusForbidden, pa.Disabled)
	if len(f.runtime.submissions) != 0 {
		t.Fatal("disabled source executed")
	}
}

func TestProjectAssistantHTTPCreateReplayAndCanonicalConflict(t *testing.T) {
	f := newPAHTTPFixture(t)
	request := f.createRequest()
	first := f.request(t, http.MethodPost, "/v1/project-assistants", request)
	paAssertHTTPContract(t, first, http.StatusOK, "ProjectAssistantBotResponse")
	second := f.request(t, http.MethodPost, "/v1/project-assistants", request)
	paAssertHTTPContract(t, second, http.StatusOK, "ProjectAssistantBotResponse")
	if first.Body.String() != second.Body.String() || f.repo.commits != 1 {
		t.Fatal("idempotent create changed")
	}
	request.Settings.Name = "Changed intent"
	paAssertHTTPFailure(t, f.request(t, http.MethodPost, "/v1/project-assistants", request), http.StatusConflict, pa.IdempotencyConflict)
	if f.repo.commits != 1 || len(f.runtime.submissions) != 0 {
		t.Fatal("conflicting create had effects")
	}
}

type paHTTPWriteCase struct {
	name, method, path, response string
	body                         any
}

func (f *paHTTPFixture) writes() []paHTTPWriteCase {
	base := "/v1/project-assistants/bot-a"
	return []paHTTPWriteCase{
		{"create", http.MethodPost, "/v1/project-assistants", "ProjectAssistantBotResponse", f.createRequest()},
		{"edit", http.MethodPatch, base, "ProjectAssistantBotResponse", contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: 1, Name: "Edited", Appearance: f.createRequest().Settings.Appearance}}},
		{"send", http.MethodPost, base + "/messages", "ProjectAssistantReceiptResponse", contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation), Fence: f.fence(), Text: "Synthetic input"}},
		{"stop", http.MethodPost, base + "/stop", "ProjectAssistantReceiptResponse", contract.ProjectAssistantStopRequest{Command: f.command(pa.StopOperation), Fence: f.fence(), TurnID: "turn-a"}},
		{"answer", http.MethodPost, base + "/permissions/offer-a/answer", "ProjectAssistantReceiptResponse", contract.ProjectAssistantPermissionAnswerRequest{Command: f.command(pa.AnswerOperation), Fence: f.fence(), OfferID: "offer-a", OptionID: "allow", TurnID: "turn-a"}},
		{"read", http.MethodPost, base + "/read", "ProjectAssistantReadResponse", contract.ProjectAssistantReadRequest{Command: f.command(pa.ReadOperation), Cursor: contract.ProjectAssistantReadCursor{Key: paWireBotKey(f.key), ConversationID: f.repo.conversation.ID, ReaderID: "forged", MessageSeq: 2}}},
		{"observed", http.MethodPost, base + "/observed", "ProjectAssistantReceiptResponse", contract.ProjectAssistantObserveRequest{Command: f.command(pa.ObserveOperation), ReceiptID: "receipt-a", EventSeq: 2}},
		{"forget", http.MethodPost, base + "/memory/forget", "ProjectAssistantMemoryResponse", contract.ProjectAssistantForgetRequest{Command: f.command(pa.ForgetOperation), Project: paWireProjectScope(f.project), ExpectedRevision: 1, SourceIds: []string{}}},
		{"memory", http.MethodPost, base + "/memory", "ProjectAssistantMemoryResponse", contract.ProjectAssistantMemoryWriteRequest{Command: f.command(pa.MemorySaveOperation), ExpectedRevision: 1, Entry: contract.ProjectAssistantMemoryEntry{ID: "entry-a", Key: paWireBotKey(f.key), Project: paWireProjectScope(f.project), Text: "Synthetic memory", Sources: []contract.ProjectAssistantMemorySource{}}}},
	}
}

func TestProjectAssistantHTTPAllWritesRejectMalformedObjectsAndHeaders(t *testing.T) {
	f := newPAHTTPFixture(t)
	f.create(t)
	for _, operation := range f.writes() {
		t.Run(operation.name, func(t *testing.T) {
			raw, err := json.Marshal(operation.body)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				Command contract.ProjectAssistantCommand `json:"command"`
			}
			if err = json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			valid := string(raw)
			bodies := []struct {
				name, body string
				code       pa.ErrorCode
				status     int
			}{
				{"unknown", strings.TrimSuffix(valid, "}") + `,"unexpected":true}`, pa.Invalid, http.StatusBadRequest},
				{"nested-unknown", strings.Replace(valid, `"command":{`, `"command":{"capabilities":[],`, 1), pa.Invalid, http.StatusBadRequest},
				{"case", strings.Replace(valid, `"command":`, `"COMMAND":`, 1), pa.Invalid, http.StatusBadRequest},
				{"missing", strings.Replace(valid, `"content_hash":"untrusted-client-hash",`, "", 1), pa.Invalid, http.StatusBadRequest},
				{"duplicate", strings.TrimSuffix(valid, "}") + `,"command":` + string(mustPAHTTPJSON(t, decoded.Command)) + `}`, pa.Invalid, http.StatusBadRequest},
				{"null-command", strings.Replace(valid, string(mustPAHTTPJSON(t, decoded.Command)), "null", 1), pa.Invalid, http.StatusBadRequest},
				{"trailing", valid + ` {}`, pa.Invalid, http.StatusBadRequest},
				{"trailing-scalar", valid + ` true`, pa.Invalid, http.StatusBadRequest},
				{"null", `null`, pa.Invalid, http.StatusBadRequest},
				{"invalid-utf8", strings.Replace(valid, "untrusted-client-hash", string([]byte{0xff}), 1), pa.Invalid, http.StatusBadRequest},
				{"array", `[]`, pa.Invalid, http.StatusBadRequest},
				{"oversize", valid + strings.Repeat(" ", paBodyLimit(operation.name)), pa.CapacityFull, http.StatusTooManyRequests},
			}
			for _, test := range bodies {
				t.Run(test.name, func(t *testing.T) {
					before := f.effects()
					r := httptest.NewRequest(operation.method, operation.path, strings.NewReader(test.body))
					r.Header.Set("Idempotency-Key", decoded.Command.IdempotencyKey)
					rec := httptest.NewRecorder()
					f.handler.ServeHTTP(rec, r)
					paAssertHTTPFailure(t, rec, test.status, test.code)
					if f.effects() != before {
						t.Fatal("rejected body had effects")
					}
				})
			}
			for _, test := range []struct {
				name   string
				values []string
			}{{"missing-header", nil}, {"wrong-header", []string{"other"}}, {"duplicate-header", []string{decoded.Command.IdempotencyKey, decoded.Command.IdempotencyKey}}} {
				t.Run(test.name, func(t *testing.T) {
					before := f.effects()
					r := httptest.NewRequest(operation.method, operation.path, bytes.NewReader(raw))
					r.Header["Idempotency-Key"] = test.values
					rec := httptest.NewRecorder()
					f.handler.ServeHTTP(rec, r)
					paAssertHTTPFailure(t, rec, http.StatusBadRequest, pa.Invalid)
					if f.effects() != before {
						t.Fatal("rejected header had effects")
					}
				})
			}
		})
	}
	// The pre-existing body limit is inclusive, and transport bounds do not
	// become a smaller silent limit on valid settings.
	f = newPAHTTPFixture(t)
	request := f.createRequest()
	raw := mustPAHTTPJSON(t, request)
	raw = append(raw, bytes.Repeat([]byte(" "), commandBodyLimit-len(raw))...)
	r := httptest.NewRequest(http.MethodPost, "/v1/project-assistants", bytes.NewReader(raw))
	r.Header.Set("Idempotency-Key", request.Command.IdempotencyKey)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantBotResponse")
}

func mustPAHTTPJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestProjectAssistantHTTPRejectsScopeChangesBeforeEffects(t *testing.T) {
	for _, test := range []struct {
		name, operation string
		mutate          func(map[string]any)
		path            string
	}{
		{"command-machine", "create", func(v map[string]any) {
			v["command"].(map[string]any)["key"].(map[string]any)["machine_id"] = "machine-b"
		}, ""},
		{"target-machine", "create", func(v map[string]any) {
			v["settings"].(map[string]any)["target"].(map[string]any)["machine_id"] = "machine-b"
		}, ""},
		{"route-bot", "send", nil, "/v1/project-assistants/bot-b/messages"},
		{"fence-key", "send", func(v map[string]any) { v["fence"].(map[string]any)["key"].(map[string]any)["bot_id"] = "bot-b" }, ""},
		{"fence-machine", "stop", func(v map[string]any) {
			v["fence"].(map[string]any)["project"].(map[string]any)["machine_id"] = "machine-b"
		}, ""},
		{"offer-route", "answer", func(v map[string]any) { v["offer_id"] = "offer-b" }, ""},
		{"answer-key", "answer", func(v map[string]any) { v["fence"].(map[string]any)["key"].(map[string]any)["bot_id"] = "bot-b" }, ""},
		{"read-key", "read", func(v map[string]any) { v["cursor"].(map[string]any)["key"].(map[string]any)["bot_id"] = "bot-b" }, ""},
		{"forget-machine", "forget", func(v map[string]any) { v["project"].(map[string]any)["machine_id"] = "machine-b" }, ""},
		{"forget-project", "forget", func(v map[string]any) { v["project"].(map[string]any)["project_id"] = "project-b" }, ""},
		{"memory-key", "memory", func(v map[string]any) { v["entry"].(map[string]any)["key"].(map[string]any)["bot_id"] = "bot-b" }, ""},
		{"memory-machine", "memory", func(v map[string]any) {
			v["entry"].(map[string]any)["project"].(map[string]any)["machine_id"] = "machine-b"
		}, ""},
		{"memory-project", "memory", func(v map[string]any) {
			v["entry"].(map[string]any)["project"].(map[string]any)["canonical_id"] = "repo-b"
		}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newPAHTTPFixture(t)
			f.create(t)
			for _, operation := range f.writes() {
				if operation.name == test.operation {
					var body map[string]any
					if err := json.Unmarshal(mustPAHTTPJSON(t, operation.body), &body); err != nil {
						t.Fatal(err)
					}
					if test.mutate != nil {
						test.mutate(body)
					}
					path := operation.path
					if test.path != "" {
						path = test.path
					}
					before := f.effects()
					paAssertHTTPFailure(t, f.request(t, operation.method, path, body), http.StatusForbidden, pa.ScopeDenied)
					if f.effects() != before {
						t.Fatal("scope refusal had effects")
					}
					return
				}
			}
			t.Fatal("missing operation")
		})
	}
}

func TestProjectAssistantHTTPQueriesAreCompleteAndBounded(t *testing.T) {
	f := newPAHTTPFixture(t)
	f.create(t)
	queries := []string{
		"/v1/project-assistants/status?machine_id=machine-a",
		"/v1/project-assistants/bot-a?machine_id=machine-a&unknown=1",
		"/v1/project-assistants/bot-a?machine_id=machine-a&machine_id=machine-a",
		"/v1/project-assistants/bot-a?machine_id=%ZZ",
		"/v1/project-assistants?machine_id=machine-a&query=&cursor=",
		"/v1/project-assistants?machine_id=machine-a&query=&cursor=&page_size=1&extra=1",
	}
	for _, route := range []string{"list", "history", "events"} {
		for _, size := range []string{"0", "-1", "%2B1", "1.5", "x", "9223372036854775808", strconv.Itoa(pa.MaxEventPageRows + 1)} {
			path := "/v1/project-assistants?machine_id=machine-a&query=&cursor=&page_size=" + size
			if route == "history" {
				path = "/v1/project-assistants/bot-a/messages?machine_id=machine-a&conversation_id=" + f.repo.conversation.ID + "&before_seq=0&page_size=" + size
			}
			if route == "events" {
				path = "/v1/project-assistants/bot-a/events?machine_id=machine-a&after_seq=0&page_size=" + size
			}
			queries = append(queries, path)
		}
	}
	queries = append(queries, "/v1/project-assistants/bot-a/messages?machine_id=machine-a&conversation_id=x&before_seq=-1&page_size=1", "/v1/project-assistants/bot-a/events?machine_id=machine-a&after_seq=-1&page_size=1", "/v1/project-assistants/bot-a/export?machine_id=machine-a&machine_id=machine-a")
	for _, path := range queries {
		t.Run(path, func(t *testing.T) {
			paAssertHTTPFailure(t, f.request(t, http.MethodGet, path, nil), http.StatusBadRequest, pa.Invalid)
		})
	}
	for _, path := range []string{"/v1/project-assistants/bot-a?machine_id=machine-b", "/v1/project-assistants?machine_id=machine-b&query=&cursor=&page_size=1", "/v1/project-assistants/bot-a/export?machine_id=machine-b"} {
		paAssertHTTPFailure(t, f.request(t, http.MethodGet, path, nil), http.StatusForbidden, pa.ScopeDenied)
	}
	before := f.repo.historyReads
	paAssertHTTPFailure(t, f.request(t, http.MethodGet, "/v1/project-assistants/bot-a/messages?machine_id=machine-a&conversation_id=wrong&before_seq=0&page_size=1", nil), http.StatusForbidden, pa.ScopeDenied)
	if f.repo.historyReads != before {
		t.Fatal("wrong conversation reached history")
	}
	paAssertHTTPFailure(t, f.request(t, http.MethodGet, "/v1/project-assistants?machine_id=machine-a&query=&cursor=bad-cursor&page_size=1", nil), http.StatusGone, pa.CursorExpired)
}

func TestProjectAssistantHTTPFinalPublicationRechecksSource(t *testing.T) {
	for _, code := range []pa.ErrorCode{pa.Revoked, pa.ScopeDenied, pa.Disabled} {
		t.Run(string(code), func(t *testing.T) {
			f := newPAHTTPFixture(t)
			f.create(t)
			f.repo.bot.Name = "CONTENT MUST NOT BE PUBLISHED"
			f.repo.afterRead = func() {
				switch code {
				case pa.Revoked:
					f.source.State = pa.SourceRevoked
				case pa.ScopeDenied:
					f.source.Readable = false
				case pa.Disabled:
					f.source.Access = pa.Hidden
				}
			}
			rec := f.request(t, http.MethodGet, "/v1/project-assistants/bot-a?machine_id=machine-a", nil)
			paAssertHTTPFailure(t, rec, http.StatusForbidden, code)
			if strings.Contains(rec.Body.String(), "CONTENT MUST NOT BE PUBLISHED") {
				t.Fatal("revoked content escaped")
			}
		})
	}
	f := newPAHTTPFixture(t)
	f.create(t)
	publicationCalls := 0
	f.handler = newProjectAssistantHTTP(projectAssistantHTTPOptions{Service: f.service, MachineID: f.key.MachineID, Status: func(context.Context) (pa.SourceStatus, error) {
		publicationCalls++
		if publicationCalls == 2 {
			return pa.SourceStatus{}, &pa.Failure{Code: pa.Revoked, Field: "grant"}
		}
		return f.source, nil
	}})
	paAssertHTTPFailure(t, f.request(t, http.MethodGet, "/v1/project-assistants/bot-a/export?machine_id=machine-a", nil), http.StatusForbidden, pa.Revoked)
	if f.repo.exports != 1 {
		t.Fatal("did not exercise post-read revocation")
	}
	f = newPAHTTPFixture(t)
	calls := 0
	f.handler = newProjectAssistantHTTP(projectAssistantHTTPOptions{MachineID: f.key.MachineID, Status: func(context.Context) (pa.SourceStatus, error) {
		calls++
		source := f.source
		if calls == 2 {
			source.State = pa.SourceRevoked
			source.Access = pa.Hidden
			source.Readable = false
			source.Writable = false
		}
		return source, nil
	}})
	rec := f.request(t, http.MethodGet, "/v1/project-assistants/status", nil)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantSourcesResponse")
	if answer := paHTTPDecode[contract.ProjectAssistantSourcesResponse](t, rec); answer.Sources[0].State != contract.ProjectAssistantSourceStateRevoked || answer.Sources[0].Readable {
		t.Fatal("status published old metadata", answer)
	}
}

func TestProjectAssistantHTTPContentStatusRefusesBeforeEffects(t *testing.T) {
	for _, scenario := range []string{"revoked", "read-only", "missing-status", "cross-machine-status"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPAHTTPFixture(t)
			f.create(t)
			before := f.effects()
			code := pa.Revoked
			switch scenario {
			case "revoked":
				f.source.State = pa.SourceRevoked
			case "read-only":
				f.source.Writable = false
				code = pa.ReadOnly
			case "missing-status":
				f.handler = newProjectAssistantHTTP(projectAssistantHTTPOptions{Service: f.service, MachineID: f.key.MachineID})
				code = pa.Disabled
			case "cross-machine-status":
				f.source.MachineID = "machine-b"
				code = pa.ScopeDenied
			}
			paAssertHTTPFailure(t, f.request(t, http.MethodPost, "/v1/project-assistants/bot-a/messages", contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation), Fence: f.fence(), Text: "Synthetic input"}), http.StatusForbidden, code)
			if f.effects() != before {
				t.Fatal("host refusal had effects")
			}
		})
	}
	// A disabled module has no application service, but still serves truthful
	// host metadata. Every other implemented operation remains unavailable.
	f := newPAHTTPFixture(t)
	f.create(t)
	operations := f.writes()
	f.handler = newProjectAssistantHTTP(projectAssistantHTTPOptions{MachineID: f.key.MachineID, Status: func(context.Context) (pa.SourceStatus, error) { return f.source, nil }})
	for _, operation := range operations {
		paAssertHTTPFailure(t, f.request(t, operation.method, operation.path, operation.body), http.StatusForbidden, pa.Disabled)
	}
	for _, path := range []string{"/v1/project-assistants/bot-a?machine_id=machine-a", "/v1/project-assistants/bot-a/messages?machine_id=machine-a&conversation_id=x&before_seq=0&page_size=1", "/v1/project-assistants/bot-a/events?machine_id=machine-a&after_seq=0&page_size=1", "/v1/project-assistants/bot-a/export?machine_id=machine-a"} {
		paAssertHTTPFailure(t, f.request(t, http.MethodGet, path, nil), http.StatusForbidden, pa.Disabled)
	}
}

func TestProjectAssistantHTTPReaderMetadataUsesFreshReadableSource(t *testing.T) {
	for _, operation := range []string{"read", "observed"} {
		for _, scenario := range []struct {
			name   string
			code   pa.ErrorCode
			status int
		}{
			{"read-only", "", http.StatusOK},
			{"history-only", "", http.StatusOK},
			{"offline", pa.Offline, http.StatusServiceUnavailable},
			{"stale", pa.Offline, http.StatusServiceUnavailable},
			{"unreadable", pa.ScopeDenied, http.StatusForbidden},
			{"revoked", pa.Revoked, http.StatusForbidden},
		} {
			t.Run(operation+"/"+scenario.name, func(t *testing.T) {
				f := newPAHTTPFixture(t)
				f.create(t)
				f.source.Writable = false
				switch scenario.name {
				case "history-only":
					f.source.Access = pa.HistoryOnly
					f.authority.Feature = pa.FeatureState{HasData: true}
				case "offline":
					f.source.State = pa.SourceOffline
				case "stale":
					f.source.Fresh = false
				case "unreadable":
					f.source.Readable = false
				case "revoked":
					f.source.State = pa.SourceRevoked
				}
				f.authority.Source = f.source
				before := f.effects()
				for _, write := range f.writes() {
					if write.name == operation {
						rec := f.request(t, write.method, write.path, write.body)
						if scenario.code != "" {
							paAssertHTTPFailure(t, rec, scenario.status, scenario.code)
							if f.effects() != before {
								t.Fatal("unavailable metadata acknowledgement had effects")
							}
						} else {
							paAssertHTTPContract(t, rec, http.StatusOK, write.response)
							if f.effects() != before+1 {
								t.Fatal("reader metadata was not acknowledged exactly once")
							}
							if operation == "read" && f.repo.read.ReaderID != f.authority.ReaderID {
								t.Fatal("reader metadata trusted the supplied reader")
							}
						}
						if len(f.runtime.submissions)+len(f.runtime.stops)+len(f.runtime.answers) != 0 {
							t.Fatal("reader metadata invoked runtime")
						}
						return
					}
				}
				t.Fatal("missing metadata route")
			})
		}
	}
}

func TestProjectAssistantHTTPReaderHeadersCannotReplaceTheHostPrincipal(t *testing.T) {
	f := newPAHTTPFixture(t)
	f.create(t)
	body := contract.ProjectAssistantReadRequest{Command: f.command(pa.ReadOperation), Cursor: contract.ProjectAssistantReadCursor{Key: paWireBotKey(f.key), ConversationID: f.repo.conversation.ID, ReaderID: "forged-body-reader", MessageSeq: 2}}
	r := httptest.NewRequest(http.MethodPost, "/v1/project-assistants/bot-a/read", bytes.NewReader(mustPAHTTPJSON(t, body)))
	r.Header.Set("Idempotency-Key", body.Command.IdempotencyKey)
	for _, header := range []string{"X-Reader-ID", "X-Sender", "X-Account-ID", "X-Machine-ID"} {
		r.Header.Set(header, "forged-header-principal")
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantReadResponse")
	if f.repo.read.ReaderID != f.authority.ReaderID {
		t.Fatal("header/body replaced principal")
	}
}

func TestProjectAssistantHTTPPreservesTypedFailuresAndHidesRawErrors(t *testing.T) {
	for _, test := range []struct {
		code   pa.ErrorCode
		status int
	}{
		{pa.Invalid, 400}, {pa.Disabled, 403}, {pa.ReadOnly, 403}, {pa.NotFound, 404}, {pa.Offline, 503}, {pa.Unsupported, 501}, {pa.Unpaired, 403}, {pa.Revoked, 403}, {pa.StaleGeneration, 409}, {pa.StaleVersion, 409}, {pa.IdempotencyConflict, 409}, {pa.CapacityFull, 429}, {pa.OutcomeUnknown, 500}, {pa.Expired, 410}, {pa.ScopeDenied, 403}, {pa.ContextStale, 409}, {pa.CursorExpired, 410}, {pa.AdapterNotFound, 503}, {pa.CLINotInstalled, 503}, {pa.AdapterInventoryUnknown, 503}, {pa.ProviderVersionUnsupported, 501}, {pa.AuthRequired, 503}, {pa.AuthProbeUnknown, 503}, {pa.AuthRejected, 503}, {pa.AuthUnavailableInIsolatedRuntime, 503}, {pa.SandboxUnavailable, 503}, {pa.ReadOnlyUnverified, 503}, {pa.ScopeExpansionDenied, 403}, {pa.MemoryIsolationUnverified, 503}, {pa.ResumeForbiddenAfterForget, 503}, {pa.ResumeUnsupported, 501}, {pa.ExecutionEffectUnknown, 500},
	} {
		t.Run(string(test.code), func(t *testing.T) {
			f := newPAHTTPFixture(t)
			f.create(t)
			retry := int64(0)
			if test.code == pa.CapacityFull {
				retry = 7
			}
			f.runtime.err = fmt.Errorf("private native error wrapper: %w", &pa.Failure{Code: test.code, Field: "typed_field", RetryAfterSeconds: retry})
			rec := f.request(t, http.MethodPost, "/v1/project-assistants/bot-a/messages", contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation), Fence: f.fence(), Text: "Synthetic input"})
			failure := paAssertHTTPFailure(t, rec, test.status, test.code)
			if failure.Field != "typed_field" || failure.RetryAfterSeconds != retry {
				t.Fatal(failure)
			}
			if retry > 0 && rec.Header().Get("Retry-After") != "7" {
				t.Fatal("retry lost")
			}
			if strings.Contains(rec.Body.String(), "private native") {
				t.Fatal("raw error leaked")
			}
		})
	}
	f := newPAHTTPFixture(t)
	f.create(t)
	f.repo.err = errors.New("SECRET BROKER DIAGNOSTIC")
	rec := f.request(t, http.MethodGet, "/v1/project-assistants/bot-a/export?machine_id=machine-a", nil)
	failure := paAssertHTTPFailure(t, rec, http.StatusInternalServerError, pa.OutcomeUnknown)
	if failure.Field != "" || failure.RetryAfterSeconds != 0 || rec.Header().Get("Retry-After") != "" || strings.Contains(rec.Body.String(), "SECRET") {
		t.Fatal("unknown failure was retried, changed or exposed")
	}
	f.repo.err = &pa.Failure{Code: pa.OutcomeUnknown, Field: "effect", RetryAfterSeconds: 9}
	rec = f.request(t, http.MethodGet, "/v1/project-assistants/bot-a/export?machine_id=machine-a", nil)
	failure = paAssertHTTPFailure(t, rec, http.StatusInternalServerError, pa.OutcomeUnknown)
	if failure.RetryAfterSeconds != 9 || rec.Header().Get("Retry-After") != "" {
		t.Fatal("unknown evidence was lost or encouraged replay")
	}
	// A wire conversion failure is a typed refusal, never a partial success.
	f = newPAHTTPFixture(t)
	f.create(t)
	f.repo.bot.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	paAssertHTTPFailure(t, f.request(t, http.MethodGet, "/v1/project-assistants/bot-a?machine_id=machine-a", nil), http.StatusInternalServerError, pa.OutcomeUnknown)
}

func TestProjectAssistantHTTPSharedFixtures(t *testing.T) {
	data, err := os.ReadFile("../../domain/projectassistant/testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Cases []struct {
			ID string `json:"id"`
		} `json:"http_cases"`
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures.Cases {
		t.Run(fixture.ID, func(t *testing.T) {
			f := newPAHTTPFixture(t)
			switch fixture.ID {
			case "create-retry":
				f.create(t)
				f.create(t)
				if f.repo.commits != 1 {
					t.Fatal("duplicate Bot")
				}
			case "read-offline-cache":
				f.authority.Source.State = pa.SourceOffline
				f.authority.Source.Fresh = false
				f.source = f.authority.Source
				rec := f.request(t, http.MethodGet, "/v1/project-assistants?machine_id=machine-a&query=&cursor=&page_size=1", nil)
				paAssertHTTPContract(t, rec, http.StatusOK, "ProjectAssistantListResponse")
				answer := paHTTPDecode[contract.ProjectAssistantListResponse](t, rec)
				if answer.Sources[0].Fresh || answer.Sources[0].State != contract.ProjectAssistantSourceStateOffline {
					t.Fatal("offline cache became authoritative")
				}
			case "revoked-permission":
				f.create(t)
				f.authority.Source.State = pa.SourceRevoked
				paAssertHTTPFailure(t, f.request(t, http.MethodPost, "/v1/project-assistants/bot-a/permissions/offer-a/answer", contract.ProjectAssistantPermissionAnswerRequest{Command: f.command(pa.AnswerOperation), Fence: f.fence(), OfferID: "offer-a", OptionID: "allow", TurnID: "turn-a"}), http.StatusForbidden, pa.Revoked)
			case "disabled-history":
				f.create(t)
				f.authority.Feature = pa.FeatureState{HasData: true}
				f.authority.Source.Access = pa.HistoryOnly
				f.source.Access = pa.HistoryOnly
				paAssertHTTPContract(t, f.request(t, http.MethodGet, "/v1/project-assistants/bot-a/messages?machine_id=machine-a&conversation_id="+f.repo.conversation.ID+"&before_seq=0&page_size=1", nil), http.StatusOK, "ProjectAssistantHistoryResponse")
			case "native-adapter-missing":
				f.create(t)
				f.runtime.err = &pa.Failure{Code: pa.AdapterNotFound}
				paAssertHTTPFailure(t, f.request(t, http.MethodPost, "/v1/project-assistants/bot-a/messages", contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation), Fence: f.fence(), Text: "Synthetic input"}), http.StatusServiceUnavailable, pa.AdapterNotFound)
			default:
				t.Fatalf("new fixture needs acceptance: %s", fixture.ID)
			}
			if fixture.ID != "native-adapter-missing" && len(f.runtime.submissions) != 0 || len(f.runtime.stops) != 0 || len(f.runtime.answers) != 0 {
				t.Fatal("fixture unexpectedly invoked native response port")
			}
		})
	}
}

func TestProjectAssistantHTTPBotIDsRoundTripOrRefuseBeforeEffects(t *testing.T) {
	for _, test := range []struct {
		id   string
		safe bool
	}{
		{"status", false}, {".", false}, {"..", false}, {"nested/bot", false}, {`nested\bot`, false}, {"control\x00bot", false},
		{"bot-safe", true}, {"助理-α-🤖", true},
	} {
		t.Run(url.PathEscape(test.id), func(t *testing.T) {
			f := newPAHTTPFixture(t)
			f.key.BotID = test.id
			request := f.createRequest()
			request.Settings.Name = "Display name is independent: status / .."
			before := f.effects()
			create := f.request(t, http.MethodPost, "/v1/project-assistants", request)
			mux := http.NewServeMux()
			mux.Handle("/v1/project-assistants", f.handler)
			mux.Handle("/v1/project-assistants/", f.handler)
			if !test.safe {
				if create.Code == http.StatusOK {
					read := httptest.NewRecorder()
					mux.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/v1/project-assistants/"+url.PathEscape(test.id)+"?machine_id=machine-a", nil))
					t.Logf("unsafe create accepted, effects=%d; ServeMux GET=%d, response=%s", f.effects()-before, read.Code, read.Body.String())
				}
				failure := paAssertHTTPFailure(t, create, http.StatusBadRequest, pa.Invalid)
				if failure.Field != "bot_id" || f.effects() != before {
					t.Fatal("unsafe ID was not refused before effects", failure)
				}
				return
			}
			paAssertHTTPContract(t, create, http.StatusOK, "ProjectAssistantBotResponse")
			get := httptest.NewRecorder()
			mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/project-assistants/"+url.PathEscape(test.id)+"?machine_id=machine-a", nil))
			paAssertHTTPContract(t, get, http.StatusOK, "ProjectAssistantGetResponse")
			answer := paHTTPDecode[contract.ProjectAssistantGetResponse](t, get)
			if answer.Bot.Key.BotID != test.id || answer.Bot.Name != request.Settings.Name || f.effects() != before+1 {
				t.Fatal("safe ID or independent display name changed", answer.Bot)
			}
		})
	}
}

// paBodyLimit is the body bound of one write: a send carries pictures.
func paBodyLimit(operation string) int {
	if operation == "send" {
		return capacity.MaximumBotSendBodyBytes
	}
	return commandBodyLimit
}
