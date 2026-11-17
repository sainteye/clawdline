package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantwebhook"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func routineWebhookLimits() projectassistantwebhook.Limits {
	return projectassistantwebhook.Limits{BodyBytes: pa.MaxRoutineWebhookBodyBytes, DeliveryIDBytes: pa.MaxRoutineDeliveryIDBytes, GitHubEventNameBytes: pa.MaxRoutineGitHubEventNameBytes, SecretMinBytes: pa.MaxRoutineWebhookSecretBytes}
}

func routineHookDeliveryRoute(r *http.Request) bool {
	id, ok := strings.CutPrefix(r.URL.Path, "/hooks/routines/")
	return ok && r.Method == http.MethodPost && paHTTPSafeBotID(id) && r.URL.EscapedPath() == r.URL.Path
}

func (s *Server) routineHooksRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !routineHookDeliveryRoute(r) || r.URL.RawQuery != "" {
		writeRoutineHookFailure(w, paHTTPFailure(pa.Invalid, "routine_webhook_shape"))
		return
	}
	body, err := projectassistantwebhook.ReadBody(r.Body, routineWebhookLimits())
	if err != nil {
		writeRoutineHookFailure(w, err)
		return
	}
	s.projectAssistants.mu.RLock()
	h := s.projectAssistants.host
	s.projectAssistants.mu.RUnlock()
	if h == nil {
		if err = s.StartProjectAssistants(r.Context()); err != nil {
			writeRoutineHookFailure(w, err)
			return
		}
		s.projectAssistants.mu.RLock()
		h = s.projectAssistants.host
		s.projectAssistants.mu.RUnlock()
	}
	if h == nil {
		writeRoutineHookFailure(w, paHTTPFailure(pa.Disabled, "routine_webhook"))
		return
	}
	service, err := h.Service()
	if err != nil {
		writeRoutineHookFailure(w, err)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/hooks/routines/")
	result, err := service.ReceiveRoutineHook(r.Context(), id, func(secret string) (app.RoutineDelivery, error) {
		in, e := projectassistantwebhook.Receive(secret, r.Header, body, routineWebhookLimits())
		return app.RoutineDelivery{DeliveryID: in.DeliveryID, Event: in.Event, Ignored: in.Ignored}, routineWebhookFailure(e)
	}, func(ctx context.Context, key pa.BotKey, project pa.ProjectScope) error {
		state, e := h.Status(ctx)
		if e != nil {
			return e
		}
		if e = state.CheckWrite(); e != nil {
			return e
		}
		source, e := s.projectAssistantSource(ctx, key, true)
		if e != nil {
			return e
		}
		return pa.CheckSource(source, true)
	})
	if err != nil {
		writeRoutineHookFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func routineWebhookFailure(err error) error {
	if f := projectassistantwebhook.FailureOf(err); f != nil {
		switch f.Code {
		case projectassistantwebhook.Unauthorized:
			return paHTTPFailure(pa.ScopeDenied, "routine_webhook_unauthorized")
		case projectassistantwebhook.Invalid:
			return paHTTPFailure(pa.Invalid, "routine_webhook_invalid")
		case projectassistantwebhook.TooLarge:
			return paHTTPFailure(pa.CapacityFull, "routine_webhook_body")
		case projectassistantwebhook.LimitsInvalid:
			return paHTTPFailure(pa.Unsupported, "routine_webhook_limits")
		}
	}
	return err
}

func writeRoutineHookFailure(w http.ResponseWriter, err error) {
	err = routineWebhookFailure(err)
	status, code := http.StatusInternalServerError, "webhook_unavailable"
	var failure *pa.Failure
	if errors.As(err, &failure) && failure.Field == "routine_webhook_body" {
		status, code = http.StatusRequestEntityTooLarge, "webhook_too_large"
	} else if errors.As(err, &failure) && failure.Field == "routine_webhook_limits" {
		code = "webhook_limits_invalid"
	} else {
		switch pa.Code(err) {
		case pa.CapacityFull:
			status, code = http.StatusConflict, "webhook_busy"
			w.Header().Set("Retry-After", "1")
		case pa.Revoked:
			status, code = http.StatusConflict, "webhook_paused"
		case pa.Invalid:
			status, code = http.StatusBadRequest, "webhook_invalid"
		case pa.ScopeDenied:
			status, code = http.StatusUnauthorized, "webhook_unauthorized"
		case pa.Disabled, pa.Unpaired:
			status, code = http.StatusForbidden, "webhook_disabled"
		case pa.StaleVersion:
			status, code = http.StatusConflict, "webhook_changed"
		case pa.OutcomeUnknown:
			status, code = http.StatusConflict, "webhook_outcome_unknown"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code string `json:"code"`
	}{code})
}

func (h *projectAssistantHTTP) routineHook(w http.ResponseWriter, r *http.Request, route paHTTPRoute) (any, error) {
	q, err := paDecodeHTTP[contract.ProjectAssistantRoutineHookRequest](w, r)
	if err != nil {
		return nil, err
	}
	if q.ID != route.offer {
		return nil, paHTTPFailure(pa.ScopeDenied, "routine_id")
	}
	c, err := h.command(r, route, q.Command, pa.EditOperation)
	if err != nil {
		return nil, err
	}
	f, err := paHTTPFence(c, q.Fence)
	if err != nil {
		return nil, err
	}
	check, err := h.options.Service.HumanRoutinePublication(r.Context(), c.Key)
	if err != nil {
		return nil, err
	}
	hook, err := h.options.Service.HumanRoutineHook(r.Context(), app.HumanRoutineCommand{Command: c, Fence: f, ExpectedRevision: q.ExpectedRevision, Request: app.RoutineRequest{ID: q.ID}}, q.Rotate)
	if err != nil {
		return nil, err
	}
	publicURL := ""
	if h.options.HookPublicURL != nil {
		publicURL = h.options.HookPublicURL(r.Context(), q.ID, hook.Key)
	}
	body, err := json.Marshal(contract.ProjectAssistantRoutineHookResponse{ContractVersion: pa.ContractVersion, URL: hook.URL, Key: hook.Key, PublicURL: publicURL})
	if err != nil {
		return nil, err
	}
	return paEncodedOutput{body: body, check: func(ctx context.Context) error {
		if err := check(ctx); err != nil {
			return err
		}
		if publicURL != "" && (h.options.HookPublicURL == nil || h.options.HookPublicURL(ctx, q.ID, hook.Key) != publicURL) {
			return paHTTPFailure(pa.StaleVersion, "routine_cloud_enrollment")
		}
		return nil
	}}, nil
}
