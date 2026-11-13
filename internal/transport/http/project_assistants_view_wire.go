package http

import (
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func paWireReadTurn(v app.ReadTurnSummary) contract.ProjectAssistantReadTurnSummary {
	return contract.ProjectAssistantReadTurnSummary{ID: v.ID, Fence: paWireFence(v.Fence), MessageID: v.MessageID,
		State: contract.ProjectAssistantTurnState(v.State), FailureCode: string(v.FailureCode)}
}

func paWireReadHead(v app.ReadViewHead) contract.ProjectAssistantReadViewHead {
	return contract.ProjectAssistantReadViewHead{Key: paWireBotKey(v.Key), Project: paWireProjectScope(v.Project),
		Conversation: paWireConversation(v.Conversation), CurrentFence: paWireFence(v.CurrentFence),
		BotVersion: v.BotVersion, MemoryRevision: v.MemoryRevision, EventFrontier: v.EventFrontier,
		UnknownTurnCount: v.UnknownTurnCount, PendingPermissionCount: v.PendingPermissionCount, TraceCategoryCount: v.TraceCategoryCount}
}

func paWireReadSnapshot(v app.ReadSnapshot) (*contract.ProjectAssistantReadSnapshot, error) {
	bot, err := paWireBot(v.Bot)
	if err != nil {
		return nil, err
	}
	out := &contract.ProjectAssistantReadSnapshot{Bot: bot,
		PendingTurns:   make([]contract.ProjectAssistantReadTurnSummary, len(v.PendingTurns)),
		QueuedMessages: make([]contract.ProjectAssistantMessage, len(v.QueuedMessages))}
	for i, turn := range v.PendingTurns {
		out.PendingTurns[i] = paWireReadTurn(turn)
	}
	for i, message := range v.QueuedMessages {
		out.QueuedMessages[i], err = paWireMessage(message)
		if err != nil {
			return nil, err
		}
	}
	if v.LastTurn != nil {
		turn := paWireReadTurn(*v.LastTurn)
		out.LastTurn = &turn
	}
	return out, nil
}

func paWireReadItem(v app.ReadViewItem) (contract.ProjectAssistantReadViewItem, error) {
	out := contract.ProjectAssistantReadViewItem{ID: v.ID, Kind: contract.ProjectAssistantReadItemKind(v.Kind),
		AnchorMessageSeq: v.AnchorMessageSeq, ObservedEventSeq: v.ObservedEventSeq}
	count := 0
	for _, present := range []bool{v.Message != nil, v.Work != nil, v.PermissionSummary != nil,
		v.TraceSummary != nil, v.Permission != nil, v.Trace != nil, v.Event != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return out, paHTTPFailure(pa.Invalid, "view_item_payload")
	}
	var err error
	switch {
	case v.Kind == app.ReadItemMessage && v.Message != nil:
		var item contract.ProjectAssistantMessage
		item, err = paWireMessage(*v.Message)
		out.Message = &item
	case v.Kind == app.ReadItemWork && v.Work != nil:
		item := paWireWorkLink(*v.Work)
		out.Work = &item
	case v.Kind == app.ReadItemPermissionSummary && v.PermissionSummary != nil:
		v := v.PermissionSummary
		var expires string
		expires, err = paWireTime(v.ExpiresAt)
		out.PermissionSummary = &contract.ProjectAssistantReadPermissionSummary{ID: v.ID, Fence: paWireFence(v.Fence),
			TurnID: v.TurnID, State: contract.ProjectAssistantPermissionState(v.State), ExpiresAt: expires, DetailCursor: v.DetailCursor}
	case v.Kind == app.ReadItemTraceSummary && v.TraceSummary != nil:
		v := v.TraceSummary
		out.TraceSummary = &contract.ProjectAssistantReadTraceSummary{ID: v.ID, TurnID: v.TurnID,
			Category: v.Category, Complete: v.Complete, DetailCursor: v.DetailCursor}
	case v.Kind == app.ReadItemPermission && v.Permission != nil:
		var item contract.ProjectAssistantPermissionOffer
		item, err = paWirePermissionOffer(*v.Permission)
		out.Permission = &item
	case v.Kind == app.ReadItemTrace && v.Trace != nil:
		item := paWireTrace(*v.Trace)
		out.Trace = &item
	case v.Kind == app.ReadItemEvent && v.Event != nil:
		var item contract.ProjectAssistantEvent
		item, err = paWireEvent(*v.Event)
		out.Event = &item
	default:
		return out, paHTTPFailure(pa.Invalid, "view_item_kind")
	}
	return out, err
}
