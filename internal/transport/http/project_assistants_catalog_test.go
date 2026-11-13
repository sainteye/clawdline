package http

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// Observe the generated local request and delegate unchanged to the production
// Router, Server.Handler, device gate and source-owned Project registry.
type paCatalogBridgeRouter struct {
	local cloudtransport.Router
	seen  []cloudops.LocalRequest
}

func (r *paCatalogBridgeRouter) Do(ctx context.Context, request cloudops.LocalRequest) (cloudops.LocalResponse, error) {
	r.seen = append(r.seen, request)
	return r.local.Do(ctx, request)
}

func TestProjectAssistantCatalogBridgeThroughServerAndRegistry(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	registry := projects.OpenPlaceRegistry(f.s.cfg.Dir)
	base := t.TempDir()
	paths := []string{}
	for _, parent := range []string{"first", "second"} {
		path := filepath.Join(base, parent, "same-name")
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	rows, err := registry.Add(paths, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("registered fixture has %d choices", len(rows))
	}
	pins := map[string]adaptercloud.PinnedDevice{
		"owner":  f.pin(t, "owner", adaptercloud.ScopeOperate, false),
		"reader": f.pin(t, "reader", adaptercloud.ScopeReadOnly, false),
	}
	router := &paCatalogBridgeRouter{local: cloudtransport.Router{Handler: f.h,
		Authorize: cloudtransport.LocalAuthorizer(f.local, f.machine)}}
	bridge := cloudops.Bridge{MachineID: f.key.MachineID, Router: router,
		AllowCommands: func() bool { return false }}
	sequence := uint64(0)
	// This starts at the verified, decrypted transport seam, as f.cloud does.
	// It supplies a synthetic pinned sender/key; it does not claim to test relay
	// encryption/signatures. The actual HTTP owner freshly checks the pin.
	ask := func(sender, machine, cursor string, size int) (cloudops.Answer, json.RawMessage) {
		t.Helper()
		sequence++
		correlation := fmt.Sprintf("catalog-%d", sequence)
		if sequence == 1 {
			correlation = strings.Repeat("<", 128)
		}
		raw, err := json.Marshal(map[string]any{"type": "project-assistants.projects",
			"session": cloudops.MachineReplySession, "request": correlation,
			"machine_id": machine, "cursor": cursor, "page_size": size})
		if err != nil {
			t.Fatal(err)
		}
		key, err := base64.StdEncoding.DecodeString(pins[sender].PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		before := len(router.seen)
		answer := bridge.Handle(context.Background(), cloudops.Command{Channel: "ctl/" + f.key.MachineID,
			Class: cloudops.ClassCtl, Sender: sender, VerifiedKey: ed25519.PublicKey(key), Sequence: sequence, Plaintext: raw})
		if len(router.seen) != before+1 {
			t.Fatalf("new vocabulary did not reach the real local router: %+v", answer)
		}
		request := router.seen[before]
		wantQuery := map[string]string{"machine_id": machine, "cursor": cursor, "page_size": fmt.Sprint(size)}
		if request.Method != "GET" || request.Path != "/v1/project-assistant-targets" ||
			!reflect.DeepEqual(request.Query, wantQuery) || request.Body != nil || request.Viewer != sender ||
			!reflect.DeepEqual(request.Header, map[string]string{actorHeader: actorDevice}) {
			t.Fatalf("captured route/query/body/device/sender changed: %+v", request)
		}
		var payload struct {
			Read   string          `json:"read"`
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(answer.Payload, &payload); err != nil || payload.Read != "read:"+correlation ||
			payload.Status != answer.Status || answer.Name != payload.Read || answer.Session != cloudops.MachineReplySession || answer.Mutating {
			t.Fatalf("full Cloud reply identity: %+v %s %v", answer, answer.Payload, err)
		}
		if f.providerCalls.Load() != 0 {
			t.Fatal("metadata initialized a native provider")
		}
		return answer, payload.Body
	}
	page := func(sender, cursor string) contract.ProjectAssistantProjectsResponse {
		t.Helper()
		answer, body := ask(sender, f.key.MachineID, cursor, 2)
		var response contract.ProjectAssistantProjectsResponse
		if !answer.OK() || json.Unmarshal(body, &response) != nil || response.ContractVersion != pa.ContractVersion ||
			response.Source.MachineID != f.key.MachineID || !response.Source.Readable || !response.Source.Fresh ||
			response.Source.State != contract.ProjectAssistantSourceStateOnline ||
			response.Source.Access != contract.ProjectAssistantAccessInteractive || response.Revision == "" ||
			!reflect.DeepEqual(response.Agents, []contract.ProjectAssistantAgent{contract.ProjectAssistantAgentClaude, contract.ProjectAssistantAgentCodex}) ||
			len(response.Source.Capabilities) != 0 {
			t.Fatalf("source-owned catalog DTO was lost: status=%d body=%s", answer.Status, body)
		}
		if response.Source.Writable != (sender == "owner") {
			t.Fatalf("catalog changed current source write authority: %+v", response.Source)
		}
		return response
	}
	first := page("owner", "")
	if len(first.Items) != 2 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first complete page: %+v", first)
	}
	second := page("owner", first.NextCursor)
	if len(second.Items) != 1 || second.HasMore || second.NextCursor != "" || second.Revision != first.Revision {
		t.Fatalf("second complete page: %+v", second)
	}
	want := map[string]string{}
	for _, row := range rows {
		want[projects.PlaceID(row.Path)] = filepath.Base(row.Path)
	}
	ids := []string{}
	for _, item := range append(first.Items, second.Items...) {
		if item.Target.MachineID != f.key.MachineID || item.Label != want[item.Target.ProjectID] {
			t.Fatalf("registered identity/label changed: %+v", item)
		}
		ids = append(ids, item.Target.ProjectID)
		delete(want, item.Target.ProjectID)
	}
	if len(want) != 0 || !sort.StringsAreSorted(ids) {
		t.Fatalf("paged catalog repeated, omitted or reordered registered IDs: %v %v", ids, want)
	}
	readonly := page("reader", "")
	if !reflect.DeepEqual(readonly.Items, first.Items) || readonly.Source.Writable {
		t.Fatal("read-only grant lost metadata or became writable")
	}
	refusal := func(sender, machine, cursor string, status int, code pa.ErrorCode, field string) {
		t.Helper()
		answer, body := ask(sender, machine, cursor, 2)
		var response contract.ProjectAssistantErrorResponse
		if json.Unmarshal(body, &response) != nil || answer.Status != status || answer.Code != string(code) ||
			response.ContractVersion != pa.ContractVersion || response.Error.Code != contract.ProjectAssistantErrorCode(code) ||
			response.Error.Field != field || response.Error.RetryAfterSeconds != 0 || strings.Contains(string(body), `"items"`) {
			t.Fatalf("full typed source refusal was lost: status=%d code=%s body=%s", answer.Status, answer.Code, body)
		}
	}
	refusal("owner", "another-machine", "", 403, pa.ScopeDenied, "machine_id")
	// A real registry mutation invalidates the previously minted source cursor.
	if _, err := registry.Add([]string{t.TempDir()}, time.Now()); err != nil {
		t.Fatal(err)
	}
	refusal("owner", f.key.MachineID, first.NextCursor, 410, pa.CursorExpired, "projects_cursor")
	if _, err := adaptercloud.NewPinnedStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName)).Revoke("owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	refusal("owner", f.key.MachineID, "", 403, pa.Revoked, "viewer_grant")
	if f.providerCalls.Load() != 0 {
		t.Fatal("Bridge/HTTP metadata path initialized a provider")
	}
	t.Logf("real Bridge→Router→Server/gate→PlaceRegistry: %d complete requests, provider calls=%d", len(router.seen), f.providerCalls.Load())
}
