package http

import (
	"encoding/json"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"net/http"
)

func (h *projectAssistantHTTP) react(w http.ResponseWriter, r *http.Request, route paHTTPRoute) (any, error) {
	if _, err := paHTTPQuery(r); err != nil {
		return nil, err
	}
	q, err := paDecodeHTTP[struct {
		Command          contract.ProjectAssistantCommand `json:"command"`
		Fence            contract.ProjectAssistantFence   `json:"fence"`
		MessageID        string                           `json:"message_id"`
		Emoji            string                           `json:"emoji"`
		Present          *bool                            `json:"present"`
		ExpectedRevision *int64                           `json:"expected_revision"`
	}](w, r)
	if err != nil {
		return nil, err
	}
	if q.Present == nil || q.ExpectedRevision == nil {
		return nil, paHTTPFailure(pa.Invalid, "reaction_desired_state")
	}
	c, err := h.command(r, route, q.Command, pa.ReactOperation)
	if err != nil {
		return nil, err
	}
	f, err := paHTTPFence(c, q.Fence)
	if err != nil {
		return nil, err
	}
	receipt, check, err := h.options.Service.React(r.Context(), app.ReactionRequest{Command: c, Fence: f, MessageID: q.MessageID, Emoji: q.Emoji, Present: *q.Present, ExpectedRevision: *q.ExpectedRevision})
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(contract.ProjectAssistantReceiptResponse{ContractVersion: pa.ContractVersion, Receipt: paWireReceipt(receipt)})
	return paEncodedOutput{body: body, check: check, outcomeUnknown: true}, err
}

func (h *projectAssistantHTTP) reactToGroupLine(w http.ResponseWriter, r *http.Request, route paHTTPRoute) (any, error) {
	q, err := paDecodeHTTP[struct {
		Command          contract.ProjectAssistantCommand `json:"command"`
		ConversationID   string                           `json:"conversation_id"`
		GroupVersion     int64                            `json:"group_version"`
		MessageSeq       int64                            `json:"message_seq"`
		Emoji            string                           `json:"emoji"`
		Present          *bool                            `json:"present"`
		ExpectedRevision *int64                           `json:"expected_revision"`
	}](w, r)
	if err != nil {
		return nil, err
	}
	if q.Present == nil || q.ExpectedRevision == nil {
		return nil, paHTTPFailure(pa.Invalid, "reaction_desired_state")
	}
	c, err := h.command(r, route, q.Command, pa.ReactOperation)
	if err != nil {
		return nil, err
	}
	receipt, check, err := h.options.Service.ReactToGroupLine(r.Context(), app.GroupReactionRequest{
		Command: c, ConversationID: q.ConversationID, GroupVersion: q.GroupVersion, MessageSeq: q.MessageSeq,
		Emoji: q.Emoji, Present: *q.Present, ExpectedRevision: *q.ExpectedRevision,
	})
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(contract.ProjectAssistantReceiptResponse{ContractVersion: pa.ContractVersion, Receipt: paWireReceipt(receipt)})
	return paEncodedOutput{body: body, check: check, outcomeUnknown: true}, err
}
