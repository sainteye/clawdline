package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The host mounts this wrapper at the existing Bot prefix. Keeping this
// composition separate lets older Bot routes retain their existing parser.
func newProjectAssistantGroupsHTTP(options projectAssistantHTTPOptions) http.Handler {
	return &projectAssistantGroupsHTTP{projectAssistantHTTP{options: options}}
}

type projectAssistantGroupsHTTP struct{ projectAssistantHTTP }

func (h *projectAssistantGroupsHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	route, ok := paGroupRoute(r)
	if !ok {
		h.projectAssistantHTTP.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var payload any
	source, err := h.status(r.Context(), true)
	if err == nil && h.options.Service == nil {
		err = paHTTPFailure(pa.Disabled, "feature")
	}
	if err == nil && r.Method != http.MethodGet {
		err = pa.CheckSource(source, true)
	}
	if err == nil {
		if r.Method == http.MethodGet {
			payload, err = h.readGroups(r, route)
		} else {
			if _, err = paHTTPQuery(r); err == nil {
				payload, err = h.writeGroup(w, r, route)
			}
		}
	}
	if err != nil {
		paWriteHTTPFailure(w, err)
		return
	}
	var encoded []byte
	if output, ok := payload.(paEncodedOutput); ok {
		if h.options.BeforeViewPublish != nil {
			h.options.BeforeViewPublish()
		}
		err = output.check(r.Context())
		encoded = output.body
	} else {
		encoded, err = json.Marshal(payload)
	}
	if err == nil {
		_, err = h.status(r.Context(), true)
	}
	if err != nil {
		paWriteHTTPFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(encoded)
}

// Called before the generic Bot-id route. Groups share source authority and
// HTTP publication fences, but never masquerade as executable Bots.
func paGroupRoute(r *http.Request) (paHTTPRoute, bool) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/project-assistants/"), "/")
	if len(parts) == 2 && paHTTPSafeBotID(parts[0]) {
		action := ""
		if parts[1] == "team-plans" && r.Method == http.MethodGet {
			action = "team-plans"
		}
		if parts[1] == "team-answer" && r.Method == http.MethodPost {
			action = "team-answer"
		}
		if parts[1] == "team-settings" && r.Method == http.MethodPost {
			action = "team-settings"
		}
		if action != "" {
			return paHTTPRoute{bot: parts[0], action: action}, true
		}
	}
	if len(parts) < 2 || parts[0] != "rooms" || parts[1] != "groups" {
		return paHTTPRoute{}, false
	}
	parts = parts[1:]
	out := paHTTPRoute{}
	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			out.action = "groups"
		}
		if r.Method == http.MethodPost {
			out.action = "group-create"
		}
	}
	if len(parts) >= 2 && paHTTPSafeBotID(parts[1]) {
		out.bot = parts[1]
		if len(parts) == 2 {
			if r.Method == http.MethodGet {
				out.action = "group-history"
			}
			if r.Method == http.MethodPatch {
				out.action = "group-edit"
			}
			if r.Method == http.MethodDelete {
				out.action = "group-delete"
			}
		}
		if len(parts) == 3 && r.Method == http.MethodPost {
			if parts[2] == "messages" {
				out.action = "group-send"
			}
			if parts[2] == "stop" {
				out.action = "group-stop"
			}
		}
	}
	// Reserve all paths under groups, including invalid methods; do not fall
	// through and read a Bot named groups on an invalid group request.
	if out.action == "" {
		out.action = "group-invalid"
	}
	return out, true
}

