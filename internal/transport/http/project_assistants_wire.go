package http

import (
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Wire projections are explicit generated-contract values. Port DTOs have no JSON tags.
// Native binding identities appear only in the authenticated export.

func paWireTime(value time.Time) (string, error) {
	b, err := value.UTC().MarshalText()
	return string(b), err
}

func paWireBotKey(value pa.BotKey) contract.ProjectAssistantBotKey {
	var out contract.ProjectAssistantBotKey
	out.MachineID = value.MachineID
	out.BotID = value.BotID
	return out
}

func paWireProjectScope(value pa.ProjectScope) contract.ProjectAssistantProjectScope {
	var out contract.ProjectAssistantProjectScope
	out.MachineID = value.MachineID
	out.ProjectID = value.ProjectID
	out.CanonicalID = value.CanonicalID
	out.Kind = contract.ProjectAssistantProjectKind(value.Kind)
	return out
}

func paWireProjectSelection(value pa.ProjectSelection) contract.ProjectAssistantProjectSelection {
	var out contract.ProjectAssistantProjectSelection
	out.MachineID = value.MachineID
	out.ProjectID = value.ProjectID
	return out
}

func paWireAppearance(value pa.Appearance) contract.ProjectAssistantAppearance {
	var out contract.ProjectAssistantAppearance
	out.Shape = contract.ProjectAssistantShape(value.Shape)
	out.Color = value.Color
	return out
}

func paWireInstructionSnapshot(value pa.InstructionSnapshot) contract.ProjectAssistantInstructions {
	var out contract.ProjectAssistantInstructions
	out.Revision = value.Revision
	out.Text = value.Text
	out.RoleSnapshotRef = value.RoleSnapshotRef
	return out
}

func paWireBot(value pa.Bot) (contract.ProjectAssistantBot, error) {
	var out contract.ProjectAssistantBot
	out.Key = paWireBotKey(value.Key)
	out.Project = paWireProjectScope(value.Project)
	out.ConversationID = value.ConversationID
	out.Name = value.Name
	out.Appearance = paWireAppearance(value.Appearance)
	out.Agent = contract.ProjectAssistantAgent(value.Agent)
	out.PermissionPolicy = contract.ProjectAssistantPermissionPolicy(value.PermissionPolicy)
	out.Instructions = paWireInstructionSnapshot(value.Instructions)
	out.Version = value.Version
	convertedCreatedAt, err := paWireTime(value.CreatedAt)
	if err != nil {
		return out, err
	}
	out.CreatedAt = convertedCreatedAt
	convertedUpdatedAt, err := paWireTime(value.UpdatedAt)
	if err != nil {
		return out, err
	}
	out.UpdatedAt = convertedUpdatedAt
	return out, nil
}

func paWireConversation(value pa.Conversation) contract.ProjectAssistantConversation {
	var out contract.ProjectAssistantConversation
	out.Key = paWireBotKey(value.Key)
	out.ID = value.ID
	out.Generation = value.Generation
	out.BindingID = value.BindingID
	out.LastMessageSeq = value.LastMessageSeq
	out.MemoryRevision = value.MemoryRevision
	out.ActiveTurnID = value.ActiveTurnID
	out.Version = value.Version
	return out
}

func paWireFence(value pa.Fence) contract.ProjectAssistantFence {
	var out contract.ProjectAssistantFence
	out.Key = paWireBotKey(value.Key)
	out.Project = paWireProjectScope(value.Project)
	out.ConversationID = value.ConversationID
	out.BindingID = value.BindingID
	out.Generation = value.Generation
	return out
}

func paWireTurn(value pa.Turn) contract.ProjectAssistantTurn {
	var out contract.ProjectAssistantTurn
	out.ID = value.ID
	out.Fence = paWireFence(value.Fence)
	out.MessageID = value.MessageID
	out.State = contract.ProjectAssistantTurnState(value.State)
	out.Instructions = paWireInstructionSnapshot(value.Instructions)
	out.MemoryRevision = value.MemoryRevision
	out.IdempotencyKey = value.IdempotencyKey
	if value.StartedAt != nil {
		out.StartedAt = value.StartedAt.UTC().Format(time.RFC3339)
	}
	if value.Failure != nil {
		converted := paWireFailure(*value.Failure)
		out.Failure = &converted
	}
	return out
}

func paWireExecutionBinding(value pa.ExecutionBinding) contract.ProjectAssistantExecutionBinding {
	var out contract.ProjectAssistantExecutionBinding
	out.Fence = paWireFence(value.Fence)
	out.Agent = contract.ProjectAssistantAgent(value.Agent)
	out.NativeSessionID = value.NativeSessionID
	out.MemoryRevision = value.MemoryRevision
	out.ForgottenThrough = value.ForgottenThrough
	out.InstructionRevision = value.InstructionRevision
	out.Revoked = value.Revoked
	return out
}

func paWireMessage(value pa.Message) (contract.ProjectAssistantMessage, error) {
	var out contract.ProjectAssistantMessage
	out.ID = value.ID
	out.Fence = paWireFence(value.Fence)
	out.Seq = value.Seq
	out.TurnID = value.TurnID
	out.Kind = contract.ProjectAssistantMessageKind(value.Kind)
	out.Text = value.Text
	out.QuoteMessageID = value.QuoteMessageID
	for _, a := range value.Attachments {
		out.Attachments = append(out.Attachments, contract.ProjectAssistantAttachment{ID: a.ID, MediaType: a.MediaType, Bytes: a.Bytes})
	}
	convertedAt, err := paWireTime(value.At)
	if err != nil {
		return out, err
	}
	out.At = convertedAt
	return out, nil
}

func paWirePermissionOption(value pa.PermissionOption) contract.ProjectAssistantPermissionOption {
	var out contract.ProjectAssistantPermissionOption
	out.ID = value.ID
	out.Label = value.Label
	out.ReadOnly = value.ReadOnly
	out.Allow = value.Allow
	out.Kind = contract.ProjectAssistantPermissionOptionKind(value.Kind)
	return out
}

func paWirePermissionOffer(value pa.PermissionOffer) (contract.ProjectAssistantPermissionOffer, error) {
	var out contract.ProjectAssistantPermissionOffer
	out.ID = value.ID
	out.Fence = paWireFence(value.Fence)
	out.TurnID = value.TurnID
	out.NativeRequestID = value.NativeRequestID
	out.Options = make([]contract.ProjectAssistantPermissionOption, len(value.Options))
	for i := range value.Options {
		out.Options[i] = paWirePermissionOption(value.Options[i])
	}
	convertedExpiresAt, err := paWireTime(value.ExpiresAt)
	if err != nil {
		return out, err
	}
	out.ExpiresAt = convertedExpiresAt
	out.State = contract.ProjectAssistantPermissionState(value.State)
	out.SelectedOptionID = value.SelectedOptionID
	if a := value.Action; a != nil {
		action := contract.ProjectAssistantPermissionAction{Title: a.Title, ToolKind: a.ToolKind, Detail: a.Detail, Shortened: a.Shortened, Diffs: make([]contract.ProjectAssistantPermissionDiff, len(a.Diffs))}
		for i, d := range a.Diffs {
			action.Diffs[i] = contract.ProjectAssistantPermissionDiff{Path: d.Path, OldText: d.OldText, NewText: d.NewText}
		}
		out.Action = &action
	}
	return out, nil
}

func paWireWorkLink(value pa.WorkLink) contract.ProjectAssistantWorkLink {
	var out contract.ProjectAssistantWorkLink
	out.ID = value.ID
	out.Fence = paWireFence(value.Fence)
	out.TurnID = value.TurnID
	out.TaskID = value.TaskID
	out.BoardItemID = value.BoardItemID
	out.State = contract.ProjectAssistantWorkState(value.State)
	out.ResultMessageID = value.ResultMessageID
	out.LandingCommit = value.LandingCommit
	return out
}

func paWireMemorySource(value pa.MemorySource) contract.ProjectAssistantMemorySource {
	var out contract.ProjectAssistantMemorySource
	out.ID = value.ID
	out.Key = paWireBotKey(value.Key)
	out.Project = paWireProjectScope(value.Project)
	out.Kind = value.Kind
	out.Revision = value.Revision
	return out
}

func paWireMemoryRevision(value pa.MemoryRevision) contract.ProjectAssistantMemoryRevision {
	var out contract.ProjectAssistantMemoryRevision
	out.Key = paWireBotKey(value.Key)
	out.Project = paWireProjectScope(value.Project)
	out.Revision = value.Revision
	out.ForgottenThrough = value.ForgottenThrough
	out.ForgottenSourceIds = make([]string, len(value.ForgottenSourceIDs))
	for i := range value.ForgottenSourceIDs {
		out.ForgottenSourceIds[i] = value.ForgottenSourceIDs[i]
	}
	return out
}

func paWireMemoryEntry(value pa.MemoryEntry) contract.ProjectAssistantMemoryEntry {
	var out contract.ProjectAssistantMemoryEntry
	out.ID = value.ID
	out.Key = paWireBotKey(value.Key)
	out.Project = paWireProjectScope(value.Project)
	out.Text = value.Text
	out.Sources = make([]contract.ProjectAssistantMemorySource, len(value.Sources))
	for i := range value.Sources {
		out.Sources[i] = paWireMemorySource(value.Sources[i])
	}
	out.Revision = value.Revision
	out.Forgotten = value.Forgotten
	return out
}

func paWireContextSnapshot(value pa.ContextSnapshot) contract.ProjectAssistantContextSnapshot {
	var out contract.ProjectAssistantContextSnapshot
	out.Memory = paWireMemoryRevision(value.Memory)
	out.Text = value.Text
	out.Sources = make([]contract.ProjectAssistantMemorySource, len(value.Sources))
	for i := range value.Sources {
		out.Sources[i] = paWireMemorySource(value.Sources[i])
	}
	out.ProvenanceComplete = value.ProvenanceComplete
	return out
}

func paWireSourceStatus(value pa.SourceStatus) (contract.ProjectAssistantSourceStatus, error) {
	var out contract.ProjectAssistantSourceStatus
	out.MachineID = value.MachineID
	out.State = contract.ProjectAssistantSourceState(value.State)
	out.Readable = value.Readable
	out.Writable = value.Writable
	out.Fresh = value.Fresh
	convertedObservedAt, err := paWireTime(value.ObservedAt)
	if err != nil {
		return out, err
	}
	out.ObservedAt = convertedObservedAt
	out.Access = contract.ProjectAssistantAccess(value.Access)
	out.Capabilities = make([]contract.ProjectAssistantCapabilities, len(value.Capabilities))
	for i := range value.Capabilities {
		out.Capabilities[i] = paWireCapabilities(value.Capabilities[i])
	}
	return out, nil
}

func paWireReadCursor(value pa.ReadCursor) contract.ProjectAssistantReadCursor {
	var out contract.ProjectAssistantReadCursor
	out.Key = paWireBotKey(value.Key)
	out.ConversationID = value.ConversationID
	out.ReaderID = value.ReaderID
	out.MessageSeq = value.MessageSeq
	return out
}

func paWireBotSummary(value pa.BotSummary) (contract.ProjectAssistantBotSummary, error) {
	var out contract.ProjectAssistantBotSummary
	convertedBot, err := paWireBot(value.Bot)
	if err != nil {
		return out, err
	}
	out.Bot = convertedBot
	out.LatestMessage = value.LatestMessage
	out.Activity = value.Activity
	convertedActivityAt, err := paWireTime(value.ActivityAt)
	if err != nil {
		return out, err
	}
	out.ActivityAt = convertedActivityAt
	out.UnreadCount = value.UnreadCount
	out.ReadCursor = paWireReadCursor(value.ReadCursor)
	out.PendingPermissions = value.PendingPermissions
	out.Version = value.Version
	convertedSource, err := paWireSourceStatus(value.Source)
	if err != nil {
		return out, err
	}
	out.Source = convertedSource
	return out, nil
}

func paWireTrace(value pa.Trace) contract.ProjectAssistantTrace {
	var out contract.ProjectAssistantTrace
	out.TurnID = value.TurnID
	out.Category = value.Category
	out.Text = value.Text
	out.Complete = value.Complete
	return out
}

func paWireEvent(value pa.Event) (contract.ProjectAssistantEvent, error) {
	var out contract.ProjectAssistantEvent
	out.Version = value.Version
	out.ID = value.ID
	out.Seq = value.Seq
	out.Fence = paWireFence(value.Fence)
	out.Kind = contract.ProjectAssistantEventKind(value.Kind)
	convertedAt, err := paWireTime(value.At)
	if err != nil {
		return out, err
	}
	out.At = convertedAt
	if value.Message != nil {
		converted, err := paWireMessage(*value.Message)
		if err != nil {
			return out, err
		}
		out.Message = &converted
	}
	if value.Turn != nil {
		converted := paWireTurn(*value.Turn)
		out.Turn = &converted
	}
	if value.Permission != nil {
		converted, err := paWirePermissionOffer(*value.Permission)
		if err != nil {
			return out, err
		}
		out.Permission = &converted
	}
	if value.Work != nil {
		converted := paWireWorkLink(*value.Work)
		out.Work = &converted
	}
	if value.Memory != nil {
		converted := paWireMemoryRevision(*value.Memory)
		out.Memory = &converted
	}
	if value.Bot != nil {
		converted, err := paWireBotSummary(*value.Bot)
		if err != nil {
			return out, err
		}
		out.Bot = &converted
	}
	if value.Read != nil {
		converted := paWireReadCursor(*value.Read)
		out.Read = &converted
	}
	if value.Trace != nil {
		converted := paWireTrace(*value.Trace)
		out.Trace = &converted
	}
	if value.Source != nil {
		converted, err := paWireSourceStatus(*value.Source)
		if err != nil {
			return out, err
		}
		out.Source = &converted
	}
	return out, nil
}

func paWireCommand(value pa.Command) contract.ProjectAssistantCommand {
	var out contract.ProjectAssistantCommand
	out.Key = paWireBotKey(value.Key)
	out.Operation = contract.ProjectAssistantOperation(value.Operation)
	out.IdempotencyKey = value.IdempotencyKey
	out.ContentHash = value.ContentHash
	return out
}

func paWireReceipt(value pa.Receipt) contract.ProjectAssistantReceipt {
	var out contract.ProjectAssistantReceipt
	out.Command = paWireCommand(value.Command)
	out.ID = value.ID
	out.State = contract.ProjectAssistantEffectState(value.State)
	out.ResourceID = value.ResourceID
	out.EventSeq = value.EventSeq
	return out
}

func paWireCapabilities(value pa.Capabilities) contract.ProjectAssistantCapabilities {
	var out contract.ProjectAssistantCapabilities
	out.Key = paWireBotKey(value.Key)
	out.Project = paWireProjectScope(value.Project)
	out.Agent = contract.ProjectAssistantAgent(value.Agent)
	out.CliVersion = value.CLIVersion
	out.AdapterVersion = value.AdapterVersion
	out.Evidence = value.Evidence
	out.ReadOnlyEnforced = value.ReadOnlyEnforced
	out.NativeMemory = contract.ProjectAssistantNativeMemoryMode(value.NativeMemory)
	out.NativeMemoryVerified = value.NativeMemoryVerified
	out.Resume = value.Resume
	out.SendMessage = value.SendMessage
	if value.Unavailable != nil {
		converted := paWireFailure(*value.Unavailable)
		out.Unavailable = &converted
	}
	return out
}

func paWireFailure(value pa.Failure) contract.ProjectAssistantFailure {
	var out contract.ProjectAssistantFailure
	out.Code = contract.ProjectAssistantErrorCode(value.Code)
	out.Field = value.Field
	out.RetryAfterSeconds = value.RetryAfterSeconds
	return out
}

func paDomainBotKey(value contract.ProjectAssistantBotKey) pa.BotKey {
	var out pa.BotKey
	out.MachineID = value.MachineID
	out.BotID = value.BotID
	return out
}

func paDomainProjectScope(value contract.ProjectAssistantProjectScope) pa.ProjectScope {
	var out pa.ProjectScope
	out.MachineID = value.MachineID
	out.ProjectID = value.ProjectID
	out.CanonicalID = value.CanonicalID
	out.Kind = pa.ProjectKind(value.Kind)
	return out
}

func paDomainProjectSelection(value contract.ProjectAssistantProjectSelection) pa.ProjectSelection {
	var out pa.ProjectSelection
	out.MachineID = value.MachineID
	out.ProjectID = value.ProjectID
	return out
}

func paDomainAppearance(value contract.ProjectAssistantAppearance) pa.Appearance {
	var out pa.Appearance
	out.Shape = pa.Shape(value.Shape)
	out.Color = value.Color
	return out
}

func paDomainFence(value contract.ProjectAssistantFence) pa.Fence {
	var out pa.Fence
	out.Key = paDomainBotKey(value.Key)
	out.Project = paDomainProjectScope(value.Project)
	out.ConversationID = value.ConversationID
	out.BindingID = value.BindingID
	out.Generation = value.Generation
	return out
}

func paDomainCommand(value contract.ProjectAssistantCommand) pa.Command {
	var out pa.Command
	out.Key = paDomainBotKey(value.Key)
	out.Operation = pa.Operation(value.Operation)
	out.IdempotencyKey = value.IdempotencyKey
	out.ContentHash = value.ContentHash
	return out
}

func paDomainReadCursor(value contract.ProjectAssistantReadCursor) pa.ReadCursor {
	var out pa.ReadCursor
	out.Key = paDomainBotKey(value.Key)
	out.ConversationID = value.ConversationID
	out.ReaderID = value.ReaderID
	out.MessageSeq = value.MessageSeq
	return out
}

func paDomainMemorySource(value contract.ProjectAssistantMemorySource) pa.MemorySource {
	var out pa.MemorySource
	out.ID = value.ID
	out.Key = paDomainBotKey(value.Key)
	out.Project = paDomainProjectScope(value.Project)
	out.Kind = value.Kind
	out.Revision = value.Revision
	return out
}

func paDomainMemoryEntry(value contract.ProjectAssistantMemoryEntry) pa.MemoryEntry {
	var out pa.MemoryEntry
	out.ID = value.ID
	out.Key = paDomainBotKey(value.Key)
	out.Project = paDomainProjectScope(value.Project)
	out.Text = value.Text
	out.Sources = make([]pa.MemorySource, len(value.Sources))
	for i := range value.Sources {
		out.Sources[i] = paDomainMemorySource(value.Sources[i])
	}
	out.Revision = value.Revision
	out.Forgotten = value.Forgotten
	return out
}

func paWireExport(value app.Export) (contract.ProjectAssistantExport, error) {
	var out contract.ProjectAssistantExport
	out.ContractVersion = value.ContractVersion
	out.SchemaVersion = int64(value.SchemaVersion)
	convertedBot, err := paWireBot(value.Bot)
	if err != nil {
		return out, err
	}
	out.Bot = convertedBot
	out.Conversation = paWireConversation(value.Conversation)
	out.Context = paWireContextSnapshot(value.Context)
	out.Memory = paWireMemoryRevision(value.Memory)
	out.Bindings = make([]contract.ProjectAssistantExecutionBinding, len(value.Bindings))
	for i := range value.Bindings {
		out.Bindings[i] = paWireExecutionBinding(value.Bindings[i])
	}
	out.Events = make([]contract.ProjectAssistantEvent, len(value.Events))
	for i := range value.Events {
		converted, err := paWireEvent(value.Events[i])
		if err != nil {
			return out, err
		}
		out.Events[i] = converted
	}
	out.MemoryEntries = make([]contract.ProjectAssistantMemoryEntry, len(value.MemoryEntries))
	for i := range value.MemoryEntries {
		out.MemoryEntries[i] = paWireMemoryEntry(value.MemoryEntries[i])
	}
	out.Messages = make([]contract.ProjectAssistantMessage, len(value.Messages))
	for i := range value.Messages {
		converted, err := paWireMessage(value.Messages[i])
		if err != nil {
			return out, err
		}
		out.Messages[i] = converted
	}
	out.Permissions = make([]contract.ProjectAssistantPermissionOffer, len(value.Permissions))
	for i := range value.Permissions {
		converted, err := paWirePermissionOffer(value.Permissions[i])
		if err != nil {
			return out, err
		}
		out.Permissions[i] = converted
	}
	out.ReadCursors = make([]contract.ProjectAssistantReadCursor, len(value.ReadCursors))
	for i := range value.ReadCursors {
		out.ReadCursors[i] = paWireReadCursor(value.ReadCursors[i])
	}
	out.Receipts = make([]contract.ProjectAssistantReceipt, len(value.Receipts))
	for i := range value.Receipts {
		out.Receipts[i] = paWireReceipt(value.Receipts[i])
	}
	out.Turns = make([]contract.ProjectAssistantTurn, len(value.Turns))
	for i := range value.Turns {
		out.Turns[i] = paWireTurn(value.Turns[i])
	}
	out.WorkLinks = make([]contract.ProjectAssistantWorkLink, len(value.WorkLinks))
	for i := range value.WorkLinks {
		out.WorkLinks[i] = paWireWorkLink(value.WorkLinks[i])
	}
	return out, nil
}
