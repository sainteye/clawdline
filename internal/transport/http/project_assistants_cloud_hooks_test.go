package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	bh "github.com/sainteye/clawdline/internal/domain/botroutinehooks"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Local fixture Cloud: a wire-conforming fake, never production evidence.
type botHookFakeCloud struct {
	credential  string
	mu          sync.Mutex
	set         bh.Enrollment
	delivery    *bh.Delivery
	acks        []bh.Ack
	lostAck     bool
	lostEnroll  bool
	unavailable bool
	onEnroll    func()
	state       string
	claimCalls  int
	enrollCalls int
}

func (c *botHookFakeCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	if c.unavailable {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "webhook_unavailable"})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/hooks/") {
		body, _ := io.ReadAll(io.LimitReader(r.Body, bh.MaxEventBytes+1))
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) != 5 {
			w.WriteHeader(404)
			return
		}
		id := parts[4]
		var key string
		for _, h := range c.set.Hooks {
			if h.ID == id && h.Enabled {
				key = h.Key
			}
		}
		mac := hmac.New(sha256.New, []byte(key))
		_, _ = mac.Write(body)
		if key == "" || r.Header.Get("X-Hub-Signature-256") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
			w.WriteHeader(401)
			return
		}
		c.delivery = &bh.Delivery{ID: "cloud-queue-uuid", HookID: id, Revision: c.set.Revision, Headers: map[string]string{"x-delivery-id": r.Header.Get("X-Delivery-Id"), "x-hub-signature-256": r.Header.Get("X-Hub-Signature-256")}, BodyBase64: base64.StdEncoding.EncodeToString(body), LeaseToken: "lease-original"}
		c.state = "queued"
		w.WriteHeader(202)
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": bh.Protocol, "delivery_id": c.delivery.ID, "state": "queued"})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+c.credential {
		w.WriteHeader(401)
		return
	}
	switch r.URL.Path {
	case "/v1/bot-routine-hooks":
		var set bh.Enrollment
		if json.NewDecoder(r.Body).Decode(&set) != nil {
			w.WriteHeader(400)
			return
		}
		c.enrollCalls++
		old, _ := json.Marshal(c.set.Hooks)
		next, _ := json.Marshal(set.Hooks)
		if set.Revision < c.set.Revision || set.Revision == c.set.Revision && !bytes.Equal(old, next) {
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "webhook_revision_conflict"})
			return
		}
		c.set = set
		if c.onEnroll != nil {
			action := c.onEnroll
			c.onEnroll = nil
			action()
		}
		if c.lostEnroll {
			c.lostEnroll = false
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = connection.Close()
			}
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": bh.Protocol, "revision": set.Revision})
	case "/v1/bot-routine-hook-deliveries/claim":
		c.claimCalls++
		var delivery *bh.Delivery
		if c.state == "queued" {
			delivery = c.delivery
			c.state = "leased"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": bh.Protocol, "delivery": delivery})
	case "/v1/bot-routine-hook-deliveries/cloud-queue-uuid/ack":
		var ack bh.Ack
		_ = json.NewDecoder(r.Body).Decode(&ack)
		c.acks = append(c.acks, ack)
		if c.lostAck {
			c.lostAck = false
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = connection.Close()
			}
			return
		}
		if ack.Status >= 200 && ack.Status < 300 {
			c.state = "host_accepted"
		} else if ack.Code == "webhook_busy" {
			c.state = "queued"
		} else {
			c.state = "outcome_unknown"
		}
		fallthrough
	case "/v1/bot-routine-hook-deliveries/cloud-queue-uuid":
		_ = json.NewEncoder(w).Encode(bh.Metadata{Protocol: bh.Protocol, DeliveryID: "cloud-queue-uuid", State: c.state})
	default:
		w.WriteHeader(404)
	}
}
func botHookTestWorker(t *testing.T, f *paServerFixture, client bh.Cloud) *botRoutineHookWorker {
	t.Helper()
	w := &botRoutineHookWorker{server: f.s, identity: bh.Identity{AccountID: f.identity.AccountID, MachineID: f.identity.MachineID}, cloud: client, store: store.New(f.s.store.ProjectAssistantDatabase()), ctx: t.Context()}
	f.s.projectAssistants.cloudHooks.worker = w
	return w
}
func botHookIngress(t *testing.T, client *http.Client, base, machine, id, key, identity string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(body)
	r, err := http.NewRequest(http.MethodPost, base+"/v1/hooks/"+url.PathEscape(machine)+"/"+url.PathEscape(id), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	r.Header.Set("X-Delivery-Id", identity)
	response, err := client.Do(r)
	if err != nil {
		t.Fatal("ingress transport failed")
	}
	defer response.Body.Close()
	var result struct {
		DeliveryID string `json:"delivery_id"`
	}
	_ = json.NewDecoder(response.Body).Decode(&result)
	if response.StatusCode != 202 || result.DeliveryID == "" {
		t.Fatalf("ingress did not queue: HTTP %d", response.StatusCode)
	}
	return result.DeliveryID
}
func botHookAwaitRun(t *testing.T, f *paServerFixture, id, original string) pa.RoutineRun {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for {
		_, runs, err := store.New(f.s.store.ProjectAssistantDatabase()).Routines(context.Background(), f.key)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 1 && runs[0].State == pa.TurnSucceeded {
			if runs[0].RoutineID != id || runs[0].DeliveryID != original {
				t.Fatal("source receipt lost original identity")
			}
			return runs[0]
		}
		if time.Now().After(until) {
			states := []pa.TurnState{}
			for _, run := range runs {
				states = append(states, run.State)
			}
			t.Fatal("scripted provider did not complete actual source admission", states)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBotRoutineHooksHTTPHostSQLiteExactBytesLostAckAndRedelivery(t *testing.T) {
	f, p, source := newRoutineHumanFixture(t)
	paServerOK(t, f.send(t, f.local, "cloud-hook-main-before"))
	routineHumanIdle(t, f)
	d, page, key := saveHookRoutine(t, f, source, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
	fake := &botHookFakeCloud{credential: f.identity.MachineCredential, lostAck: true}
	cloud := httptest.NewTLSServer(fake)
	defer cloud.Close()
	client := adaptercloud.NewBotRoutineHookClient(cloud.URL, f.identity.MachineCredential)
	client.HTTP = cloud.Client()
	w := botHookTestWorker(t, f, client)
	if got := f.s.botRoutineHookPublicURL(t.Context(), d.ID, key); got != "" {
		t.Fatal("configured address claimed enrollment")
	}
	if err := w.cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := f.s.botRoutineHookPublicURL(t.Context(), d.ID, key); got != cloud.URL+"/v1/hooks/"+f.identity.MachineID+"/"+d.ID {
		t.Fatal("acknowledged address missing")
	}
	// The authenticated human route publishes the optional address through its
	// existing response, without lending its bearer to the background receiver.
	q := contract.ProjectAssistantRoutineHookRequest{Command: f.command(pa.EditOperation, "cloud-public-address"), Fence: page.Fence, ID: d.ID, ExpectedRevision: d.Version}
	status, raw := routineHumanTCP(t, f, source, "POST", "/v1/project-assistants/"+f.key.BotID+"/routines/"+d.ID+"/hook", f.local, q)
	var hook contract.ProjectAssistantRoutineHookResponse
	_ = json.Unmarshal(raw, &hook)
	if status != 200 || hook.PublicURL == "" || hook.Key != key {
		t.Fatal("human hook response missing current public enrollment")
	}
	body := []byte(" {\"text\":\"synthetic event\",\"number\":9007199254740993} \n")
	queue := botHookIngress(t, cloud.Client(), cloud.URL, f.identity.MachineID, d.ID, key, "original-sender-id", body)
	if err := w.cycle(t.Context()); err == nil {
		t.Fatal("lost ack was treated as success")
	}
	retained, e := w.store.CloudHookPending(t.Context(), w.identity)
	if e != nil || retained == nil || retained.Ack == nil {
		t.Fatal("no local response retained", e)
	}
	if retained.Ack.Status != 200 || retained.Ack.RunID == "" {
		t.Fatalf("source admission HTTP %d code %s", retained.Ack.Status, retained.Ack.Code)
	}
	first := botHookAwaitRun(t, f, d.ID, "original-sender-id")
	if first.ID == queue {
		t.Fatal("Cloud queue id replaced source admission id")
	}
	pending, err := w.store.CloudHookPending(t.Context(), w.identity)
	if err != nil || pending == nil || pending.Ack == nil || pending.Ack.RunID != first.ID {
		t.Fatal("lost ack not durably retained", err)
	}
	if err = w.cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	if len(fake.acks) != 2 || fake.acks[0] != fake.acks[1] || fake.acks[0].Status != 200 {
		t.Fatal("ack replay changed actual HTTP result")
	}
	// Simulate lease loss with the same original body/proof/identity. Host must
	// return its durable original receipt instead of starting another prompt.
	fake.state = "queued"
	fake.delivery.LeaseToken = "lease-replaced"
	fake.mu.Unlock()
	if err = w.cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	second := botHookAwaitRun(t, f, d.ID, "original-sender-id")
	if second.ID != first.ID {
		t.Fatal("redelivery created another run")
	}
	p.mu.Lock()
	sends := len(p.sends)
	p.mu.Unlock()
	if sends != 2 {
		t.Fatal("redelivery started another scripted prompt", sends)
	}
	archive, err := w.store.ExportArchive(t.Context(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	export, _ := json.Marshal(archive)
	if bytes.Contains(export, []byte(key)) || bytes.Contains(export, []byte("lease-replaced")) {
		t.Fatal("private bridge authority in export")
	}
	paServerOK(t, f.send(t, f.local, "cloud-hook-main-after"))
	routineHumanIdle(t, f)
}

func TestBotRoutineHooksWithdrawalAndLiveScope(t *testing.T) {
	for _, change := range []string{"disable", "pause", "rotation", "project", "account", "deletion"} {
		t.Run(change, func(t *testing.T) {
			f, _, source := newRoutineHumanFixture(t)
			d, page, key := saveHookRoutine(t, f, source, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
			fake := &botHookFakeCloud{credential: f.identity.MachineCredential}
			cloud := httptest.NewTLSServer(fake)
			defer cloud.Close()
			client := adaptercloud.NewBotRoutineHookClient(cloud.URL, f.identity.MachineCredential)
			client.HTTP = cloud.Client()
			w := botHookTestWorker(t, f, client)
			if err := w.cycle(t.Context()); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "disable":
				paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
			case "rotation":
				q := contract.ProjectAssistantRoutineHookRequest{Command: f.command(pa.EditOperation, "rotate-enrolled"), Fence: page.Fence, ID: d.ID, ExpectedRevision: d.Version, Rotate: true}
				status, _ := routineHumanTCP(t, f, source, "POST", "/v1/project-assistants/"+f.key.BotID+"/routines/"+d.ID+"/hook", f.local, q)
				if status != 200 {
					t.Fatal("rotation failed")
				}
			case "pause":
				q := contract.ProjectAssistantRoutineEnabledRequest{Command: f.command(pa.EditOperation, "pause-enrolled"), Fence: page.Fence, ID: d.ID, ExpectedRevision: d.Version, Enabled: false}
				status, _ := routineHumanTCP(t, f, source, "POST", "/v1/project-assistants/"+f.key.BotID+"/routines/"+d.ID+"/enabled", f.local, q)
				if status != 200 {
					t.Fatal("pause failed")
				}
			case "project":
				if err := os.Rename(f.project, f.project+"-moved"); err != nil {
					t.Fatal(err)
				}
			case "account":
				i := f.identity
				i.AccountID = "foreign-account"
				if err := adaptercloud.NewIdentityStore(f.s.cfg.Dir + "/" + cloudkeys.DirName).Save(i); err != nil {
					t.Fatal(err)
				}
			case "deletion":
				if err := w.store.DeleteBot(t.Context(), f.key); err != nil {
					t.Fatal(err)
				}
			}
			if f.s.botRoutineHookPublicURL(t.Context(), d.ID, key) != "" {
				t.Fatal("stale/foreign scope retained public readiness")
			}
			err := w.cycle(t.Context())
			if change == "account" {
				if err == nil {
					t.Fatal("foreign namespace used")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			fake.mu.Lock()
			set := fake.set
			fake.mu.Unlock()
			if set.Revision != 2 {
				t.Fatal("changed authority did not advance enrollment", set.Revision)
			}
			if change != "rotation" && change != "pause" && len(set.Hooks) != 0 {
				t.Fatal("authority not withdrawn")
			}
			if change == "pause" && (len(set.Hooks) != 1 || set.Hooks[0].Enabled) {
				t.Fatal("pause was not published")
			}
		})
	}
}

func TestBotRoutineHooksUnknownBarrierAndSourceFailureIsolation(t *testing.T) {
	f, _, source := newRoutineHumanFixture(t)
	d, _, _ := saveHookRoutine(t, f, source, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
	fake := &botHookFakeCloud{credential: f.identity.MachineCredential, state: "leased"}
	cloud := httptest.NewTLSServer(fake)
	defer cloud.Close()
	client := adaptercloud.NewBotRoutineHookClient(cloud.URL, f.identity.MachineCredential)
	client.HTTP = cloud.Client()
	w := botHookTestWorker(t, f, client)
	if err := w.store.BeginCloudHook(t.Context(), w.identity, "cloud-queue-uuid"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := w.cycle(t.Context()); bh.Code(err) != "webhook_outcome_unknown" {
			t.Fatal("ambiguous local outcome was replayed", err)
		}
	}
	fake.mu.Lock()
	claims := fake.claimCalls
	acks := len(fake.acks)
	fake.mu.Unlock()
	if claims != 0 || acks != 0 {
		t.Fatal("unknown barrier forwarded or manufactured ack")
	}
	if f.s.botRoutineHookPublicURL(t.Context(), d.ID, "wrong-key") != "" {
		t.Fatal("unknown key exposed public address")
	}
	paServerOK(t, f.send(t, f.local, "ordinary-chat-during-hook-failure"))
	routineHumanIdle(t, f)
}

func TestBotRoutineHooksEnrollmentAmbiguityAndCurrentSnapshot(t *testing.T) {
	for _, mode := range []string{"lost-response", "changed-during-response", "cloud-ahead"} {
		t.Run(mode, func(t *testing.T) {
			f, _, source := newRoutineHumanFixture(t)
			d, _, key := saveHookRoutine(t, f, source, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
			fake := &botHookFakeCloud{credential: f.identity.MachineCredential}
			switch mode {
			case "lost-response":
				fake.lostEnroll = true
			case "changed-during-response":
				fake.onEnroll = func() {
					if _, err := f.s.store.ProjectAssistantDatabase().Exec(`UPDATE project_assistant_routine_hooks SET secret=? WHERE id=?`, strings.Repeat("b", 64), d.ID); err != nil {
						t.Error(err)
					}
				}
			case "cloud-ahead":
				fake.set = bh.Enrollment{Protocol: bh.Protocol, Revision: 100, Hooks: []bh.Hook{}}
			}
			cloud := httptest.NewTLSServer(fake)
			defer cloud.Close()
			client := adaptercloud.NewBotRoutineHookClient(cloud.URL, f.identity.MachineCredential)
			client.HTTP = cloud.Client()
			w := botHookTestWorker(t, f, client)
			if err := w.cycle(t.Context()); err == nil {
				t.Fatal("ambiguous/stale enrollment was ready")
			}
			if got := f.s.botRoutineHookPublicURL(t.Context(), d.ID, key); got != "" {
				t.Fatal("unacknowledged key got public address")
			}
			err := w.cycle(t.Context())
			if mode == "cloud-ahead" {
				if bh.Code(err) != "webhook_revision_conflict" {
					t.Fatal("Cloud higher revision overwritten", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			fake.mu.Lock()
			revision := fake.set.Revision
			calls := fake.enrollCalls
			fake.mu.Unlock()
			expected := int64(1)
			if mode == "changed-during-response" {
				expected = 2
			}
			if revision != expected || calls != 2 {
				t.Fatal("canonical revision/transport retries changed", revision, calls)
			}
		})
	}
}

func TestBotRoutineHooksStorageFailuresNeverAuthorizeAnotherForward(t *testing.T) {
	for _, stage := range []string{"before-forward", "after-forward"} {
		t.Run(stage, func(t *testing.T) {
			f, _, source := newRoutineHumanFixture(t)
			d, _, key := saveHookRoutine(t, f, source, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
			fake := &botHookFakeCloud{credential: f.identity.MachineCredential}
			cloud := httptest.NewTLSServer(fake)
			defer cloud.Close()
			client := adaptercloud.NewBotRoutineHookClient(cloud.URL, f.identity.MachineCredential)
			client.HTTP = cloud.Client()
			w := botHookTestWorker(t, f, client)
			if err := w.cycle(t.Context()); err != nil {
				t.Fatal(err)
			}
			botHookIngress(t, cloud.Client(), cloud.URL, f.identity.MachineID, d.ID, key, "storage-original", []byte(`{}`))
			event := "INSERT"
			if stage == "after-forward" {
				event = "UPDATE"
			}
			if _, err := f.s.store.ProjectAssistantDatabase().Exec(`CREATE TRIGGER fail_cloud_hook BEFORE ` + event + ` ON project_assistant_cloud_hook_pending BEGIN SELECT RAISE(FAIL,'synthetic storage failure'); END`); err != nil {
				t.Fatal(err)
			}
			if err := w.cycle(t.Context()); err == nil {
				t.Fatal("storage failure accepted")
			}
			_, runs, err := w.store.Routines(t.Context(), f.key)
			if err != nil {
				t.Fatal(err)
			}
			expected := 0
			if stage == "after-forward" {
				expected = 1
			}
			if len(runs) != expected {
				t.Fatal("unexpected admission count", len(runs))
			}
			if _, err = f.s.store.ProjectAssistantDatabase().Exec(`DROP TRIGGER fail_cloud_hook`); err != nil {
				t.Fatal(err)
			}
			if stage == "after-forward" {
				if err = w.cycle(t.Context()); bh.Code(err) != "webhook_outcome_unknown" {
					t.Fatal("unknown result forwarded again", err)
				}
			}
			fake.mu.Lock()
			ackCount := len(fake.acks)
			fake.mu.Unlock()
			if ackCount != 0 {
				t.Fatal("storage failure manufactured a Host ack")
			}
		})
	}
}

func TestBotRoutineHooksWorkerCancellationAndDefaultOff(t *testing.T) {
	f := newPAServerFixture(t, true)
	var calls atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
	defer cloud.Close()
	ctx, cancel := context.WithCancel(t.Context())
	w := botHookTestWorker(t, f, adaptercloud.NewBotRoutineHookClient(cloud.URL, f.identity.MachineCredential))
	if err := w.cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	done := f.s.StartBotRoutineHooks(ctx, bh.Identity{AccountID: f.identity.AccountID, MachineID: f.identity.MachineID}, adaptercloud.NewBotRoutineHookClient(cloud.URL, f.identity.MachineCredential))
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker failed to stop")
	}
	if calls.Load() != 0 {
		t.Fatal("default-off source sent empty registrations")
	}
	if got := f.s.botRoutineHookPublicURL(t.Context(), "routine", "key"); got != "" {
		t.Fatal("canceled worker still ready")
	}
}

func TestBotRoutineHooksOfflineKeepsLocalHumanKeyControls(t *testing.T) {
	f, _, source := newRoutineHumanFixture(t)
	d, page, key := saveHookRoutine(t, f, source, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
	fake := &botHookFakeCloud{credential: f.identity.MachineCredential}
	cloud := httptest.NewTLSServer(fake)
	defer cloud.Close()
	client := adaptercloud.NewBotRoutineHookClient(cloud.URL, f.identity.MachineCredential)
	client.HTTP = cloud.Client()
	w := botHookTestWorker(t, f, client)
	if err := w.cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.s.botRoutineHookPublicURL(t.Context(), d.ID, key) == "" {
		t.Fatal("initial address not ready")
	}
	fake.mu.Lock()
	fake.unavailable = true
	fake.mu.Unlock()
	if err := w.cycle(t.Context()); bh.Code(err) != "webhook_unavailable" {
		t.Fatal("offline Cloud not typed unavailable", err)
	}
	q := contract.ProjectAssistantRoutineHookRequest{Command: f.command(pa.EditOperation, "offline-local-controls"), Fence: page.Fence, ID: d.ID, ExpectedRevision: d.Version}
	status, raw := routineHumanTCP(t, f, source, "POST", "/v1/project-assistants/"+f.key.BotID+"/routines/"+d.ID+"/hook", f.local, q)
	var answer contract.ProjectAssistantRoutineHookResponse
	if status != 200 || json.Unmarshal(raw, &answer) != nil || answer.Key != key || answer.URL != "/hooks/routines/"+d.ID || answer.PublicURL != "" {
		t.Fatal("Cloud failure broke local controls or retained ready address")
	}
}

func TestBotRoutineHooksActiveGenerationStopsBeforeReplacement(t *testing.T) {
	f, _, source := newRoutineHumanFixture(t)
	saveHookRoutine(t, f, source, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
	claimed := make(chan struct{}, 2)
	release := make(chan struct{})
	var active, peak atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Method == http.MethodPut {
			_ = json.NewEncoder(w).Encode(map[string]any{"protocol": bh.Protocol, "revision": 1})
			return
		}
		n := active.Add(1)
		for previous := peak.Load(); n > previous && !peak.CompareAndSwap(previous, n); previous = peak.Load() {
		}
		defer active.Add(-1)
		claimed <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer cloud.Close()
	defer close(release)
	id := bh.Identity{AccountID: f.identity.AccountID, MachineID: f.identity.MachineID}
	for range 2 {
		ctx, cancel := context.WithCancel(t.Context())
		done := f.s.StartBotRoutineHooks(ctx, id, adaptercloud.NewBotRoutineHookClient(cloud.URL, f.identity.MachineCredential))
		select {
		case <-claimed:
		case <-time.After(2 * time.Second):
			cancel()
			t.Fatal("claim did not start")
		}
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("active worker did not stop")
		}
		// Remote socket teardown can lag local cancellation; await observation
		// before measuring the next fixture's independent TCP handler lifetime.
		until := time.Now().Add(time.Second)
		for active.Load() != 0 && time.Now().Before(until) {
			time.Sleep(time.Millisecond)
		}
		if active.Load() != 0 {
			t.Fatal("canceled claim remained active")
		}
	}
	if peak.Load() != 1 {
		t.Fatal("two generations polled concurrently", peak.Load())
	}
}

// Root may connect this opt-in fixture to its independently provisioned local
// Fastify/Mongo control plane. No private checkout/path or provisioning secret
// belongs in public source. Normal tests always exercise the local fake above.
func TestBotRoutineHooksRealControlPlaneOptIn(t *testing.T) {
	base := os.Getenv("CLAWDLINE_TEST_BOT_HOOKS_API")
	if base == "" {
		t.Skip("opt-in: provide an already provisioned local control-plane fixture")
	}
	u, err := bh.TrustedBase(base)
	if err != nil || u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" {
		t.Fatal("opt-in fixture requires a local control plane")
	}
	f := newPAServerFixture(t, true)
	for name, target := range map[string]*string{"CLAWDLINE_TEST_BOT_HOOKS_ACCOUNT": &f.identity.AccountID, "CLAWDLINE_TEST_BOT_HOOKS_MACHINE": &f.identity.MachineID, "CLAWDLINE_TEST_BOT_HOOKS_CREDENTIAL": &f.identity.MachineCredential} {
		value := os.Getenv(name)
		if value == "" {
			t.Fatalf("opt-in fixture requires %s", name)
		}
		*target = value
	}
	f.identity.APIBase = base
	f.key.MachineID = f.identity.MachineID
	if err = adaptercloud.NewIdentityStore(f.s.cfg.Dir + "/" + cloudkeys.DirName).Save(f.identity); err != nil {
		t.Fatal("cannot save fixture identity")
	}
	p := &routineHumanProvider{}
	f.s.projectAssistants.provider = func() (app.Provider, error) { return p, nil }
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	paServerOK(t, f.send(t, f.local, "opt-in-main-before"))
	routineHumanIdle(t, f)
	source := httptest.NewServer(f.h)
	defer source.Close()
	d, _, key := saveHookRoutine(t, f, source, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
	w := botHookTestWorker(t, f, adaptercloud.NewBotRoutineHookClient(base, f.identity.MachineCredential))
	if err = w.cycle(t.Context()); err != nil {
		t.Fatal("real control-plane enrollment failed:", bh.Code(err))
	}
	queue := botHookIngress(t, &http.Client{Timeout: 5 * time.Second}, base, f.identity.MachineID, d.ID, key, "source-original-opt-in", []byte(" {\"text\":\"synthetic real-control-plane fixture\"} \n"))
	if err = w.cycle(t.Context()); err != nil {
		t.Fatal("real control-plane forwarding failed:", bh.Code(err))
	}
	run := botHookAwaitRun(t, f, d.ID, "source-original-opt-in")
	metadata, err := w.cloud.Inspect(t.Context(), queue)
	if err != nil || metadata.State != "host_accepted" || metadata.RunID != run.ID {
		t.Fatal("real control-plane metadata did not prove source admission")
	}
	t.Log("REAL_BOT_HOOKS_HOST_SQLITE_OK enrolled/queued/host-admitted/scripted-provider-completed; native=0 model=0")
}
