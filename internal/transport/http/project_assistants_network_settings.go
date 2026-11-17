package http

import (
	"context"
	"encoding/json"
	"net/http"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (h *projectAssistantHTTP) networkSettings(w http.ResponseWriter, r *http.Request, route paHTTPRoute) (any, error) {
	var view pa.NetworkSettingsView
	var check func(context.Context) error
	var err error
	if r.Method == http.MethodGet {
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			return nil, paHTTPFailure(pa.Invalid, "body")
		}
		q, e := paHTTPQuery(r, "machine_id", "conversation_id")
		if e != nil {
			return nil, e
		}
		key, e := h.queryKey(q, route)
		if e != nil {
			return nil, e
		}
		view, check, err = h.options.Service.NetworkSettings(r.Context(), app.MemoryReadRequest{Key: key, ConversationID: q.Get("conversation_id")})
	} else {
		if _, e := paHTTPQuery(r); e != nil {
			return nil, e
		}
		q, e := paDecodeHTTP[contract.ProjectAssistantNetworkSettingsRequest](w, r)
		if e != nil {
			return nil, e
		}
		command, e := h.command(r, route, q.Command, pa.NetworkSettingsOperation)
		if e != nil {
			return nil, e
		}
		patch := pa.NetworkSettingsPatch{}
		if q.Patch.OfferedToNetwork != nil {
			patch.OfferedToNetwork = &q.Patch.OfferedToNetwork.Enabled
		}
		if q.Patch.ReachableByAssistants != nil {
			patch.ReachableByAssistants = &q.Patch.ReachableByAssistants.Enabled
		}
		view, check, err = h.options.Service.SaveNetworkSettings(r.Context(), app.NetworkSettingsRequest{Command: command, Fence: paDomainFence(q.Fence), ExpectedVersion: q.ExpectedVersion, ExpectedRevision: q.ExpectedRevision, Patch: patch})
	}
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(struct {
		Contract string                 `json:"contract_version"`
		Network  pa.NetworkSettingsView `json:"network"`
	}{pa.ContractVersion, view})
	if err != nil {
		return nil, err
	}
	return paEncodedMemorySettings{body, check}, nil
}
