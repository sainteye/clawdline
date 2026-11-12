package projectassistant

import (
	"encoding/hex"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

type FeatureState struct{ Enabled, HasData bool }
type Access string

const (
	Hidden      Access = "hidden"
	HistoryOnly Access = "history_only"
	Interactive Access = "interactive"
)

func (s FeatureState) Access() Access {
	if s.Enabled {
		return Interactive
	}
	if s.HasData {
		return HistoryOnly
	}
	return Hidden
}
func (s FeatureState) CheckWrite() error {
	if !s.Enabled {
		return fail(Disabled, "feature")
	}
	return nil
}

func CheckSource(s SourceStatus, write bool) error {
	switch s.State {
	case SourceRevoked:
		return fail(Revoked, "source")
	case SourceUnpaired:
		return fail(Unpaired, "source")
	case SourceUnsupported:
		return fail(Unsupported, "source")
	case SourceOffline:
		if write {
			return fail(Offline, "source")
		}
	case SourceOnline:
	default:
		return fail(Invalid, "source_state")
	}
	if !s.Readable {
		return fail(ScopeDenied, "source_read")
	}
	if write && (!s.Writable || !s.Fresh) {
		return fail(ReadOnly, "source_write")
	}
	if write && s.Access != Interactive {
		return fail(Disabled, "source_feature")
	}
	return nil
}

func ValidateTarget(target ProjectSelection, resolved ProjectResolution) error {
	if target.MachineID == "" || target.ProjectID == "" {
		return fail(Invalid, "target")
	}
	if target.MachineID != resolved.Scope.MachineID || target.ProjectID != resolved.Scope.ProjectID {
		return fail(ScopeDenied, "target")
	}
	if !resolved.Current {
		return fail(StaleVersion, "project_resolution")
	}
	if !resolved.Available || resolved.Directory == "" {
		return fail(NotFound, "project_directory")
	}
	return ValidateScope(resolved.Scope)
}

func ValidateScope(s ProjectScope) error {
	if s.MachineID == "" || s.ProjectID == "" || s.CanonicalID == "" {
		return fail(Invalid, "project")
	}
	if s.Kind != RepositoryProject && s.Kind != PlaceProject {
		return fail(Invalid, "project_kind")
	}
	return nil
}

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func ValidateAppearance(a Appearance) error {
	switch a.Shape {
	case Blob, Squircle, Wedge, Pebble, Tablet, Hex, Cloud, Teardrop:
	default:
		return fail(Invalid, "shape")
	}
	if !colorPattern.MatchString(a.Color) {
		return fail(Invalid, "color")
	}
	return nil
}
func validateSettings(name string, appearance Appearance, instructions string) error {
	if !utf8.ValidString(name) || strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > MaxNameRunes {
		return fail(Invalid, "name")
	}
	if !utf8.ValidString(instructions) || len(instructions) > MaxInstructionBytes {
		return fail(CapacityFull, "instructions")
	}
	return ValidateAppearance(appearance)
}

// ValidateCreate has no Runtime dependency: creation records configuration only.
func ValidateCreate(in CreateBot, resolved ProjectResolution) error {
	if err := ValidateTarget(in.Target, resolved); err != nil {
		return err
	}
	if in.Agent != ClaudeCode && in.Agent != Codex {
		return fail(Unsupported, "agent")
	}
	if in.PermissionPolicy != Ask && in.PermissionPolicy != AutoRead {
		return fail(Invalid, "permission_policy")
	}
	return validateSettings(in.Name, in.Appearance, in.Instructions)
}
func ApplyEdit(bot Bot, in EditBot) (Bot, error) {
	if in.ExpectedVersion != bot.Version {
		return bot, fail(StaleVersion, "bot")
	}
	if err := validateSettings(in.Name, in.Appearance, in.Instructions); err != nil {
		return bot, err
	}
	bot.Name = in.Name
	bot.Appearance = in.Appearance
	if bot.Instructions.Text != in.Instructions {
		bot.Instructions.Text = in.Instructions
		bot.Instructions.Revision++
	}
	bot.Version++
	return bot, nil
}

// CaptureInstructions is called only while claiming the next queued turn.
// Edits never mutate an already captured Turn.Instructions.
func CaptureInstructions(bot Bot, activeTurnID string) (InstructionSnapshot, error) {
	if activeTurnID != "" {
		return InstructionSnapshot{}, fail(StaleVersion, "active_turn")
	}
	return bot.Instructions, nil
}
func ValidateMessage(text string) error {
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" {
		return fail(Invalid, "text")
	}
	if len(text) > MaxMessageBytes {
		return fail(CapacityFull, "text")
	}
	return nil
}
func CheckAdmission(pending int) error {
	if pending < 0 {
		return fail(Invalid, "pending")
	}
	if pending >= MaxPendingTurns {
		return fail(CapacityFull, "pending_turns")
	}
	return nil
}
func CheckPage(size int, events bool) error {
	bound := MaxPageRows
	if events {
		bound = MaxEventPageRows
	}
	if size < 1 || size > bound {
		return fail(Invalid, "page_size")
	}
	return nil
}
func CheckFence(got, current Fence) error {
	if current.Key.MachineID == "" || current.Key.BotID == "" || current.ConversationID == "" || current.BindingID == "" || current.Generation < 1 {
		return fail(Invalid, "current_fence")
	}
	if err := ValidateScope(current.Project); err != nil {
		return err
	}
	if current.Key.MachineID != current.Project.MachineID {
		return fail(ScopeDenied, "current_source")
	}
	if got.Key != current.Key || got.Project != current.Project || got.ConversationID != current.ConversationID {
		return fail(ScopeDenied, "fence")
	}
	if got.Generation != current.Generation || got.BindingID != current.BindingID {
		return fail(StaleGeneration, "fence")
	}
	return nil
}

