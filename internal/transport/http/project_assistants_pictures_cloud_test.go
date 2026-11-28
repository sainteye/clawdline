package http

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This starts at the verified/decrypted sender seam and uses the production
// Bridge, Router, HTTP gate, normalization, attachment store and message store.
// The provider is synthetic; no model, relay or physical phone is contacted.
func TestProjectAssistantPicturesCloudUploadReadReplayAndRevocation(t *testing.T) {
	f := newPAServerFixture(t, true)
	session := &paServerSession{sent: make(chan struct{})}
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		f.providerCalls.Add(1)
		return paServerProvider{open: func(context.Context, app.OpenRequest) (app.ProviderSession, error) { return session, nil }}, nil
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	pins := map[string]adaptercloud.PinnedDevice{"owner": f.pin(t, "owner", adaptercloud.ScopeOperate, false), "reader": f.pin(t, "reader", adaptercloud.ScopeReadOnly, false)}
	bridge := cloudops.Bridge{MachineID: f.key.MachineID, Router: f.cloudRouter(), AllowCommands: func() bool { return true }}
	ask := func(sender, word string, fields map[string]any) cloudops.Answer {
		t.Helper()
		fields["type"] = "project-assistants." + word
		fields["session"] = cloudops.MachineReplySession
		fields["request"] = strings.Repeat("<", 128)
		if cmd, ok := fields["command"].(map[string]any); ok {
			fields["request"] = cmd["idempotency_key"]
		}
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		key, _ := base64.StdEncoding.DecodeString(pins[sender].PublicKey)
		return bridge.Handle(context.Background(), cloudops.Command{Channel: "ctl/" + f.key.MachineID, Class: cloudops.ClassCtl, Sender: sender, VerifiedKey: ed25519.PublicKey(key), Sequence: 1, Plaintext: raw})
	}
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	g := f.get(t, f.local)
	fence := pa.Fence{Key: f.key, Project: paDomainProjectScope(g.Bot.Project), ConversationID: g.Conversation.ID}
	cmd := f.command(pa.SendOperation, "picture-send")
	raw, _ := json.Marshal(map[string]any{"command": cmd, "fence": paWireFence(fence), "text": "", "quote_message_id": "", "images": []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())}})
	var send map[string]any
	_ = json.Unmarshal(raw, &send)
	if a := ask("owner", "send-pictures", send); !a.OK() {
		t.Fatalf("upload: %d %s", a.Status, a.Payload)
	}
	select {
	case <-session.sent:
	case <-time.After(2 * time.Second):
		t.Fatal("synthetic provider did not receive the picture-only turn")
	}
	// A lost receipt can be explicitly queried with the same command without
	// storing another user message or invoking the native provider twice.
	if a := ask("owner", "send-pictures", send); !a.OK() {
		t.Fatalf("replay: %d %s", a.Status, a.Payload)
	}
	x, err := featurestore.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Messages) != 1 || len(x.Messages[0].Attachments) != 1 {
		t.Fatalf("picture-only message not durable: %+v", x.Messages)
	}
	a := x.Messages[0].Attachments[0]
	fields := map[string]any{"machine_id": f.key.MachineID, "bot_id": f.key.BotID, "id": a.ID}
	answer := ask("reader", "attachment", fields)
	if !answer.OK() {
		t.Fatalf("paired reader: %d %s", answer.Status, answer.Payload)
	}
	var envelope struct {
		Body struct {
			ID, Data string
			Bytes    int    `json:"byte_count"`
			Media    string `json:"media_type"`
		}
	}
	if err = json.Unmarshal(answer.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(envelope.Body.Data)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := f.s.projectAssistantAttachments().Read(f.key, a)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, stored) || envelope.Body.ID != a.ID || envelope.Body.Bytes != len(stored) || envelope.Body.Media != a.MediaType {
		t.Fatal("stored picture changed in transport")
	}
	if a := ask("reader", "send-pictures", send); a.OK() {
		t.Fatal("legacy read-only grant admitted content write")
	}
	fields["machine_id"] = "foreign"
	if a := ask("owner", "attachment", fields); a.OK() {
		t.Fatal("foreign machine read admitted")
	}
	fields["machine_id"] = f.key.MachineID
	fields["bot_id"] = "other-bot"
	if a := ask("owner", "attachment", fields); a.OK() {
		t.Fatal("foreign Bot read admitted")
	}
	fields["bot_id"] = f.key.BotID
	fields["id"] = strings.Repeat("0", 64)
	if a := ask("owner", "attachment", fields); a.OK() {
		t.Fatal("missing attachment read admitted")
	}
	fields["id"] = a.ID
	if _, err = adaptercloud.NewPinnedStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName)).Revoke("owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	if a := ask("owner", "attachment", fields); a.OK() {
		t.Fatal("revoked device read admitted")
	}
	if f.providerCalls.Load() != 1 {
		t.Fatalf("provider initialized %d times", f.providerCalls.Load())
	}
}
