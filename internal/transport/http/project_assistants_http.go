package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The host mounts this handler behind its existing gate and supplies current
// trusted request context. Constructing it installs nothing and starts no runtime.
type projectAssistantHTTPOptions struct {
	Service   *app.Service
	MachineID string
	Status    func(context.Context) (pa.SourceStatus, error)
}

type projectAssistantHTTP struct{ options projectAssistantHTTPOptions }

func newProjectAssistantHTTP(options projectAssistantHTTPOptions) http.Handler {
	return &projectAssistantHTTP{options: options}
}

func paHTTPFailure(code pa.ErrorCode, field string) error {
	return &pa.Failure{Code: code, Field: field}
}

// This final check is a publication fence, not a cross-process transaction or
// evidence that an outer Cloud frame has been authenticated.
func (h *projectAssistantHTTP) status(ctx context.Context, content bool) (pa.SourceStatus, error) {
	if h.options.MachineID == "" || h.options.Status == nil {
		return pa.SourceStatus{}, paHTTPFailure(pa.Disabled, "host")
	}
	source, err := h.options.Status(ctx)
	if err != nil {
		return pa.SourceStatus{}, err
	}
	if source.MachineID != h.options.MachineID {
		return pa.SourceStatus{}, paHTTPFailure(pa.ScopeDenied, "source_machine")
	}
	if content {
		if err = pa.CheckSource(source, false); err != nil {
			return pa.SourceStatus{}, err
		}
		if source.Access == pa.Hidden {
			return pa.SourceStatus{}, paHTTPFailure(pa.Disabled, "source_feature")
		}
	}
	return source, nil
}

