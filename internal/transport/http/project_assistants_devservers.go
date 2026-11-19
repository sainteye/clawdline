package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func paDevServerRoute(r *http.Request) (paHTTPRoute, bool) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/project-assistants/"), "/")
	if !strings.HasPrefix(r.URL.Path, "/v1/project-assistants/") || len(parts) < 2 || !paHTTPSafeBotID(parts[0]) || parts[1] != "dev-servers" {
		return paHTTPRoute{}, false
	}
	if len(parts) == 2 && r.Method == http.MethodGet {
		return paHTTPRoute{bot: parts[0], action: "server-list"}, true
	}
	if len(parts) == 4 && paHTTPSafeBotID(parts[2]) {
		if r.Method == http.MethodGet && parts[3] == "logs" {
			return paHTTPRoute{bot: parts[0], action: "server-logs", offer: parts[2]}, true
		}
		if r.Method == http.MethodPost {
			switch parts[3] {
			case "start", "restart", "stop", "remove":
				return paHTTPRoute{bot: parts[0], action: "server-" + parts[3], offer: parts[2]}, true
			}
		}
	}
	return paHTTPRoute{}, false
}

func paWireDevServer(info app.DevServerInfo) contract.ProjectAssistantDevServer {
	out := contract.ProjectAssistantDevServer{ID: info.ID, Name: info.Name, Command: info.Command, Folder: info.Folder, Attempt: info.Attempt, State: info.State, Failure: info.Failure, Fingerprint: info.Fingerprint, Revision: info.Revision, PortsKnown: info.PortsKnown, Ports: []contract.ProjectAssistantDevServerPort{}}
	if info.PortsKnown {
		for _, port := range info.Ports {
			remote := append([]string{}, port.Remote...)
			out.Ports = append(out.Ports, contract.ProjectAssistantDevServerPort{Port: int64(port.Port), Bind: port.Bind, Local: port.Local, Remote: remote})
		}
	}
	return out
}

func (h *projectAssistantHTTP) devServerHTTP(w http.ResponseWriter, r *http.Request, route paHTTPRoute) (any, error) {
	var result app.DevServerResult
	var key pa.BotKey
	var err error
	if r.Method == http.MethodGet {
		q, e := paHTTPQuery(r, "machine_id")
		if e != nil {
			return nil, e
		}
		key, err = h.queryKey(q, route)
		if err != nil {
			return nil, err
		}
		result, err = h.options.Service.DevServers(r.Context(), key, route.offer)
	} else {
		q, e := paDecodeHTTP[contract.ProjectAssistantDevServerControlRequest](w, r)
		if e != nil {
			return nil, e
		}
		key = pa.BotKey{MachineID: q.Key.MachineID, BotID: q.Key.BotID}
		if key.MachineID != h.options.MachineID || key.BotID != route.bot || q.ID != route.offer {
			return nil, paHTTPFailure(pa.ScopeDenied, "server_control")
		}
		result, err = h.options.Service.ControlDevServer(r.Context(), app.DevServerControl{Key: key, ID: q.ID, IdempotencyKey: q.IdempotencyKey, Fingerprint: q.Fingerprint, Attempt: q.Attempt, Revision: q.Revision, Action: strings.TrimPrefix(route.action, "server-")})
	}
	if err != nil {
		return nil, err
	}
	if err := h.options.Service.AuthorizeRead(r.Context(), key); err != nil {
		return nil, err
	}
	out := contract.ProjectAssistantDevServersResponse{ContractVersion: pa.ContractVersion, Servers: []contract.ProjectAssistantDevServer{}}
	for _, info := range result.Servers {
		out.Servers = append(out.Servers, paWireDevServer(info))
	}
	if result.Server != nil {
		info := paWireDevServer(*result.Server)
		out.Server = &info
	}
	if result.Log != nil {
		out.Log = &contract.ProjectAssistantDevServerLog{Text: result.Log.Text, Truncated: result.Log.Truncated}
	}
	body, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return paEncodedOutput{body: body, outcomeUnknown: r.Method != http.MethodGet, check: func(ctx context.Context) error { return h.options.Service.AuthorizeRead(ctx, key) }}, nil
}