func paWireGroup(g pa.Group) contract.ProjectAssistantGroup {
	out := contract.ProjectAssistantGroup{Key: paWireBotKey(g.Key), Project: paWireProjectScope(g.Project), ConversationID: g.ConversationID, Name: g.Name, MembershipRevision: g.MembershipRevision, Version: g.Version, Deleted: g.Deleted, CreatedAt: paRoutineTime(g.CreatedAt), UpdatedAt: paRoutineTime(g.UpdatedAt), Members: []contract.ProjectAssistantGroupMember{}}
	for _, m := range g.Members {
		out.Members = append(out.Members, contract.ProjectAssistantGroupMember{Key: paWireBotKey(m.Key), JoinedRevision: m.JoinedRevision})
	}
	return out
}
func paWireRoom(st pa.RoomState) contract.ProjectAssistantRoomState {
	return contract.ProjectAssistantRoomState{Revision: st.Revision, Epoch: st.Epoch, Running: st.Running, Pending: st.Pending, StoppedAt: st.StoppedAt}
}
func paWireRoomEntry(e pa.RoomEntry) contract.ProjectAssistantRoomEntry {
	out := contract.ProjectAssistantRoomEntry{Seq: e.Seq, Kind: string(e.Kind), Text: e.Text}
	if e.Author != (pa.BotKey{}) {
		k := paWireBotKey(e.Author)
		out.Author = &k
	}
	if e.ReplyToAuthor != (pa.BotKey{}) {
		k := paWireBotKey(e.ReplyToAuthor)
		out.ReplyToAuthor = &k
	}
	return out
}

// Keep the full JSON object or refuse it. Cloud's existing plaintext bound
// includes its actual reply envelope; no truncated card or invented cursor.
func paGroupOutput(value any, check func(context.Context) error) (any, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	size, err := cloudops.ProjectAssistantReadReplySize("read:"+strings.Repeat("<", 128), http.StatusOK, body)
	if err != nil {
		return nil, err
	}
	if len(body) > cloudops.ReadPlaintextBytesLimit || size > cloudops.ReadPlaintextBytesLimit {
		return nil, paHTTPFailure(pa.CapacityFull, "group_reply_bytes")
	}
	return paEncodedOutput{body: body, check: check}, nil
}

func (h *projectAssistantHTTP) readGroups(r *http.Request, route paHTTPRoute) (any, error) {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return nil, paHTTPFailure(pa.Invalid, "body")
	}
	fields := []string{"machine_id"}
	if route.action == "group-history" {
		fields = append(fields, "before_seq", "page_size")
	}
	q, err := paHTTPQuery(r, fields...)
	if err != nil {
		return nil, err
	}
	key, err := h.queryKey(q, route)
	if err != nil {
		return nil, err
	}
	check, err := h.options.Service.GroupPublication(r.Context(), key)
	if err != nil {
		return nil, err
	}
	if route.action == "team-plans" {
		plans, err := h.options.Service.TeamPlans(r.Context(), key)
		if err != nil {
			return nil, err
		}
		out := contract.ProjectAssistantTeamPlansResponse{ContractVersion: pa.ContractVersion, Plans: []contract.ProjectAssistantTeamPlan{}}
		for _, p := range plans {
			out.Plans = append(out.Plans, paWireTeamPlan(p))
		}
		return paGroupOutput(out, check)
	}
	source, err := h.status(r.Context(), true)
	if err != nil {
		return nil, err
	}
	wire, err := paWireSourceStatus(source)
	if err != nil {
		return nil, err
	}
	if route.action == "groups" {
		groups, err := h.options.Service.Groups(r.Context(), key.MachineID)
		if err != nil {
			return nil, err
		}
		out := contract.ProjectAssistantGroupListResponse{ContractVersion: pa.ContractVersion, Source: wire, Groups: []contract.ProjectAssistantGroup{}}
		for _, g := range groups {
			out.Groups = append(out.Groups, paWireGroup(g))
		}
		return paGroupOutput(out, check)
	}
	if route.action != "group-history" {
		return nil, paHTTPFailure(pa.Invalid, "group_route")
	}
	before, err := paHTTPNumber(q, "before_seq")
	if err != nil {
		return nil, err
	}
	size, err := paHTTPNumber(q, "page_size")
	if err != nil || size < 1 || size > pa.MaxPageRows {
		return nil, paHTTPFailure(pa.Invalid, "page_size")
	}
	page, err := h.options.Service.GroupHistory(r.Context(), app.GroupHistoryQuery{Key: key, BeforeSeq: before, PageSize: int(size)})
	if err != nil {
		return nil, err
	}
	out := contract.ProjectAssistantGroupHistoryResponse{ContractVersion: pa.ContractVersion, Source: wire, Group: paWireGroup(page.Group), Room: paWireRoom(page.Room), Lines: []contract.ProjectAssistantRoomEntry{}}
	for _, e := range page.Lines {
		out.Lines = append(out.Lines, paWireRoomEntry(e))
	}
	if len(page.Lines) == int(size) {
		out.HasMore = true
		out.NextBeforeSeq = page.Lines[0].Seq
	}
	return paPackGroupHistory(out, check)
}

