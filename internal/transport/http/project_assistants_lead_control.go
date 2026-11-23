package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The Lead uses the console's Bot operations through an in-process handler.
// Only these operations are exposed, even if other daemon routes are added.
var leadBotWrites = map[string]pa.Operation{
	"create": pa.CreateOperation, "edit": pa.EditOperation, "team-settings": pa.EditOperation,
	"visibility": pa.VisibilityOperation, "delete": pa.DeleteOperation, "stop": pa.StopOperation,
	"queue-edit": pa.QueueEditOperation, "send-now": pa.SendNowOperation, "react": pa.ReactOperation,
	"memory": pa.MemorySaveOperation, "forget": pa.ForgetOperation, "rule-remove": pa.RuleRemoveOperation,
	"handoff-card": pa.EditOperation, "rename-artifact": pa.RenameArtifactOperation, "network-settings": pa.NetworkSettingsOperation,
	"routine-save": pa.EditOperation, "routine-delete": pa.EditOperation, "routine-enabled": pa.EditOperation, "routine-test": pa.EditOperation,
	"group-create": pa.CreateOperation, "group-edit": pa.EditOperation, "group-delete": pa.DeleteOperation, "group-stop": pa.StopOperation, "group-react": pa.ReactOperation,
	"server-start": "", "server-restart": "", "server-stop": "", "server-remove": "",
}

func leadBotReadable(action string) bool {
	switch action {
	case "list", "get", "view", "history", "events", "send-receipt", "rules", "memory-view", "configuration", "extensions", "login-profiles", "network-settings", "handoff-card",
		"routines", "routine-runs", "routine-preview", "server-list", "server-logs", "groups", "group-history", "team-plans":
		return true
	default:
		return false
	}
}

