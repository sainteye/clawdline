package push

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
)

const (
	cloudSubscription = "c1000000-0000-4000-8000-000000000001"
	machineCredential = "machine-credential-fixture"
)

// fakeCloud is Clawdline Cloud's `POST /v1/push/send` as the contract pins it
// (docs/push.md, "Cloud-sent push"): machine credential, the sealed bytes as
// base64url, and the answer the test scripts.
type fakeCloud struct {
	*httptest.Server
	mu      sync.Mutex
	answers []func(w http.ResponseWriter)
	calls   atomic.Int64
	bodies  []map[string]any
}

func newFakeCloud(t *testing.T, answers ...func(w http.ResponseWriter)) *fakeCloud {
	t.Helper()
	c := &fakeCloud{answers: answers}
	c.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(c.calls.Add(1))
		if r.Method != http.MethodPost || r.URL.Path != "/v1/push/send" {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+machineCredential {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
			return
		}
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		c.mu.Unlock()
		answers[min(n-1, len(answers)-1)](w)
	}))
	t.Cleanup(c.Close)
	return c
}

func (c *fakeCloud) body(t *testing.T, i int) map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if i >= len(c.bodies) {
		t.Fatalf("Cloud received %d sends, not %d", len(c.bodies), i+1)
	}
	return c.bodies[i]
}