// The only unbound request is first-send admission. PA-02 atomically mints
// generation 1 and a binding before execution; all later requests use CheckFence.
func CheckSendFence(got, current Fence) error {
	if current.BindingID != "" || current.Generation != 0 {
		return CheckFence(got, current)
	}
	if current.Key.MachineID == "" || current.Key.BotID == "" || current.ConversationID == "" {
		return fail(Invalid, "initial_fence")
	}
	if err := ValidateScope(current.Project); err != nil {
		return err
	}
	if current.Key.MachineID != current.Project.MachineID || got != current {
		return fail(ScopeDenied, "initial_fence")
	}
	return nil
}

func CheckPermission(offer PermissionOffer, answer PermissionAnswer, context PermissionContext) error {
	current := context.Fence
	if err := context.Feature.CheckWrite(); err != nil {
		return err
	}
	if context.BindingRevoked {
		return fail(Revoked, "binding")
	}
	if context.Source.MachineID != current.Key.MachineID {
		return fail(ScopeDenied, "source")
	}
	if err := CheckSource(context.Source, true); err != nil {
		return err
	}
	if err := CheckFence(offer.Fence, current); err != nil {
		return err
	}
	if err := CheckFence(answer.Fence, current); err != nil {
		return err
	}
	if context.ActiveTurnID == "" || offer.TurnID != context.ActiveTurnID || answer.TurnID != offer.TurnID {
		return fail(StaleVersion, "permission_turn")
	}
	if answer.OfferID != offer.ID || offer.ID == "" {
		return fail(NotFound, "permission")
	}
	if offer.NativeRequestID == "" {
		return fail(Invalid, "native_request")
	}
	if offer.State == PermissionExpired {
		return fail(Expired, "permission")
	}
	if offer.State != PermissionPending {
		return fail(StaleVersion, "permission_answered")
	}
	if !context.Now.Before(offer.ExpiresAt) {
		return fail(Expired, "permission")
	}
	for _, option := range offer.Options {
		if option.ID == answer.OptionID {
			// Native approval cannot widen the enforced read-only chat sandbox.
			if option.Kind == AllowAlways || option.Kind == RejectAlways {
				return fail(ScopeExpansionDenied, "persistent_permission")
			}
			if (option.Kind != AllowOnce && option.Kind != RejectOnce) || option.Allow != (option.Kind == AllowOnce) {
				return fail(Invalid, "permission_kind")
			}
			if option.Allow && !option.ReadOnly {
				return fail(ScopeExpansionDenied, "native_write")
			}
			return nil
		}
	}
	return fail(Invalid, "permission_option")
}

func AdvanceRead(current ReadCursor, incoming ReadCursor, lastSeq int64) (ReadCursor, error) {
	if current.Key != incoming.Key || current.ConversationID != incoming.ConversationID || current.ReaderID != incoming.ReaderID || current.ReaderID == "" {
		return current, fail(ScopeDenied, "reader")
	}
	if incoming.MessageSeq < 0 || incoming.MessageSeq > lastSeq || lastSeq < current.MessageSeq {
		return current, fail(Invalid, "read_seq")
	}
	if incoming.MessageSeq > current.MessageSeq {
		current.MessageSeq = incoming.MessageSeq
	}
	return current, nil
}

type ReplayDecision string

const (
	Execute        ReplayDecision = "execute"
	ReturnReceipt  ReplayDecision = "return_receipt"
	InspectUnknown ReplayDecision = "inspect_unknown"
)

