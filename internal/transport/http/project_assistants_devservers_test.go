package http

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"

	serverstore "github.com/sainteye/clawdline/internal/adapters/botdevserverstore"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestHumanServerHTTPAcceptsOnlyKnownIDsAndPreservesReadOnlyDeviceAuthority(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	ctx := context.Background()
	host := f.s.projectAssistants.host
	if err := host.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	bot, err := featurestore.New(f.s.store.ProjectAssistantDatabase()).Bot(ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := serverstore.NewCatalog(ctx, f.s.store.ProjectAssistantDatabase())
	if err != nil {
		t.Fatal(err)
	}
	saved := servers.SavedServer{ID: "known-server", Spec: servers.Spec{Key: f.key, Project: bot.Project, Name: "Synthetic dormant server", Command: "synthetic-never-executed", Directory: f.project}}
	if err := c.Create(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if err := host.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	base := "/v1/project-assistants/" + f.key.BotID + "/dev-servers"
	read := f.request(t, "GET", base+"?machine_id="+f.key.MachineID, f.readonly, nil)
	paServerOK(t, read)
	var list contract.ProjectAssistantDevServersResponse
	if err := json.Unmarshal(read.Body.Bytes(), &list); err != nil || len(list.Servers) != 1 || list.Servers[0].ID != saved.ID {
		t.Fatal(list, err)
	}
	info := list.Servers[0]
	if !info.PortsKnown || info.Ports == nil || len(info.Ports) != 0 {
		t.Fatal("dormant server did not encode a known empty port array", info)
	}
	request := contract.ProjectAssistantDevServerControlRequest{Key: paWireBotKey(f.key), ID: info.ID, Fingerprint: info.Fingerprint, Attempt: info.Attempt, Revision: info.Revision, IdempotencyKey: "remove-known"}
	if denied := f.request(t, "POST", base+"/"+info.ID+"/remove", f.readonly, request); denied.Code == 200 {
		t.Fatal("read-only device changed a source resource")
	}
	if denied := f.request(t, "POST", base+"/unknown/start", f.lan, request); denied.Code == 200 {
		t.Fatal("path/body mismatch was accepted")
	}
	raw, _ := json.Marshal(request)
	var extra map[string]any
	_ = json.Unmarshal(raw, &extra)
	extra["command"] = "must never execute"
	if denied := f.request(t, "POST", base+"/"+info.ID+"/start", f.lan, extra); denied.Code == 200 {
		t.Fatal("paired client supplied an arbitrary command")
	}
	paServerOK(t, f.request(t, "GET", base+"/"+info.ID+"/logs?machine_id="+f.key.MachineID, f.readonly, nil))
	paServerOK(t, f.request(t, "POST", base+"/"+info.ID+"/remove", f.lan, request))
	after := f.request(t, "GET", base+"?machine_id="+f.key.MachineID, f.readonly, nil)
	paServerOK(t, after)
	if err := json.Unmarshal(after.Body.Bytes(), &list); err != nil || len(list.Servers) != 0 {
		t.Fatal(list, err)
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("known-ID metadata controls opened a native provider")
	}
}

func TestServerSupportSignalPreservesSourceReadAuthority(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	for _, token := range []string{f.local, f.readonly} {
		rec := f.request(t, "GET", "/v1/project-assistants/status", token, nil)
		paServerOK(t, rec)
		var response contract.ProjectAssistantSourcesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || len(response.Sources) != 1 {
			t.Fatal(response, err)
		}
		source := response.Sources[0]
		if source.DevServersSupported != (runtime.GOOS != "windows") {
			t.Fatalf("source support not projected: %#v", source)
		}
		if !source.Readable || source.Writable != (token == f.local) {
			t.Fatalf("support changed reader authority: %#v", source)
		}
	}
}