func answer(status int, body string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// courier is the real Cloud adapter behind the push package's seam, the way
// internal/transport/http puts them together.
type courier struct{ client adaptercloud.PushClient }

func (c courier) Forward(ctx context.Context, m CloudMessage) (CloudReply, error) {
	reply, err := c.client.Send(ctx, adaptercloud.PushSendRequest{
		SubscriptionID: m.SubscriptionID, Ciphertext: m.Ciphertext, ContentType: m.ContentType,
		TTL: m.TTL, Urgency: m.Urgency, Topic: m.Topic,
	})
	return CloudReply{Status: reply.Status, Code: reply.Code, PushStatus: reply.PushStatus,
		RetryAfter: reply.RetryAfter}, err
}

func cloudSender(t *testing.T, store *Store, cloud *fakeCloud, slept *[]time.Duration) *Sender {
	t.Helper()
	sender := senderFor(t, store, slept)
	if cloud != nil {
		sender.Cloud = courier{adaptercloud.NewPushClient(cloud.URL, machineCredential)}
	}
	return sender
}

func cloudRow(b browser, id, endpoint, device string) Subscription {
	row := b.subscription(id, endpoint, device)
	row.CloudID = cloudSubscription
	return row
}

// TestACloudSubscriptionGoesThroughCloudAndNeverToItsEndpoint: sealed here to
// the browser's keys, handed to Cloud, and the endpoint never hears from this
// machine — whose own VAPID key is never minted for it.
func TestACloudSubscriptionGoesThroughCloudAndNeverToItsEndpoint(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	b := newBrowser(t)
	endpoint := newService(t, http.StatusCreated)
	cloud := newFakeCloud(t, answer(http.StatusOK, `{"status":"sent","push_status":201}`))
	if err := store.Add(cloudRow(b, "one", endpoint.URL+"/send", "phone")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	var slept []time.Duration
	delivery, err := cloudSender(t, store, cloud, &slept).Send(context.Background(), Notification{
		Title: "Clawdline", Body: "a session is waiting", URL: "/", Tag: "session-1",
	}, "")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if delivery.Sent != 1 || delivery.Failed != 0 {
		t.Errorf("delivery is %+v", delivery)
	}
	if endpoint.calls.Load() != 0 {
		t.Errorf("the endpoint was POSTed to directly %d times", endpoint.calls.Load())
	}
	if store.HasVAPIDKey() {
		t.Error("a machine with only a cloud subscription minted its own VAPID key")
	}
	body := cloud.body(t, 0)
	for key, want := range map[string]any{
		"subscription_id": cloudSubscription,
		"content_type":    "application/octet-stream",
		"ttl":             float64(TTL),
		"urgency":         Urgency,
		"topic":           Topic("session-1"),
	} {
		if body[key] != want {
			t.Errorf("%s is %v, want %v", key, body[key], want)
		}
	}
	for _, leaked := range []string{"p256dh", "auth", "endpoint", "keys"} {
		if _, ok := body[leaked]; ok {
			t.Errorf("Cloud was sent %q", leaked)
		}
	}
	sealed, err := DecodeBase64URL(body["ciphertext"].(string))
	if err != nil {
		t.Fatalf("ciphertext: %v", err)
	}
	var message map[string]any
	if err := json.Unmarshal(b.open(t, sealed), &message); err != nil {
		t.Fatalf("the browser's plaintext is not JSON: %v", err)
	}
	if message["title"] != "Clawdline" {
		t.Errorf("the browser read %v", message)
	}
}

// TestCloudsGoneIsDroppedHere: Cloud's 410 is the proof, and one try is enough.
func TestCloudsGoneIsDroppedHere(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	b := newBrowser(t)
	cloud := newFakeCloud(t, answer(http.StatusGone, `{"error":"subscription_gone"}`))
	_ = store.Add(cloudRow(b, "one", "https://fcm.example.com/send/1", "phone"))
	var slept []time.Duration
	delivery, err := cloudSender(t, store, cloud, &slept).Send(context.Background(), Notification{Title: "x"}, "")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if delivery.Failed != 1 {
		t.Errorf("delivery is %+v", delivery)
	}
	if rows, _ := store.Subscriptions(); len(rows) != 0 {
		t.Errorf("a subscription Cloud says is gone was kept: %+v", rows)
	}
	if cloud.calls.Load() != 1 {
		t.Errorf("a gone subscription was sent %d times", cloud.calls.Load())
	}
}

// TestCloudsOtherAnswersAreRetriedOrLeftAlone: a rate limit and a push service
// having a bad moment are retried; a refusal about the message and a
// subscription Cloud does not know are left exactly where they are.
func TestCloudsOtherAnswersAreRetriedOrLeftAlone(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		answers []func(w http.ResponseWriter)
		sent    bool
		calls   int64
		waited  []time.Duration
	}{
		{"429 then sent", []func(http.ResponseWriter){
			answer(http.StatusTooManyRequests, `{"error":"rate_limited","retry_after":7}`),
			answer(http.StatusOK, `{"status":"sent","push_status":201}`)}, true, 2, []time.Duration{7 * time.Second}},
		{"502 from a 503 then sent", []func(http.ResponseWriter){
			answer(http.StatusBadGateway, `{"error":"push_failed","push_status":503}`),
			answer(http.StatusOK, `{"status":"sent","push_status":201}`)}, true, 2, nil},
		{"502 from a 400", []func(http.ResponseWriter){
			answer(http.StatusBadGateway, `{"error":"push_failed","push_status":400}`)}, false, 1, nil},
		{"404 unknown", []func(http.ResponseWriter){
			answer(http.StatusNotFound, `{"error":"unknown_subscription"}`)}, false, 1, nil},
		{"429 past the ceiling", []func(http.ResponseWriter){
			answer(http.StatusTooManyRequests, `{"error":"rate_limited","retry_after":600}`)}, false, 1, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			store := openStore(t)
			b := newBrowser(t)
			cloud := newFakeCloud(t, c.answers...)
			_ = store.Add(cloudRow(b, "one", "https://fcm.example.com/send/1", "phone"))
			var slept []time.Duration
			delivery, err := cloudSender(t, store, cloud, &slept).Send(context.Background(), Notification{Title: "x"}, "")
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			if (delivery.Sent == 1) != c.sent {
				t.Errorf("delivery is %+v", delivery)
			}
			if cloud.calls.Load() != c.calls {
				t.Errorf("Cloud was asked %d times, want %d", cloud.calls.Load(), c.calls)
			}
			if c.waited != nil && (len(slept) != 1 || slept[0] != c.waited[0]) {
				t.Errorf("waited %v, want %v", slept, c.waited)
			}
			if rows, _ := store.Subscriptions(); len(rows) != 1 {
				t.Errorf("the subscription was dropped: %+v", rows)
			}
		})
	}
}

// TestALocalSubscriptionStillGoesDirect beside a cloud one: its own endpoint,
// this machine's own key, and nothing through Cloud.
func TestALocalSubscriptionStillGoesDirect(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	phone, laptop := newBrowser(t), newBrowser(t)
	direct := newService(t, http.StatusCreated)
	cloud := newFakeCloud(t, answer(http.StatusOK, `{"status":"sent","push_status":201}`))
	_ = store.Add(cloudRow(phone, "cloud", "https://fcm.example.com/send/1", "phone"))
	_ = store.Add(laptop.subscription("local", direct.URL+"/send", "laptop"))
	var slept []time.Duration
	delivery, err := cloudSender(t, store, cloud, &slept).Send(context.Background(), Notification{Title: "x"}, "")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if delivery.Sent != 2 {
		t.Errorf("delivery is %+v", delivery)
	}
	if direct.calls.Load() != 1 || cloud.calls.Load() != 1 {
		t.Errorf("direct %d, Cloud %d; want one each", direct.calls.Load(), cloud.calls.Load())
	}
	if got := (*direct.last.Load()).Get("Authorization"); !strings.HasPrefix(got, "vapid t=") {
		t.Errorf("the direct send's Authorization is %q", got)
	}
	if !store.HasVAPIDKey() {
		t.Error("the direct subscription was sent without this machine's key")
	}
}