// Keep the newest whole lines that fit the existing Cloud envelope. The
// continuation is the oldest retained sequence, so the next read neither
// skips an older line nor repeats the retained page. A single oversized line
// is refused; its text is never cut to make a success-shaped response.
func paPackGroupHistory(out contract.ProjectAssistantGroupHistoryResponse, check func(context.Context) error) (any, error) {
	if encoded, err := paGroupOutput(out, check); err == nil {
		return encoded, nil
	} else if pa.Code(err) != pa.CapacityFull {
		return nil, err
	}
	low, high := 1, len(out.Lines)-1
	var best any
	for low <= high {
		count := (low + high) / 2
		candidate := out
		candidate.Lines = out.Lines[len(out.Lines)-count:]
		candidate.HasMore = true
		candidate.NextBeforeSeq = candidate.Lines[0].Seq
		encoded, err := paGroupOutput(candidate, check)
		if err == nil {
			best = encoded
			low = count + 1
		} else if pa.Code(err) == pa.CapacityFull {
			high = count - 1
		} else {
			return nil, err
		}
	}
	if best == nil {
		return nil, paHTTPFailure(pa.CapacityFull, "group_reply_bytes")
	}
	return best, nil
}

func paGroupKeys(keys []contract.ProjectAssistantBotKey) []pa.BotKey {
	out := make([]pa.BotKey, len(keys))
	for i, k := range keys {
		out[i] = paDomainBotKey(k)
	}
	return out
}
func (h *projectAssistantHTTP) writeGroup(w http.ResponseWriter, r *http.Request, route paHTTPRoute) (any, error) {
	switch route.action {
	case "group-create":
		v, err := paDecodeHTTP[contract.ProjectAssistantGroupCreateRequest](w, r)
		if err != nil {
			return nil, err
		}
		c, err := h.command(r, route, v.Command, pa.CreateOperation)
		if err != nil {
			return nil, err
		}
		g, receipt, err := h.options.Service.CreatePersonGroup(r.Context(), c, app.CreateGroup{Target: paDomainProjectSelection(v.Settings.Target), Name: v.Settings.Name, Members: paGroupKeys(v.Settings.Members)})
		if err != nil {
			return nil, err
		}
		return contract.ProjectAssistantGroupResponse{ContractVersion: pa.ContractVersion, Group: paWireGroup(g), Receipt: paWireReceipt(receipt)}, nil
	case "group-edit":
		v, err := paDecodeHTTP[contract.ProjectAssistantGroupEditRequest](w, r)
		if err != nil {
			return nil, err
		}
		c, err := h.command(r, route, v.Command, pa.EditOperation)
		if err != nil {
			return nil, err
		}
		g, receipt, err := h.options.Service.EditGroup(r.Context(), c, pa.GroupEdit{ExpectedVersion: v.Settings.ExpectedVersion, Name: v.Settings.Name, Members: paGroupKeys(v.Settings.Members)})
		if err != nil {
			return nil, err
		}
		return contract.ProjectAssistantGroupResponse{ContractVersion: pa.ContractVersion, Group: paWireGroup(g), Receipt: paWireReceipt(receipt)}, nil
	case "group-delete":
		v, err := paDecodeHTTP[contract.ProjectAssistantGroupDeleteRequest](w, r)
		if err != nil {
			return nil, err
		}
		c, err := h.command(r, route, v.Command, pa.DeleteOperation)
		if err != nil {
			return nil, err
		}
		return paHTTPReceipt(h.options.Service.DeleteGroup(r.Context(), c, v.ExpectedVersion))
	case "group-stop":
		v, err := paDecodeHTTP[contract.ProjectAssistantGroupStopRequest](w, r)
		if err != nil {
			return nil, err
		}
		c, err := h.command(r, route, v.Command, pa.StopOperation)
		if err != nil {
			return nil, err
		}
		st, err := h.options.Service.StopGroup(r.Context(), c.Key)
		if err != nil {
			return nil, err
		}
		return contract.ProjectAssistantGroupStopResponse{ContractVersion: pa.ContractVersion, Room: paWireRoom(st.State), Stopped: st.Stopped}, nil
	case "group-send":
		v, err := paDecodeHTTP[contract.ProjectAssistantGroupSendRequest](w, r)
		if err != nil {
			return nil, err
		}
		c, err := h.command(r, route, v.Command, pa.SendOperation)
		if err != nil {
			return nil, err
		}
		var reply pa.BotKey
		if v.ReplyToAuthor != nil {
			reply = paDomainBotKey(*v.ReplyToAuthor)
		}
		e, st, err := h.options.Service.SendGroup(r.Context(), c, v.Text, reply)
		if err != nil {
			return nil, err
		}
		return contract.ProjectAssistantGroupSendResponse{ContractVersion: pa.ContractVersion, Entry: paWireRoomEntry(e), Room: paWireRoom(st.State)}, nil
	case "team-settings":
		v, err := paDecodeHTTP[contract.ProjectAssistantTeamSettingsRequest](w, r)
		if err != nil {
			return nil, err
		}
		c, err := h.command(r, route, v.Command, pa.EditOperation)
		if err != nil {
			return nil, err
		}
		settings := pa.EditBot{FallbackAgents: paDomainOptionalAgents(v.Settings.FallbackAgents), ExpectedVersion: v.Settings.ExpectedVersion, Name: v.Settings.Name, Appearance: paDomainAppearance(v.Settings.Appearance), Instructions: v.Settings.Instructions, PermissionPolicy: pa.PermissionPolicy(v.Settings.PermissionPolicy), Mode: v.Settings.Mode, ManageTeam: &v.Settings.ManageTeam}
		return paHTTPBot(h.options.Service.Edit(r.Context(), c, settings))
	case "team-answer":
		v, err := paDecodeHTTP[contract.ProjectAssistantTeamAnswerRequest](w, r)
		if err != nil {
			return nil, err
		}
		c, err := h.command(r, route, v.Command, pa.AnswerOperation)
		if err != nil {
			return nil, err
		}
		state, err := h.options.Service.AnswerTeamPlan(r.Context(), c, v.PlanID, v.Allow)
		if err != nil {
			return nil, err
		}
		return contract.ProjectAssistantTeamAnswerResponse{ContractVersion: pa.ContractVersion, PlanID: v.PlanID, State: state}, nil
	}
	return nil, paHTTPFailure(pa.Invalid, "group_route")
}

func paWireTeamPlan(p app.TeamPlan) contract.ProjectAssistantTeamPlan {
	return contract.ProjectAssistantTeamPlan{ID: p.ID, Manager: paWireBotKey(p.Manager), ManagerName: p.ManagerName, Target: paWireBotKey(p.Target), Operation: string(p.Operation), Detail: p.Detail, ExpiresAt: paRoutineTime(p.ExpiresAt)}
}
