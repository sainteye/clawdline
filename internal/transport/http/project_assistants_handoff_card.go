package http

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type paEncodedHandoff struct {
	body  []byte
	check func(context.Context) error
	write bool
}

func paDomainHandoffRef(ref *contract.ProjectAssistantHandoffRef) *pa.HandoffRef {
	if ref == nil {
		return nil
	}
	return &pa.HandoffRef{ID: ref.ID, Revision: ref.Revision}
}

func (h *projectAssistantHTTP) handoffCard(w http.ResponseWriter, r *http.Request, route paHTTPRoute) (any, error) {
	service, ctx := h.options.Service, r.Context()
	var view app.HandoffView
	var key pa.BotKey
	var conversation string
	isWrite := r.Method == http.MethodPost
	if isWrite {
		if _, err := paHTTPQuery(r); err != nil {
			return nil, err
		}
		request, err := paDecodeHTTP[contract.ProjectAssistantHandoffRequest](w, r)
		if err != nil {
			return nil, err
		}
		command, err := h.command(r, route, request.Command, pa.EditOperation)
		if err != nil {
			return nil, err
		}
		fence, err := paHTTPFence(command, request.Fence)
		if err != nil {
			return nil, err
		}
		key, conversation = command.Key, fence.ConversationID
		view, err = service.WriteHandoff(ctx, app.HandoffRequest{Command: command, Fence: fence,
			Action: app.HandoffAction(request.Action), Ref: paDomainHandoffRef(request.Ref), Text: request.Text})
		if err != nil {
			return nil, err
		}
	} else {
		q, err := paHTTPQuery(r, "machine_id", "conversation_id")
		if err != nil {
			return nil, err
		}
		key, err = h.queryKey(q, route)
		if err != nil {
			return nil, err
		}
		conversation = q.Get("conversation_id")
		view, err = service.ReadHandoff(ctx, key, conversation)
		if err != nil {
			return nil, err
		}
	}
	body := contract.ProjectAssistantHandoffResponse{ContractVersion: pa.ContractVersion, Fence: paWireFence(view.Fence), CanPrepare: view.CanPrepare}
	if card := view.Card; card != nil {
		body.Card = &contract.ProjectAssistantHandoffCard{
			Ref:   contract.ProjectAssistantHandoffRef{ID: card.Ref.ID, Revision: card.Ref.Revision},
			State: contract.ProjectAssistantHandoffCardState(card.State), Text: card.Text,
			BookmarkMessageID: card.BookmarkMessageID, TurnID: card.TurnID,
		}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return paEncodedHandoff{body: encoded, write: isWrite, check: func(ctx context.Context) error {
		current, err := service.ReadHandoff(ctx, key, conversation)
		if err == nil && !reflect.DeepEqual(view, current) {
			return paHTTPFailure(pa.StaleVersion, "handoff_publication")
		}
		return err
	}}, nil
}