func DecideReplay(command Command, receipt *Receipt) (ReplayDecision, error) {
	if command.Key.MachineID == "" || command.Key.BotID == "" || command.Operation == "" || command.IdempotencyKey == "" || len(command.ContentHash) != 64 {
		return "", fail(Invalid, "command")
	}
	if _, err := hex.DecodeString(command.ContentHash); err != nil {
		return "", fail(Invalid, "content_hash")
	}
	switch command.Operation {
	case CreateOperation, EditOperation, SendOperation, StopOperation, AnswerOperation, ReadOperation, ObserveOperation, ForgetOperation, MemorySaveOperation, DispatchOperation:
	default:
		return "", fail(Invalid, "operation")
	}
	if receipt == nil {
		return Execute, nil
	}
	if receipt.Command != command {
		return "", fail(IdempotencyConflict, "command")
	}
	if receipt.State == EffectUnknown || receipt.State == EffectExecuting {
		return InspectUnknown, nil
	}
	return ReturnReceipt, nil
}
func CanTransition(from, to TurnState) bool {
	switch from {
	case TurnAccepted:
		return to == TurnQueued || to == TurnCancelled
	case TurnQueued:
		return to == TurnRunning || to == TurnCancelled || to == TurnFailed
	case TurnRunning:
		return to == TurnWaitingPermission || to == TurnSucceeded || to == TurnFailed || to == TurnCancelled || to == TurnUnknown
	case TurnWaitingPermission:
		return to == TurnRunning || to == TurnCancelled || to == TurnFailed || to == TurnUnknown
	}
	return false
}

// VisibleReply is applied transactionally once per turn completion. Explicit
// messages are already durable; final text is promoted only when none exist.
func VisibleReply(explicitMessageIDs []string, finalText string, stopped bool) (string, bool) {
	if len(explicitMessageIDs) > 0 || stopped || strings.TrimSpace(finalText) == "" {
		return "", false
	}
	return finalText, true
}

func CheckCapabilities(c Capabilities) error {
	if c.Agent != ClaudeCode && c.Agent != Codex {
		return fail(Unsupported, "agent")
	}
	if c.Unavailable != nil {
		return c.Unavailable
	}
	if c.CLIVersion == "" || c.AdapterVersion == "" || c.Evidence == "" || !c.ReadOnlyEnforced {
		return fail(ReadOnlyUnverified, "native_read_only")
	}
	if !c.NativeMemoryVerified || (c.NativeMemory != NativeMemoryDisabled && c.NativeMemory != NativeMemoryIsolated) {
		return fail(MemoryIsolationUnverified, "native_memory")
	}
	return nil
}

func CheckExecutionCapabilities(c Capabilities, fence Fence) error {
	if err := CheckCapabilities(c); err != nil {
		return err
	}
	if c.Key != fence.Key || c.Project != fence.Project {
		return fail(ScopeDenied, "capability_scope")
	}
	return nil
}
func CheckContext(snapshot ContextSnapshot, current MemoryRevision) error {
	if err := ValidateScope(current.Project); err != nil {
		return err
	}
	if current.Key.MachineID != current.Project.MachineID || current.Key.BotID == "" || current.Revision < 1 || current.ForgottenThrough > current.Revision || current.ForgottenThrough < 0 {
		return fail(Invalid, "memory")
	}
	if snapshot.Memory.Key != current.Key || snapshot.Memory.Project != current.Project {
		return fail(ScopeDenied, "memory")
	}
	if !snapshot.ProvenanceComplete || snapshot.Memory.Revision != current.Revision || snapshot.Memory.ForgottenThrough != current.ForgottenThrough {
		return fail(ContextStale, "memory_revision")
	}
	if strings.TrimSpace(snapshot.Text) != "" && len(snapshot.Sources) == 0 {
		return fail(ContextStale, "memory_provenance")
	}
	for _, source := range snapshot.Sources {
		if source.ID == "" || source.Key != current.Key || source.Project != current.Project || source.Revision < 1 || source.Revision > current.Revision || slices.Contains(current.ForgottenSourceIDs, source.ID) {
			return fail(ContextStale, "memory_source")
		}
	}
	return nil
}

func CheckOpaqueResume(binding ExecutionBinding, current MemoryRevision) error {
	if binding.Fence.Key != current.Key || binding.Fence.Project != current.Project {
		return fail(ScopeDenied, "resume")
	}
	if binding.ForgottenThrough != current.ForgottenThrough {
		return fail(ResumeForbiddenAfterForget, "native_history")
	}
	if binding.MemoryRevision != current.Revision {
		return fail(ContextStale, "native_history")
	}
	return nil
}

type ResumeDecision string

const (
	ResumeExisting ResumeDecision = "resume"
	StartFiltered  ResumeDecision = "start_filtered"
)

