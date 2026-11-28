package http

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	serverstore "github.com/sainteye/clawdline/internal/adapters/botdevserverstore"
	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestDevServerCloudWordsReachPairedSourceAndKeepKnownHandleFences(t *testing.T) {
	j := newPAJointFixture(t)
	f := j.f
	ctx := context.Background()
	host := f.s.projectAssistants.host
	if err := host.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	bot, err := featurestore.New(f.s.store.ProjectAssistantDatabase()).Bot(ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := serverstore.NewCatalog(ctx, f.s.store.ProjectAssistantDatabase())
	if err != nil {
		t.Fatal(err)
	}
	saved := servers.SavedServer{ID: "cloud-known-server", Spec: servers.Spec{Key: f.key, Project: bot.Project, Name: "Synthetic dormant server", Command: "synthetic-never-executed", Directory: f.project}}
	if err = catalog.Create(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.SaveRule(ctx, servers.Rule{ServerID: saved.ID, Spec: saved.Spec}, servers.Deny); err != nil {
		t.Fatal(err)
	}
	if err = host.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	bridge := cloudops.Bridge{MachineID: f.key.MachineID, Router: f.cloudRouter(), AllowCommands: func() bool { return true }}
	sequence := 0
	call := func(sender, word string, fields map[string]any) (int, json.RawMessage) {
		t.Helper()
		sequence++
		body := map[string]any{}
		for k, v := range fields {
			body[k] = v
		}
		ref, ok := fields["idempotency_key"].(string)
		if !ok {
			ref = fmt.Sprintf("cloud-server-read-%d", sequence)
		}
		body["type"], body["session"], body["request"] = "project-assistants."+word, cloudops.MachineReplySession, ref
		plain, _ := json.Marshal(body)
		key, e := base64.StdEncoding.DecodeString(j.pins[sender].PublicKey)
		if e != nil {
			t.Fatal(e)
		}
		answer := bridge.Handle(ctx, cloudops.Command{Channel: "ctl/" + f.key.MachineID, Class: cloudops.ClassCtl, Sender: sender, VerifiedKey: ed25519.PublicKey(key), Sequence: uint64(sequence), Plaintext: plain})
		var envelope struct {
			Body json.RawMessage `json:"body"`
		}
		if e = json.Unmarshal(answer.Payload, &envelope); e != nil {
			t.Fatalf("missing source reply: %v %s", e, answer.Payload)
		}
		if len(envelope.Body) == 0 {
			if answer.OK() {
				t.Fatalf("successful reply has no body: %s", answer.Payload)
			}
			return answer.Status, answer.Payload // A preflight refusal has no source body.
		}
		return answer.Status, envelope.Body
	}
	fields := map[string]any{"key": paWireBotKey(f.key)}
	status, raw := call("reader", "dev-servers", fields)
	var list contract.ProjectAssistantDevServersResponse
	if status != 200 || json.Unmarshal(raw, &list) != nil || len(list.Servers) != 1 || list.Servers[0].ID != saved.ID {
		t.Fatalf("list: %d %s", status, raw)
	}
	info := list.Servers[0]
	status, raw = call("reader", "dev-server-logs", map[string]any{"key": fields["key"], "id": info.ID})
	if status != 200 {
		t.Fatalf("logs: %d %s", status, raw)
	}
	control := map[string]any{"key": fields["key"], "id": info.ID, "fingerprint": info.Fingerprint, "attempt": info.Attempt, "revision": info.Revision, "idempotency_key": "cloud-reader-stop"}
	if status, raw = call("reader", "dev-server-stop", control); status != 403 {
		t.Fatalf("read-only mutation: %s", raw)
	}
	control["idempotency_key"], control["revision"] = "cloud-stale-stop", info.Revision+1
	if status, raw = call("owner", "dev-server-stop", control); status != 409 {
		t.Fatalf("stale receipt mutation: %s", raw)
	}
	control["revision"] = info.Revision
	// A person's known-handle control cannot override an explicit source deny.
	for _, action := range []string{"start", "restart"} {
		control["idempotency_key"] = "cloud-unapproved-" + action
		if status, raw = call("owner", "dev-server-"+action, control); status == 200 {
			t.Fatalf("unapproved %s: %s", action, raw)
		}
	}
	control["idempotency_key"] = "cloud-known-stop"
	if status, raw = call("owner", "dev-server-stop", control); status != 200 {
		t.Fatalf("stop: %d %s", status, raw)
	}
	control["idempotency_key"] = "cloud-known-remove"
	if status, raw = call("owner", "dev-server-remove", control); status != 200 {
		t.Fatalf("remove: %d %s", status, raw)
	}
	if _, err = adaptercloud.NewPinnedStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName)).Revoke("owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	control["idempotency_key"] = "cloud-revoked-stop"
	if status, raw = call("owner", "dev-server-stop", control); status != 403 && status != 401 {
		t.Fatalf("revoked mutation: %s", raw)
	}
	if f.providerCalls.Load() != 0 || host.Usage().Processes != 0 {
		t.Fatal("Cloud metadata controls initialized a native provider")
	}
}

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