// TestAppleStillGetsTheDeclarativeEnvelopeThroughCloud: the envelope follows
// the endpoint's host and the stored origin, whoever carries it.
func TestAppleStillGetsTheDeclarativeEnvelopeThroughCloud(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	b := newBrowser(t)
	cloud := newFakeCloud(t, answer(http.StatusOK, `{"status":"sent","push_status":201}`))
	row := cloudRow(b, "one", "https://web.push.apple.com/QW", "phone")
	row.Origin = "https://app.example.com"
	_ = store.Add(row)
	var slept []time.Duration
	if _, err := cloudSender(t, store, cloud, &slept).Send(context.Background(),
		Notification{Title: "Clawdline", Body: "waiting", URL: "/#session=a"}, ""); err != nil {
		t.Fatalf("Send: %v", err)
	}
	body := cloud.body(t, 0)
	if body["content_type"] != "application/notification+json" {
		t.Fatalf("content_type is %v", body["content_type"])
	}
	sealed, _ := DecodeBase64URL(body["ciphertext"].(string))
	var message map[string]any
	if err := json.Unmarshal(b.open(t, sealed), &message); err != nil {
		t.Fatalf("plaintext: %v", err)
	}
	notification, _ := message["notification"].(map[string]any)
	if notification["navigate"] != "https://app.example.com/#session=a" {
		t.Errorf("the declarative message is %v", message)
	}
}

// TestACloudSubscriptionWithoutCloudIsUndeliverableAndNotSentDirect: a machine
// that is not signed in does not fall back to its own key, keeps the row, and
// says in diagnostics how many it is holding.
func TestACloudSubscriptionWithoutCloudIsUndeliverableAndNotSentDirect(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	b := newBrowser(t)
	endpoint := newService(t, http.StatusCreated)
	_ = store.Add(cloudRow(b, "one", endpoint.URL+"/send", "phone"))
	var slept []time.Duration
	delivery, err := cloudSender(t, store, nil, &slept).Send(context.Background(), Notification{Title: "x"}, "")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if delivery.Sent != 0 || delivery.Failed != 1 {
		t.Errorf("delivery is %+v", delivery)
	}
	if endpoint.calls.Load() != 0 || store.HasVAPIDKey() {
		t.Errorf("fell back to this machine's own key: %d calls", endpoint.calls.Load())
	}
	if store.CloudSubscriptions() != 1 {
		t.Errorf("CloudSubscriptions is %d", store.CloudSubscriptions())
	}
}

// TestACloudIDSurvivesARestartAndIsCheckedOnTheWayIn.
func TestACloudIDSurvivesARestartAndIsCheckedOnTheWayIn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	b := newBrowser(t)
	if err := store.Add(cloudRow(b, "one", "https://fcm.example.com/send/1", "phone")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	again, _ := Open(dir)
	rows, err := again.Subscriptions()
	if err != nil || len(rows) != 1 || rows[0].CloudID != cloudSubscription {
		t.Fatalf("after a restart: %+v, %v", rows, err)
	}

	browserBody := func(cloudID any) map[string]any {
		body := map[string]any{"endpoint": "https://fcm.example.com/send/1", "keys": map[string]any{
			"p256dh": EncodeBase64URL(b.private.PublicKey().Bytes()), "auth": EncodeBase64URL(b.auth)}}
		if cloudID != nil {
			body["cloud_subscription_id"] = cloudID
		}
		return body
	}
	if row, ok := FromBrowser(browserBody(nil), "x", "phone", ""); !ok || row.Cloud() {
		t.Errorf("a plain subscription became %+v, %v", row, ok)
	}
	if row, ok := FromBrowser(browserBody(strings.ToUpper(cloudSubscription)), "x", "phone", ""); !ok || row.CloudID != cloudSubscription {
		t.Errorf("a cloud subscription became %+v, %v", row, ok)
	}
	for _, bad := range []any{"", "not-a-uuid", 7, cloudSubscription + "0"} {
		if _, ok := FromBrowser(browserBody(bad), "x", "phone", ""); ok {
			t.Errorf("cloud_subscription_id %v was believed", bad)
		}
	}
}