// Forgetting invalidates opaque native history even when an adapter can resume.
// PA-03 supplies a freshly filtered, provenance-complete snapshot; PA-02 mints
// a new binding and generation before sending it. No full-history fallback.
func DecideResume(binding ExecutionBinding, current MemoryRevision, snapshot ContextSnapshot, caps Capabilities) (ResumeDecision, error) {
	if err := CheckExecutionCapabilities(caps, binding.Fence); err != nil {
		return "", err
	}
	if binding.Fence.Key != current.Key || binding.Fence.Project != current.Project || binding.Agent != caps.Agent {
		return "", fail(ScopeDenied, "binding")
	}
	if binding.Revoked {
		return "", fail(Revoked, "binding")
	}
	if err := CheckContext(snapshot, current); err != nil {
		return "", err
	}
	if caps.Resume && binding.NativeSessionID != "" && CheckOpaqueResume(binding, current) == nil {
		return ResumeExisting, nil
	}
	return StartFiltered, nil
}

// ValidateEvent prevents payload ambiguity during durable replay. Payload data
// itself is validated against the current aggregate before Repository.Commit.
func ValidateEvent(e Event) error {
	if e.Version != ContractVersion || e.ID == "" || e.Seq < 1 || e.Fence.Key.MachineID == "" || e.Fence.Key.BotID == "" {
		return fail(Invalid, "event")
	}
	count := 0
	for _, present := range []bool{e.Message != nil, e.Turn != nil, e.Permission != nil, e.Work != nil, e.Memory != nil, e.Bot != nil, e.Read != nil, e.Trace != nil, e.Source != nil} {
		if present {
			count++
		}
	}
	valid := false
	switch e.Kind {
	case BotCreated, BotUpdated:
		valid = e.Bot != nil
	case MessageCreated:
		valid = e.Message != nil
	case TurnUpdated:
		valid = e.Turn != nil
	case PermissionOffered, PermissionAnswered:
		valid = e.Permission != nil
	case WorkUpdated:
		valid = e.Work != nil
	case MemoryChanged:
		valid = e.Memory != nil
	case ReadAdvanced:
		valid = e.Read != nil
	case TraceUpdated:
		valid = e.Trace != nil
	case SourceUpdated:
		valid = e.Source != nil
	case FeatureDisabled:
		return checkNoPayload(count)
	}
	if !valid || count != 1 {
		return fail(Invalid, "event_payload")
	}
	return nil
}
func checkNoPayload(count int) error {
	if count != 0 {
		return fail(Invalid, "event_payload")
	}
	return nil
}

// Forget is monotonic, optimistic and source-specific; tombstones are durable.
func Forget(current MemoryRevision, expected int64, sourceIDs []string) (MemoryRevision, error) {
	if err := ValidateScope(current.Project); err != nil {
		return current, err
	}
	if current.Key.MachineID != current.Project.MachineID || current.Key.BotID == "" || current.Revision < 1 {
		return current, fail(Invalid, "memory")
	}
	if expected != current.Revision {
		return current, fail(StaleVersion, "memory")
	}
	if len(sourceIDs) == 0 {
		return current, fail(Invalid, "sources")
	}
	for _, id := range sourceIDs {
		if strings.TrimSpace(id) == "" {
			return current, fail(Invalid, "source_id")
		}
	}
	current.ForgottenSourceIDs = slices.Clone(current.ForgottenSourceIDs)
	for _, id := range sourceIDs {
		if !slices.Contains(current.ForgottenSourceIDs, id) {
			current.ForgottenSourceIDs = append(current.ForgottenSourceIDs, id)
		}
	}
	current.Revision++
	current.ForgottenThrough = current.Revision
	return current, nil
}

func ValidateMemoryWrite(entry MemoryEntry, current MemoryRevision, expected int64) error {
	if expected != current.Revision {
		return fail(StaleVersion, "memory")
	}
	if entry.Revision != expected {
		return fail(StaleVersion, "memory_entry_revision")
	}
	if entry.Key != current.Key || entry.Project != current.Project {
		return fail(ScopeDenied, "memory_entry")
	}
	if entry.ID == "" || strings.TrimSpace(entry.Text) == "" || entry.Forgotten || len(entry.Sources) == 0 {
		return fail(Invalid, "memory_entry")
	}
	if slices.Contains(current.ForgottenSourceIDs, entry.ID) {
		return fail(ContextStale, "forgotten_entry")
	}
	return CheckContext(ContextSnapshot{Memory: current, Text: entry.Text, Sources: entry.Sources, ProvenanceComplete: true}, current)
}

// EntryBadge excludes revoked/unpaired sources. Offline cached data retains its
// separate freshness marker rather than becoming an authoritative empty list.
func EntryBadge(rows []BotSummary) (unread, pending int64) {
	for _, r := range rows {
		if CheckSource(r.Source, false) != nil {
			continue
		}
		unread += r.UnreadCount
		pending += r.PendingPermissions
	}
	return
}
