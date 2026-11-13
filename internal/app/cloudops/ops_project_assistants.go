package cloudops

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/contract"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func init() {
	for _, word := range []string{"status", "projects", "list", "get", "history", "events", "export", "view"} {
		register(op{name: "project-assistants." + word, read: true,
			shape: projectAssistantReply, partial: projectAssistantFailure})
	}
	for _, word := range []string{"create", "edit", "send", "stop", "answer", "read", "observed", "forget", "memory"} {
		register(op{name: "project-assistants." + word,
			readLevel: word == "read" || word == "observed",
			guard:     projectAssistantGuard, shape: projectAssistantReply, partial: projectAssistantFailure})
	}
}

func projectAssistantBotID(id string) bool {
	return id != "" && id != "status" && id != "." && id != ".." && utf8.ValidString(id) &&
		!strings.ContainsAny(id, `/\`) && !strings.ContainsFunc(id, unicode.IsControl)
}

// The generated decoder owns shape, operation and ref/key equality. This
// additional guard checks the relationships within that one complete request.
// Current source machine, principal and live fence remain the HTTP owner's job.
func projectAssistantGuard(p plan) *Refusal {
	b, err := decodeBody(p.local.Body)
	if err != nil {
		return projectAssistantMalformed()
	}
	key, _ := b.genAt("command.key").(map[string]any)
	for _, path := range []string{"fence.key", "cursor.key", "entry.key"} {
		if other, present := b.genLookup(path); present && !projectAssistantSame(key, other) {
			return projectAssistantMalformed()
		}
	}
	for _, path := range []string{"settings.target", "fence.project", "project", "entry.project"} {
		if project, present := b.genLookup(path); present {
			object, ok := project.(map[string]any)
			if !ok || object["machine_id"] != key["machine_id"] {
				return projectAssistantMalformed()
			}
		}
	}
	if sources, ok := b.genAt("entry.sources").([]any); ok {
		for _, source := range sources {
			object := source.(map[string]any) // exact object shape was generated
			if !projectAssistantSame(key, object["key"]) || !projectAssistantSame(b.genAt("entry.project"), object["project"]) {
				return projectAssistantMalformed()
			}
		}
	}
	return nil
}

func projectAssistantSame(left, right any) bool {
	a, err := json.Marshal(left)
	b, other := json.Marshal(right)
	return err == nil && other == nil && string(a) == string(b)
}

func projectAssistantMalformed() *Refusal {
	return &Refusal{Status: 400, Code: "malformed_command", Message: "This Cloud command is malformed.", fixedCopy: true}
}

// This hook is limited to the generated PA error envelope; no generic refusal
// fields or other word's error policy are widened. Source Retry-After is carried
// by its generated retry_after_seconds field, since LocalResponse has no headers.
func projectAssistantFailure(res LocalResponse) (json.RawMessage, bool) {
	b, err := decodeBody(res.Body)
	if err != nil || res.Status < 400 || !b.has("contract_version", "error") || b["contract_version"] != pa.ContractVersion {
		return nil, false
	}
	rule := genRule{kind: "object", closed: true, required: []string{"code", "field", "retry_after_seconds"}, properties: map[string]genRule{
		"code": {kind: "string", nonEmpty: true}, "field": {kind: "string"},
		"retry_after_seconds": {kind: "integer", min: genInt(0), max: genInt(domaincloud.MaxSafeInteger)},
	}}
	if !b.genValid("error", rule) {
		return nil, false
	}
	code := pa.ErrorCode(genText(b.genAt("error.code")))
	switch code {
	case pa.Invalid, pa.Disabled, pa.ReadOnly, pa.NotFound, pa.Offline, pa.Unsupported, pa.Unpaired,
		pa.Revoked, pa.StaleGeneration, pa.StaleVersion, pa.IdempotencyConflict, pa.CapacityFull,
		pa.OutcomeUnknown, pa.Expired, pa.ScopeDenied, pa.ContextStale, pa.CursorExpired,
		pa.CLINotInstalled, pa.AdapterNotFound, pa.AdapterInventoryUnknown, pa.ProviderVersionUnsupported,
		pa.AuthRequired, pa.AuthProbeUnknown, pa.AuthRejected, pa.AuthUnavailableInIsolatedRuntime,
		pa.SandboxUnavailable, pa.ReadOnlyUnverified, pa.ScopeExpansionDenied, pa.MemoryIsolationUnverified,
		pa.ResumeForbiddenAfterForget, pa.ResumeUnsupported, pa.ExecutionEffectUnknown:
		return res.Body, true
	}
	return nil, false
}

// Reuse the registered plaintext ceiling without truncation, invented cursors,
// compression negotiation or a second buffer protocol. The answer hook retains
// the full failure body and gives it a matching non-success outer status.
func projectAssistantReply(p plan, res LocalResponse) (json.RawMessage, Refusal) {
	if len(res.Body) > ReadPlaintextBytesLimit {
		return projectAssistantReplyFailure(pa.CapacityFull, "cloud_reply_bytes"), Refusal{}
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(res.Body, &object) != nil || object == nil {
		return projectAssistantReplyFailure(pa.OutcomeUnknown, "cloud_reply"), Refusal{}
	}
	var version string
	if json.Unmarshal(object["contract_version"], &version) != nil || version != pa.ContractVersion {
		return projectAssistantReplyFailure(pa.OutcomeUnknown, "cloud_reply"), Refusal{}
	}
	if len(payload(p.name, res.Status, res.Body, nil)) > ReadPlaintextBytesLimit {
		return projectAssistantReplyFailure(pa.CapacityFull, "cloud_reply_bytes"), Refusal{}
	}
	return res.Body, Refusal{}
}

func projectAssistantReplyFailure(code pa.ErrorCode, field string) json.RawMessage {
	out, _ := json.Marshal(contract.ProjectAssistantErrorResponse{ContractVersion: pa.ContractVersion,
		Error: contract.ProjectAssistantFailure{Code: contract.ProjectAssistantErrorCode(code), Field: field}})
	return out
}

func projectAssistantShapedFailure(word string, body json.RawMessage) (int, string, bool) {
	if !strings.HasPrefix(word, "project-assistants.") {
		return 0, "", false
	}
	if _, ok := projectAssistantFailure(LocalResponse{Status: 500, Body: body}); !ok {
		return 0, "", false
	}
	var failure contract.ProjectAssistantErrorResponse
	if json.Unmarshal(body, &failure) != nil {
		return 0, "", false
	}
	status := 502
	if failure.Error.Code == contract.ProjectAssistantErrorCode(pa.CapacityFull) {
		status = 429
	}
	return status, string(failure.Error.Code), true
}