func (s *Server) manageBots(ctx context.Context, service *app.Service, lead pa.BotKey, q app.LeadBotRequest) (json.RawMessage, error) {
	const base = "/v1/project-assistants"
	if (q.Path != "" && !strings.HasPrefix(q.Path, "/")) || strings.ContainsAny(q.Path, "%?#\\") {
		return nil, paHTTPFailure(pa.Invalid, "bot_control_path")
	}
	for _, segment := range strings.Split(strings.TrimPrefix(q.Path, "/"), "/") {
		if q.Path != "" && !paHTTPSafeBotID(segment) {
			return nil, paHTTPFailure(pa.Invalid, "bot_control_path")
		}
	}
	query := url.Values{}
	for k, v := range q.Query {
		query.Set(k, v)
	}
	u := &url.URL{Path: base + q.Path, RawQuery: query.Encode()}
	r := &http.Request{Method: q.Method, URL: u, Header: make(http.Header), Body: http.NoBody}
	r = r.WithContext(ctx)
	route, ok := paGroupRoute(r)
	if !ok {
		var err error
		route, err = paRoute(r)
		if err != nil {
			return nil, err
		}
	}
	if q.Method == http.MethodGet {
		if !leadBotReadable(route.action) || len(q.Body) != 0 || q.IdempotencyKey != "" {
			return nil, paHTTPFailure(pa.ScopeDenied, "bot_control_read")
		}
		if query.Has("machine_id") && query.Get("machine_id") != lead.MachineID {
			return nil, paHTTPFailure(pa.ScopeDenied, "machine_id")
		}
		query.Set("machine_id", lead.MachineID)
		if route.action == "list" {
			for k, v := range map[string]string{"query": "", "cursor": "", "page_size": strconv.Itoa(pa.MaxPageRows)} {
				if !query.Has(k) {
					query.Set(k, v)
				}
			}
		}
		u.RawQuery = query.Encode()
	} else {
		op, ok := leadBotWrites[route.action]
		if !ok {
			return nil, paHTTPFailure(pa.ScopeDenied, "bot_control_write")
		}
		if route.bot == lead.BotID && (route.action == "stop" || route.action == "delete" || route.action == "send-now") {
			return nil, paHTTPFailure(pa.ScopeDenied, "active_lead_turn")
		}
		if len(q.Query) != 0 || strings.TrimSpace(q.IdempotencyKey) == "" {
			return nil, paHTTPFailure(pa.Invalid, "idempotency_key")
		}
		body, key, err := s.leadBotWriteBody(ctx, service, lead, route, op, q)
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
	}
	attachments := s.projectAssistantAttachments()
	handler := newProjectAssistantGroupsHTTP(projectAssistantHTTPOptions{
		Service: service, MachineID: lead.MachineID, Attachments: attachments,
		Status: func(ctx context.Context) (pa.SourceStatus, error) {
			// Reads recheck the live role before publishing data. A write can
			// itself transfer the role, so its receipt uses the source check.
			if q.Method == http.MethodGet {
				if err := service.AuthorizeRead(ctx, lead); err != nil {
					return pa.SourceStatus{}, err
				}
			}
			return s.projectAssistantSource(ctx, lead, false)
		},
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, r)
	if response.Code >= http.StatusBadRequest {
		var result contract.ProjectAssistantErrorResponse
		if json.Unmarshal(response.Body.Bytes(), &result) == nil && result.Error.Code != "" {
			return nil, &pa.Failure{Code: pa.ErrorCode(result.Error.Code), Field: result.Error.Field, RetryAfterSeconds: result.Error.RetryAfterSeconds}
		}
		return nil, paHTTPFailure(pa.OutcomeUnknown, "bot_control_response")
	}
	return json.RawMessage(response.Body.Bytes()), nil
}

func (s *Server) leadBotWriteBody(ctx context.Context, service *app.Service, lead pa.BotKey, route paHTTPRoute, op pa.Operation, q app.LeadBotRequest) ([]byte, string, error) {
	body := map[string]json.RawMessage{}
	if len(q.Body) != 0 && (json.Unmarshal(q.Body, &body) != nil || body == nil) {
		return nil, "", paHTTPFailure(pa.Invalid, "body")
	}
	if _, exists := body["command"]; exists {
		return nil, "", paHTTPFailure(pa.Invalid, "command")
	}
	digest := sha256.Sum256([]byte(lead.BotID + "\x00" + q.IdempotencyKey))
	key := "lead-" + hex.EncodeToString(digest[:])
	bot := route.bot
	if bot == "" {
		bot = key
	}
	target := pa.BotKey{MachineID: lead.MachineID, BotID: bot}
	if op == "" {
		// Named dev-server actions have a dedicated control request.
		body["key"], _ = json.Marshal(paWireBotKey(target))
		body["id"], _ = json.Marshal(route.offer)
		body["idempotency_key"], _ = json.Marshal(key)
	} else {
		body["command"], _ = json.Marshal(paWireCommand(pa.Command{Key: target, Operation: op, IdempotencyKey: key}))
	}
	if route.action == "create" || route.action == "group-create" {
		var settings map[string]json.RawMessage
		if json.Unmarshal(body["settings"], &settings) != nil || settings == nil {
			return nil, "", paHTTPFailure(pa.Invalid, "settings")
		}
		selection := map[string]json.RawMessage{}
		if raw := body["folder"]; raw != nil {
			var folder string
			if settings["target"] != nil || json.Unmarshal(raw, &folder) != nil || strings.TrimSpace(folder) == "" {
				return nil, "", paHTTPFailure(pa.Invalid, "folder")
			}
			resolved, err := (projectAssistantProjects{s}).ResolveTeamFolder(ctx, lead.MachineID, folder)
			if err != nil {
				return nil, "", err
			}
			selection["project_id"], _ = json.Marshal(resolved.Scope.ProjectID)
			delete(body, "folder")
		} else if raw := settings["target"]; raw != nil && (json.Unmarshal(raw, &selection) != nil || selection == nil) {
			return nil, "", paHTTPFailure(pa.Invalid, "settings_target")
		}
		if selection["project_id"] == nil {
			selection["project_id"] = json.RawMessage(`""`)
		}
		if selection["machine_id"] == nil {
			selection["machine_id"], _ = json.Marshal(lead.MachineID)
		}
		settings["target"], _ = json.Marshal(selection)
		body["settings"], _ = json.Marshal(settings)
	}
	if route.action == "edit" || route.action == "team-settings" {
		settings := map[string]json.RawMessage{}
		if json.Unmarshal(body["settings"], &settings) != nil || settings == nil || settings["expected_version"] == nil {
			return nil, "", paHTTPFailure(pa.Invalid, "settings_expected_version")
		}
		if bot == lead.BotID && settings["roster"] != nil {
			var roster contract.ProjectAssistantRosterEdit
			if json.Unmarshal(settings["roster"], &roster) == nil && roster.FreshContext {
				return nil, "", paHTTPFailure(pa.ScopeDenied, "active_lead_turn")
			}
		}
		current, _, _, err := service.Get(ctx, target)
		if err != nil {
			return nil, "", err
		}
		for field, value := range map[string]any{"name": current.Name, "instructions": current.Instructions.Text, "appearance": paWireAppearance(current.Appearance)} {
			if _, exists := settings[field]; !exists {
				settings[field], _ = json.Marshal(value)
			}
		}
		if settings["mode"] != nil && settings["permission_policy"] == nil {
			settings["permission_policy"], _ = json.Marshal(current.PermissionPolicy)
		}
		if settings["permission_policy"] != nil && settings["mode"] == nil && settings["agent"] == nil && current.Mode != "" {
			settings["mode"], _ = json.Marshal(current.Mode)
		}
		body["settings"], _ = json.Marshal(settings)
	}
	encoded, err := json.Marshal(body)
	return encoded, key, err
}
