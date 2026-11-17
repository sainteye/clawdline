package http

import (
	"context"
	"encoding/json"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"net/http"
)

func (h *projectAssistantHTTP) loginProfiles(w http.ResponseWriter, r *http.Request, route paHTTPRoute) (any, error) {
	var value any
	var check func(context.Context) error
	if route.action == "extensions" {
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
		catalog, c, e := h.options.Service.Extensions(r.Context(), app.MemoryReadRequest{Key: key, ConversationID: q.Get("conversation_id")})
		if e != nil {
			return nil, e
		}
		value = struct {
			Contract string              `json:"contract_version"`
			Catalog  pa.ExtensionCatalog `json:"catalog"`
		}{pa.ContractVersion, catalog}
		check = c
	} else if route.action == "login-profiles" {
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			return nil, paHTTPFailure(pa.Invalid, "body")
		}
		q, e := paHTTPQuery(r, "machine_id", "conversation_id", "agent")
		if e != nil {
			return nil, e
		}
		key, e := h.queryKey(q, route)
		if e != nil {
			return nil, e
		}
		profiles, c, e := h.options.Service.LoginProfiles(r.Context(), app.MemoryReadRequest{Key: key, ConversationID: q.Get("conversation_id")}, pa.Agent(q.Get("agent")))
		if e != nil {
			return nil, e
		}
		value = struct {
			Contract string            `json:"contract_version"`
			Profiles []pa.LoginProfile `json:"profiles"`
		}{pa.ContractVersion, profiles}
		check = c
	} else {
		if _, e := paHTTPQuery(r); e != nil {
			return nil, e
		}
		q, e := paDecodeHTTP[contract.ProjectAssistantLoginProfileAction](w, r)
		if e != nil {
			return nil, e
		}
		c, e := h.command(r, route, q.Command, pa.LoginProfileOperation)
		if e != nil {
			return nil, e
		}
		profile, revalidate, e := h.options.Service.LoginProfileAction(r.Context(), pa.LoginProfileAction{Command: c, Fence: paDomainFence(q.Fence), ExpectedVersion: q.ExpectedVersion, ProfileID: q.ProfileID, Revision: q.Revision, Agent: pa.Agent(q.Agent), Action: q.Action, Name: q.Name})
		if e != nil {
			return nil, e
		}
		value = struct {
			Contract string          `json:"contract_version"`
			Profile  pa.LoginProfile `json:"profile"`
		}{pa.ContractVersion, profile}
		check = revalidate
	}
	body, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	return paEncodedMemorySettings{body, check}, nil
}