func (h *projectAssistantHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	payload, err := h.handle(w, r)
	if err != nil {
		paWriteHTTPFailure(w, err)
		return
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		paWriteHTTPFailure(w, err)
		return
	}
	if r.URL.Path == "/v1/project-assistants/status" {
		// Refresh the status payload itself rather than publishing metadata from
		// the earlier callback if access changed while it was being encoded.
		source, e := h.status(r.Context(), false)
		if e == nil {
			var wire contract.ProjectAssistantSourceStatus
			wire, e = paWireSourceStatus(source)
			if e == nil {
				encoded, e = json.Marshal(contract.ProjectAssistantSourcesResponse{ContractVersion: pa.ContractVersion, Sources: []contract.ProjectAssistantSourceStatus{wire}})
			}
		}
		if e != nil {
			paWriteHTTPFailure(w, e)
			return
		}
	} else if _, err = h.status(r.Context(), true); err != nil {
		paWriteHTTPFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(append(encoded, '\n'))
}

type paHTTPRoute struct{ bot, action, offer string }

func paHTTPSafeBotID(id string) bool {
	// IDs occupy exactly one URL path segment. The display name is separate.
	// Reserve the fixed status endpoint and reject path normalization syntax.
	if id == "" || id == "status" || id == "." || id == ".." || !utf8.ValidString(id) || strings.ContainsAny(id, `/\`) {
		return false
	}
	return !strings.ContainsFunc(id, unicode.IsControl)
}

func paRoute(r *http.Request) (paHTTPRoute, error) {
	const base = "/v1/project-assistants"
	if r.URL.Path == base {
		switch r.Method {
		case http.MethodGet:
			return paHTTPRoute{action: "list"}, nil
		case http.MethodPost:
			return paHTTPRoute{action: "create"}, nil
		}
	}
	if strings.HasPrefix(r.URL.Path, base+"/") {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, base+"/"), "/")
		bot := parts[0]
		if bot != "" {
			if bot == "status" && len(parts) == 1 && r.Method == http.MethodGet {
				return paHTTPRoute{action: "status"}, nil
			}
			if !paHTTPSafeBotID(bot) {
				return paHTTPRoute{}, paHTTPFailure(pa.Invalid, "bot_id")
			}
			if len(parts) == 1 {
				if r.Method == http.MethodGet {
					return paHTTPRoute{bot: bot, action: "get"}, nil
				}
				if r.Method == http.MethodPatch {
					return paHTTPRoute{bot: bot, action: "edit"}, nil
				}
			}
			if len(parts) == 2 {
				if r.Method == http.MethodGet {
					switch parts[1] {
					case "messages":
						return paHTTPRoute{bot: bot, action: "history"}, nil
					case "events", "export":
						return paHTTPRoute{bot: bot, action: parts[1]}, nil
					}
				}
				if r.Method == http.MethodPost {
					switch parts[1] {
					case "messages":
						return paHTTPRoute{bot: bot, action: "send"}, nil
					case "stop", "read", "observed", "memory":
						return paHTTPRoute{bot: bot, action: parts[1]}, nil
					}
				}
			}
			if r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "memory" && parts[2] == "forget" {
				return paHTTPRoute{bot: bot, action: "forget"}, nil
			}
			if r.Method == http.MethodPost && len(parts) == 4 && parts[1] == "permissions" && parts[2] != "" && parts[3] == "answer" {
				return paHTTPRoute{bot: bot, action: "answer", offer: parts[2]}, nil
			}
		}
	}
	return paHTTPRoute{}, paHTTPFailure(pa.Invalid, "route")
}

func paHTTPQuery(r *http.Request, fields ...string) (url.Values, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query) != len(fields) {
		return nil, paHTTPFailure(pa.Invalid, "query")
	}
	for _, field := range fields {
		if len(query[field]) != 1 {
			return nil, paHTTPFailure(pa.Invalid, field)
		}
	}
	return query, nil
}

func paHTTPNumber(q url.Values, field string) (int64, error) {
	text := q.Get(field)
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			return 0, paHTTPFailure(pa.Invalid, field)
		}
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, paHTTPFailure(pa.Invalid, field)
	}
	return n, nil
}

func (h *projectAssistantHTTP) queryKey(q url.Values, route paHTTPRoute) (pa.BotKey, error) {
	key := pa.BotKey{MachineID: q.Get("machine_id"), BotID: route.bot}
	if key.MachineID == "" || key.MachineID != h.options.MachineID {
		return key, paHTTPFailure(pa.ScopeDenied, "machine_id")
	}
	return key, nil
}

func (h *projectAssistantHTTP) command(r *http.Request, route paHTTPRoute, c contract.ProjectAssistantCommand, operation pa.Operation) (pa.Command, error) {
	command := paDomainCommand(c)
	if h.options.MachineID == "" || command.Key.MachineID != h.options.MachineID || command.Key.BotID == "" || route.bot != "" && route.bot != command.Key.BotID {
		return command, paHTTPFailure(pa.ScopeDenied, "command_key")
	}
	if !paHTTPSafeBotID(command.Key.BotID) {
		return command, paHTTPFailure(pa.Invalid, "bot_id")
	}
	if command.Operation != operation {
		return command, paHTTPFailure(pa.Invalid, "operation")
	}
	values := r.Header.Values("Idempotency-Key")
	if len(values) != 1 || values[0] == "" || values[0] != command.IdempotencyKey {
		return command, paHTTPFailure(pa.Invalid, "idempotency_key")
	}
	return command, nil
}

func paHTTPFence(command pa.Command, value contract.ProjectAssistantFence) (pa.Fence, error) {
	fence := paDomainFence(value)
	if fence.Key != command.Key || fence.Project.MachineID != command.Key.MachineID {
		return fence, paHTTPFailure(pa.ScopeDenied, "fence")
	}
	if err := pa.ValidateScope(fence.Project); err != nil {
		return fence, err
	}
	return fence, nil
}

func paHTTPReceipt(receipt pa.Receipt, err error) (any, error) {
	return contract.ProjectAssistantReceiptResponse{ContractVersion: pa.ContractVersion, Receipt: paWireReceipt(receipt)}, err
}

func paHTTPBot(bot pa.Bot, receipt pa.Receipt, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	wire, err := paWireBot(bot)
	return contract.ProjectAssistantBotResponse{ContractVersion: pa.ContractVersion, Bot: wire, Receipt: paWireReceipt(receipt)}, err
}

func paHTTPMemory(memory pa.MemoryRevision, err error) (any, error) {
	return contract.ProjectAssistantMemoryResponse{ContractVersion: pa.ContractVersion, Memory: paWireMemoryRevision(memory)}, err
}

func (h *projectAssistantHTTP) handle(w http.ResponseWriter, r *http.Request) (any, error) {
	route, err := paRoute(r)
	if err != nil {
		return nil, err
	}
	if route.action == "status" {
		if _, err = paHTTPQuery(r); err != nil {
			return nil, err
		}
		source, err := h.status(r.Context(), false)
		if err != nil {
			return nil, err
		}
		wire, err := paWireSourceStatus(source)
		return contract.ProjectAssistantSourcesResponse{ContractVersion: pa.ContractVersion, Sources: []contract.ProjectAssistantSourceStatus{wire}}, err
	}
	if h.options.Service == nil {
		return nil, paHTTPFailure(pa.Disabled, "feature")
	}
	source, err := h.status(r.Context(), true)
	if err != nil {
		return nil, err
	}
	if r.Method != http.MethodGet {
		if route.action == "read" || route.action == "observed" {
			// These acknowledge this principal's own view, without authorizing
			// content or native effects on a read-only/history-only source.
			if source.State != pa.SourceOnline || !source.Fresh {
				return nil, paHTTPFailure(pa.Offline, "read_observation")
			}
		} else {
			if err = pa.CheckSource(source, true); err != nil {
				return nil, err
			}
		}
	}
	if r.Method == http.MethodGet {
		return h.read(r, route)
	}
	if _, err = paHTTPQuery(r); err != nil {
		return nil, err
	}
	return h.write(w, r, route)
}

func (h *projectAssistantHTTP) read(r *http.Request, route paHTTPRoute) (any, error) {
	fields := []string{"machine_id"}
	switch route.action {
	case "list":
		fields = append(fields, "query", "cursor", "page_size")
	case "history":
		fields = append(fields, "conversation_id", "before_seq", "page_size")
	case "events":
		fields = append(fields, "after_seq", "page_size")
	}
	q, err := paHTTPQuery(r, fields...)
	if err != nil {
		return nil, err
	}
	key, err := h.queryKey(q, route)
	if err != nil {
		return nil, err
	}
	var size int64
	if q.Has("page_size") {
		size, err = paHTTPNumber(q, "page_size")
		if err != nil {
			return nil, err
		}
		bound := int64(pa.MaxPageRows)
		if route.action == "events" {
			bound = pa.MaxEventPageRows
		}
		if size < 1 || size > bound {
			return nil, paHTTPFailure(pa.Invalid, "page_size")
		}
	}
	s := h.options.Service
	ctx := r.Context()
	switch route.action {
	case "list":
		page, err := s.List(ctx, pa.ListBots{MachineID: key.MachineID, Query: q.Get("query"), Cursor: q.Get("cursor"), PageSize: int(size)})
		if err != nil {
			return nil, err
		}
		response := contract.ProjectAssistantListResponse{ContractVersion: pa.ContractVersion, Items: make([]contract.ProjectAssistantBotSummary, len(page.Items)), Sources: make([]contract.ProjectAssistantSourceStatus, len(page.Sources)), NextCursor: page.NextCursor}
		for i, item := range page.Items {
			response.Items[i], err = paWireBotSummary(item)
			if err != nil {
				return nil, err
			}
		}
		for i, source := range page.Sources {
			response.Sources[i], err = paWireSourceStatus(source)
			if err != nil {
				return nil, err
			}
		}
		return response, nil
	case "get":
		bot, conversation, source, err := s.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		wireBot, err := paWireBot(bot)
		if err != nil {
			return nil, err
		}
		wireSource, err := paWireSourceStatus(source)
		return contract.ProjectAssistantGetResponse{ContractVersion: pa.ContractVersion, Bot: wireBot, Conversation: paWireConversation(conversation), Source: wireSource}, err
	case "history":
		before, err := paHTTPNumber(q, "before_seq")
		if err != nil {
			return nil, err
		}
		_, conversation, _, err := s.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		if q.Get("conversation_id") == "" || q.Get("conversation_id") != conversation.ID {
			return nil, paHTTPFailure(pa.ScopeDenied, "conversation_id")
		}
		page, err := s.History(ctx, app.HistoryQuery{Key: key, ConversationID: conversation.ID, BeforeSeq: before, PageSize: int(size)})
		if err != nil {
			return nil, err
		}
		response := contract.ProjectAssistantHistoryResponse{ContractVersion: pa.ContractVersion, Conversation: paWireConversation(conversation), Items: make([]contract.ProjectAssistantMessage, len(page.Items)), NextBeforeSeq: page.NextBeforeSeq, HasMore: page.HasMore}
		for i, item := range page.Items {
			response.Items[i], err = paWireMessage(item)
			if err != nil {
				return nil, err
			}
		}
		return response, nil
	case "events":
		after, err := paHTTPNumber(q, "after_seq")
		if err != nil {
			return nil, err
		}
		page, err := s.Events(ctx, pa.EventQuery{Key: key, AfterSeq: after, PageSize: int(size)})
		if err != nil {
			return nil, err
		}
		response := contract.ProjectAssistantEventsResponse{ContractVersion: pa.ContractVersion, Items: make([]contract.ProjectAssistantEvent, len(page.Items)), NextSeq: page.NextSeq, HasMore: page.HasMore}
		for i, item := range page.Items {
			response.Items[i], err = paWireEvent(item)
			if err != nil {
				return nil, err
			}
		}
		return response, nil
	case "export":
		snapshot, err := s.Export(ctx, key)
		if err != nil {
			return nil, err
		}
		return paWireExport(snapshot)
	}
	return nil, paHTTPFailure(pa.Invalid, "route")
}

func (h *projectAssistantHTTP) write(w http.ResponseWriter, r *http.Request, route paHTTPRoute) (any, error) {
	s, ctx := h.options.Service, r.Context()
	switch route.action {
	case "create":
		request, err := paDecodeHTTP[contract.ProjectAssistantCreateRequest](w, r)
		if err != nil {
			return nil, err
		}
		command, err := h.command(r, route, request.Command, pa.CreateOperation)
		if err != nil {
			return nil, err
		}
		settings := request.Settings
		if settings.Target.MachineID != command.Key.MachineID {
			return nil, paHTTPFailure(pa.ScopeDenied, "target_machine")
		}
		bot, receipt, err := s.Create(ctx, command, pa.CreateBot{Target: paDomainProjectSelection(settings.Target), Name: settings.Name, Appearance: paDomainAppearance(settings.Appearance), Agent: pa.Agent(settings.Agent), PermissionPolicy: pa.PermissionPolicy(settings.PermissionPolicy), Instructions: settings.Instructions, RoleSnapshotRef: settings.RoleSnapshotRef})
		return paHTTPBot(bot, receipt, err)
	case "edit":
		request, err := paDecodeHTTP[contract.ProjectAssistantEditRequest](w, r)
		if err != nil {
			return nil, err
		}
		command, err := h.command(r, route, request.Command, pa.EditOperation)
		if err != nil {
			return nil, err
		}
		settings := request.Settings
		bot, receipt, err := s.Edit(ctx, command, pa.EditBot{ExpectedVersion: settings.ExpectedVersion, Name: settings.Name, Appearance: paDomainAppearance(settings.Appearance), Instructions: settings.Instructions})
		return paHTTPBot(bot, receipt, err)
	case "send":
		request, err := paDecodeHTTP[contract.ProjectAssistantSendRequest](w, r)
		if err != nil {
			return nil, err
		}
		command, err := h.command(r, route, request.Command, pa.SendOperation)
		if err != nil {
			return nil, err
		}
		fence, err := paHTTPFence(command, request.Fence)
		if err != nil {
			return nil, err
		}
		return paHTTPReceipt(s.Send(ctx, command, pa.SendMessage{Fence: fence, Text: request.Text, QuoteMessageID: request.QuoteMessageID}))
	case "stop":
		request, err := paDecodeHTTP[contract.ProjectAssistantStopRequest](w, r)
		if err != nil {
			return nil, err
		}
		command, err := h.command(r, route, request.Command, pa.StopOperation)
		if err != nil {
			return nil, err
		}
		fence, err := paHTTPFence(command, request.Fence)
		if err != nil {
			return nil, err
		}
		return paHTTPReceipt(s.Stop(ctx, app.StopRequest{Command: command, Fence: fence, TurnID: request.TurnID}))
	case "answer":
		request, err := paDecodeHTTP[contract.ProjectAssistantPermissionAnswerRequest](w, r)
		if err != nil {
			return nil, err
		}
		command, err := h.command(r, route, request.Command, pa.AnswerOperation)
		if err != nil {
			return nil, err
		}
		fence, err := paHTTPFence(command, request.Fence)
		if err != nil {
			return nil, err
		}
		if request.OfferID != route.offer {
			return nil, paHTTPFailure(pa.ScopeDenied, "offer_id")
		}
		return paHTTPReceipt(s.Answer(ctx, command, pa.PermissionAnswer{Fence: fence, OfferID: request.OfferID, OptionID: request.OptionID, TurnID: request.TurnID}))
	case "read":
		request, err := paDecodeHTTP[contract.ProjectAssistantReadRequest](w, r)
		if err != nil {
			return nil, err
		}
		command, err := h.command(r, route, request.Command, pa.ReadOperation)
		if err != nil {
			return nil, err
		}
		cursor := paDomainReadCursor(request.Cursor)
		if cursor.Key != command.Key {
			return nil, paHTTPFailure(pa.ScopeDenied, "read_key")
		}
		cursor, err = s.MarkRead(ctx, command, cursor)
		return contract.ProjectAssistantReadResponse{ContractVersion: pa.ContractVersion, Cursor: paWireReadCursor(cursor)}, err
	case "observed":
		request, err := paDecodeHTTP[contract.ProjectAssistantObserveRequest](w, r)
		if err != nil {
			return nil, err
		}
		command, err := h.command(r, route, request.Command, pa.ObserveOperation)
		if err != nil {
			return nil, err
		}
		if request.ReceiptID == "" || request.EventSeq < 0 {
			return nil, paHTTPFailure(pa.Invalid, "observation")
		}
		return paHTTPReceipt(s.Observe(ctx, command, request.ReceiptID, request.EventSeq))
	case "forget":
		request, err := paDecodeHTTP[contract.ProjectAssistantForgetRequest](w, r)
		if err != nil {
			return nil, err
		}
		command, err := h.command(r, route, request.Command, pa.ForgetOperation)
		if err != nil {
			return nil, err
		}
		if request.Project.MachineID != command.Key.MachineID {
			return nil, paHTTPFailure(pa.ScopeDenied, "memory_project")
		}
		return paHTTPMemory(s.Forget(ctx, app.ForgetRequest{Command: command, Project: paDomainProjectScope(request.Project), ExpectedRevision: request.ExpectedRevision, SourceIDs: request.SourceIds}))
	case "memory":
		request, err := paDecodeHTTP[contract.ProjectAssistantMemoryWriteRequest](w, r)
		if err != nil {
			return nil, err
		}
		command, err := h.command(r, route, request.Command, pa.MemorySaveOperation)
		if err != nil {
			return nil, err
		}
		if request.Entry.Key != request.Command.Key || request.Entry.Project.MachineID != command.Key.MachineID {
			return nil, paHTTPFailure(pa.ScopeDenied, "memory_entry")
		}
		return paHTTPMemory(s.SaveMemory(ctx, app.MemoryWrite{Command: command, ExpectedRevision: request.ExpectedRevision, Entry: paDomainMemoryEntry(request.Entry)}))
	}
	return nil, paHTTPFailure(pa.Invalid, "route")
}

func paDecodeHTTP[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var value T
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, commandBodyLimit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return value, paHTTPFailure(pa.CapacityFull, "body")
		}
		return value, paHTTPFailure(pa.Invalid, "body")
	}
	if !utf8.Valid(raw) {
		return value, paHTTPFailure(pa.Invalid, "body")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&value); err != nil {
		return value, paHTTPFailure(pa.Invalid, "body")
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return value, paHTTPFailure(pa.Invalid, "body")
	}
	// All request fields in this contract are required and non-null. Compare
	// object keys with a generated value's JSON shape to also reject Go's
	// case-insensitive field matching and omitted zero-valued fields.
	normalized, err := json.Marshal(value)
	if err != nil {
		return value, err
	}
	input, err := paJSONObject(json.NewDecoder(bytes.NewReader(raw)))
	if err != nil {
		return value, err
	}
	var expected any
	if err = json.Unmarshal(normalized, &expected); err != nil {
		return value, err
	}
	if _, object := input.(map[string]any); !object || !paJSONShape(input, expected) {
		return value, paHTTPFailure(pa.Invalid, "body")
	}
	return value, nil
}

// paJSONObject retains duplicate-key evidence before maps could discard it.
// Typed decoding above has already rejected malformed/deep or mistyped JSON.
func paJSONObject(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, paHTTPFailure(pa.Invalid, "body")
	}
	if token == nil {
		return nil, paHTTPFailure(pa.Invalid, "body")
	}
	if delimiter, ok := token.(json.Delim); ok {
		switch delimiter {
		case '{':
			object := map[string]any{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, paHTTPFailure(pa.Invalid, "body")
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, paHTTPFailure(pa.Invalid, "body")
				}
				if _, exists := object[key]; exists {
					return nil, paHTTPFailure(pa.Invalid, "body")
				}
				object[key], err = paJSONObject(decoder)
				if err != nil {
					return nil, err
				}
			}
			if _, err = decoder.Token(); err != nil {
				return nil, paHTTPFailure(pa.Invalid, "body")
			}
			return object, nil
		case '[':
			array := []any{}
			for decoder.More() {
				item, err := paJSONObject(decoder)
				if err != nil {
					return nil, err
				}
				array = append(array, item)
			}
			if _, err = decoder.Token(); err != nil {
				return nil, paHTTPFailure(pa.Invalid, "body")
			}
			return array, nil
		}
		return nil, paHTTPFailure(pa.Invalid, "body")
	}
	return token, nil
}

func paJSONShape(input, expected any) bool {
	switch value := input.(type) {
	case map[string]any:
		other, ok := expected.(map[string]any)
		if !ok || len(value) != len(other) {
			return false
		}
		for key, item := range value {
			target, exists := other[key]
			if !exists || !paJSONShape(item, target) {
				return false
			}
		}
	case []any:
		other, ok := expected.([]any)
		if !ok || len(value) != len(other) {
			return false
		}
		for i := range value {
			if !paJSONShape(value[i], other[i]) {
				return false
			}
		}
	}
	return input != nil && expected != nil
}

func paWriteHTTPFailure(w http.ResponseWriter, err error) {
	failure := pa.Failure{Code: pa.Code(err)}
	var typed *pa.Failure
	if errors.As(err, &typed) {
		failure = *typed
	}
	status := http.StatusServiceUnavailable
	switch failure.Code {
	case pa.Invalid:
		status = http.StatusBadRequest
	case pa.Disabled, pa.ReadOnly, pa.Unpaired, pa.Revoked, pa.ScopeDenied, pa.ScopeExpansionDenied:
		status = http.StatusForbidden
	case pa.NotFound:
		status = http.StatusNotFound
	case pa.StaleGeneration, pa.StaleVersion, pa.IdempotencyConflict, pa.ContextStale:
		status = http.StatusConflict
	case pa.Expired, pa.CursorExpired:
		status = http.StatusGone
	case pa.CapacityFull:
		status = http.StatusTooManyRequests
	case pa.Unsupported, pa.ProviderVersionUnsupported, pa.ResumeUnsupported:
		status = http.StatusNotImplemented
	case pa.OutcomeUnknown, pa.ExecutionEffectUnknown:
		status = http.StatusInternalServerError
	}
	if failure.Code == pa.CapacityFull && failure.RetryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(failure.RetryAfterSeconds, 10))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSON(w, contract.ProjectAssistantErrorResponse{ContractVersion: pa.ContractVersion, Error: paWireFailure(failure)})
}
