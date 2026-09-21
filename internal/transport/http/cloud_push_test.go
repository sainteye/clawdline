package http

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	adapterpush "github.com/sainteye/clawdline/internal/adapters/push"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

// The four words a phone needs before it can be notified at all, end to end
// and in one process: the plaintext the console seals, decoded by the bridge,
// carried by the in-process router with this daemon's own credentials stamped
// on it — and therefore through the gate, the capability checks and the exact
// routes a paired browser on this machine's own network reaches.
//
// Nothing here is a fake but the transport and the push service's address. The
// store is real, the key is really minted, and the subscriptions are really
// written and read back.

// pushStandIn is this daemon's own handler over a fresh state directory, the
// bridge a Cloud viewer is answered through, and the two credentials these
// tests are about: this machine's own token, which is what an in-process Cloud
// request arrives holding, and one read-only paired device, which is what a
// phone on this machine's own network is.
type pushStandIn struct {
	server  *Server
	handler http.Handler
	bridge  cloudops.Bridge
	local   string
	phone   string
}

func newPushStandIn(t *testing.T) *pushStandIn {
	t.Helper()
	return newPushStandInFor(t, adaptercloud.DefaultAppOrigin)
}

// newPushStandInFor is the same machine answering for a named console, which
// is what `cloud_app_origin` in the settings file decides: the default is
// `app.clawdline.com`, and a person who put their own console there is
// followed to it. The link reads that key once and hands it to the router
// (`internal/transport/cloud`'s `wire`), so this argument is that settings
// value arriving where the routes can see it.
func newPushStandInFor(t *testing.T, appOrigin string) *pushStandIn {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(t.TempDir(), "swift"))
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := &Server{cfg: config.Config{Dir: dir, Port: 7757}, store: st, icons: &icon.Registry{}}
	handler := s.Handler()
	local, machineToken, err := s.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	_, phone, err := s.gate().auth.AddDevice("phone", auth.NewCaps(auth.Read), false)
	if err != nil {
		t.Fatal(err)
	}
	return &pushStandIn{
		server:  s,
		handler: handler,
		bridge: cloudops.Bridge{MachineID: "mac-01",
			Router: cloud.Router{Handler: handler,
				Authorize: cloud.LocalAuthorizer(local, machineToken),
				AppOrigin: appOrigin},
			// Deliberately off: these three words are read-level, and a Mac
			// with remote writes switched off still lets a phone ask to be
			// notified.
			AllowCommands: func() bool { return false }},
		local: local,
		phone: phone,
	}
}

// ask seals nothing and decrypts nothing: it hands the bridge the plaintext a
// viewer's envelope would have carried.
func (p *pushStandIn) ask(t *testing.T, seq uint64, body map[string]any) (cloudops.Answer, map[string]any) {
	t.Helper()
	plaintext, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	answer := p.bridge.Handle(context.Background(), cloudops.Command{
		Channel: "ctl/mac-01", Class: cloudops.ClassCtl, Sender: "web_viewer-01",
		Sequence: seq, Plaintext: plaintext})
	var payload map[string]any
	if len(answer.Payload) > 0 {
		if err := json.Unmarshal(answer.Payload, &payload); err != nil {
			t.Fatalf("payload %s: %v", answer.Payload, err)
		}
	}
	return answer, payload
}

// call is the same request over this machine's own network, which is the road
// the actor header is about.
func (p *pushStandIn) call(t *testing.T, token, path, body string, actor bool) *httptest.ResponseRecorder {
	t.Helper()
	headers := map[string]string{"Authorization": "Bearer " + token,
		"Content-Type": "application/json"}
	if actor {
		headers[actorHeader] = actorDevice
	}
	return call{path: path, body: body, headers: headers}.do(p.handler)
}

func (p *pushStandIn) rows(t *testing.T) []adapterpush.Subscription {
	t.Helper()
	held, err := p.server.push()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := held.Subscriptions()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// cloudSubscription is what a browser's `PushSubscription.toJSON()` hands the
// page, with a real P-256 point and a real 16-octet secret: `FromBrowser`
// checks both before anything is stored, so a made-up pair would be refused
// for the wrong reason.
func cloudSubscription(t *testing.T, endpoint string) map[string]any {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	secret := make([]byte, adapterpush.AuthSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("auth: %v", err)
	}
	return map[string]any{
		"endpoint": endpoint,
		"keys": map[string]any{
			"p256dh": adapterpush.EncodeBase64URL(key.PublicKey().Bytes()),
			"auth":   adapterpush.EncodeBase64URL(secret),
		},
	}
}

// TestACloudViewerRegistersForNotifications walks the registration the phone
// could not finish: the key, the subscription, and taking it back again.
//
// The test button is not pressed here. `push-test` reaches a push service over
// the network, which a test must not, and what this exercise is about is the
// three requests in front of it — the ones that never left the browser.
func TestACloudViewerRegistersForNotifications(t *testing.T) {
	s := newPushStandIn(t)

	answer, payload := s.ask(t, 1, map[string]any{"type": "push-key",
		"session": cloudops.MachineReplySession, "request": "req-key"})
	if !answer.OK() {
		t.Fatalf("the viewer could not ask for the key: %d/%q %s",
			answer.Status, answer.Code, answer.Payload)
	}
	key, _ := bodyOf(t, payload)["key"].(string)
	raw, err := adapterpush.DecodeBase64URL(key)
	if err != nil || len(raw) != adapterpush.SubscriberKeyBytes || raw[0] != 0x04 {
		t.Fatalf("the key that crossed is not an uncompressed point: %q (%d octets, %v)",
			key, len(raw), err)
	}

	subscription := cloudSubscription(t, "https://web.push.apple.com/QWxpY2U")
	answer, payload = s.ask(t, 2, map[string]any{"type": "push-subscribe",
		"session": cloudops.MachineReplySession, "request": "req-sub",
		"subscription": subscription})
	if !answer.OK() {
		t.Fatalf("the viewer could not subscribe: %d/%q %s",
			answer.Status, answer.Code, answer.Payload)
	}
	body := bodyOf(t, payload)
	id, _ := body["id"].(string)
	if body["ok"] != true || id == "" {
		t.Fatalf("the answer names no subscription: %v", body)
	}
	if rows := s.rows(t); len(rows) != 1 {
		t.Fatalf("one registration wrote %d subscriptions", len(rows))
	}

	// A retry is not a second phone being notified. **The mechanism is not a
	// receipt**: `/v1/push/subscribe` keeps none, so the Idempotency-Key this
	// bridge carries reaches a route that does not read it, and the retry runs
	// again and answers a new row id. What holds is the store — `Store.Add`
	// replaces any row for the same endpoint — so what the phone would receive
	// is right and only the id the page holds is stale.
	again, payloadAgain := s.ask(t, 3, map[string]any{"type": "push-subscribe",
		"session": cloudops.MachineReplySession, "request": "req-sub",
		"subscription": subscription})
	if !again.OK() {
		t.Fatalf("the retry was refused: %d/%q", again.Status, again.Code)
	}
	if rows := s.rows(t); len(rows) != 1 {
		t.Fatalf("a retried registration left %d subscriptions", len(rows))
	}
	id, _ = bodyOf(t, payloadAgain)["id"].(string)

	// And taking it back, which is what the page does when somebody turns
	// notifications off.
	answer, payload = s.ask(t, 4, map[string]any{"type": "push-unsubscribe",
		"session": cloudops.MachineReplySession, "request": "req-drop", "id": id})
	if !answer.OK() {
		t.Fatalf("the viewer could not unsubscribe: %d/%q %s",
			answer.Status, answer.Code, answer.Payload)
	}
	if bodyOf(t, payload)["ok"] != true {
		t.Fatalf("the removal did not answer ok: %v", payload)
	}
	if rows := s.rows(t); len(rows) != 0 {
		t.Fatalf("the subscription survived being taken back: %d rows", len(rows))
	}

	// A subscription the route will not have is its own refusal, carried with
	// its own code — not silence, and not a shrug.
	answer, payload = s.ask(t, 5, map[string]any{"type": "push-subscribe",
		"session": cloudops.MachineReplySession, "request": "req-bad",
		"subscription": map[string]any{"endpoint": "http://not-https.example/x"}})
	if answer.Status != http.StatusBadRequest || answer.Code != "bad_request" {
		t.Fatalf("answered %d/%q, wanted 400/bad_request", answer.Status, answer.Code)
	}
	if failure, _ := payload["error"].(map[string]any); failure["layer"] != "mac_route" {
		t.Fatalf("the refusal was not the route's: %v", payload)
	}
}

// TestEveryCloudViewerIsTheSameDeviceHere is the divergence these words are
// advertised with, measured rather than reasoned about.
//
// A subscription belongs to the device that made it, and over Cloud that
// device is not the phone: the in-process request carries this machine's own
// local credential, so every viewer on the account is the same device to the
// push store. `Store.Add` keeps one row per device, so a second Cloud browser
// asking to be notified silently takes the first one's place — where two
// phones paired to this machine's own network keep a row each.
//
// This is not what the push feature should do, and it is not fixed by carrying
// the four words: it needs the viewer's own identity to reach the route, which
// no word on this wire carries. It is held here so that the next reader finds
// it stated rather than discovering it on a phone that stopped buzzing.
func TestEveryCloudViewerIsTheSameDeviceHere(t *testing.T) {
	s := newPushStandIn(t)

	for i, endpoint := range []string{
		"https://web.push.apple.com/QWxpY2U",
		"https://fcm.googleapis.com/fcm/send/Qm9i",
	} {
		answer, _ := s.ask(t, uint64(i+1), map[string]any{"type": "push-subscribe",
			"session": cloudops.MachineReplySession, "request": "req-sub-" + string(rune('a'+i)),
			"subscription": cloudSubscription(t, endpoint)})
		if !answer.OK() {
			t.Fatalf("%s was refused: %d/%q", endpoint, answer.Status, answer.Code)
		}
	}
	rows := s.rows(t)
	if len(rows) != 1 {
		t.Fatalf("two Cloud viewers left %d subscriptions; this test is out of date, and the "+
			"divergence on `push-subscribe` in internal/app/cloudops/ops.go says one", len(rows))
	}
	if rows[0].Endpoint != "https://fcm.googleapis.com/fcm/send/Qm9i" {
		t.Fatalf("the surviving row is %q, wanted the one that arrived last", rows[0].Endpoint)
	}
	// The *origin* stored with it is no longer part of this divergence. It was:
	// the row kept the `http://127.0.0.1` the authorizer stamps for CSRF, and
	// an iOS notification resolved its address against that. The two halves
	// looked like one problem and were not — one viewer's identity cannot
	// reach this route, and the console's address always could.
	if rows[0].Origin != adaptercloud.DefaultAppOrigin {
		t.Fatalf("the stored origin is %q, want the hosted console's %q",
			rows[0].Origin, adaptercloud.DefaultAppOrigin)
	}
}

// TestACloudViewerDoesNotInheritThisMachinesOwnExemption is the one
// authorisation rule these words add, and the reason the three push writes
// carry `X-Clawdline-Actor: device`.
//
// `/v1/push/unsubscribe` removes a row only when it belongs to the device
// asking — otherwise one paired browser could quietly stop another one's
// notifications by naming its id. This machine's own token is exempt, because
// that exemption is how a script cleans up after itself.
//
// A Cloud viewer reaches these routes in process holding exactly that token
// (`internal/transport/cloud.LocalAuthorizer`), so without a word from the
// request itself a person on their phone would arrive wearing the script's
// exemption. The header takes it away and grants nothing: it cannot name a
// device, a capability or a sender.
func TestACloudViewerDoesNotInheritThisMachinesOwnExemption(t *testing.T) {
	s := newPushStandIn(t)

	// One subscription, made by the paired phone over this machine's own
	// network. It belongs to that device and to nobody else.
	subscription, err := json.Marshal(cloudSubscription(t, "https://web.push.apple.com/QWxpY2U"))
	if err != nil {
		t.Fatal(err)
	}
	rec := s.call(t, s.phone, "/v1/push/subscribe", string(subscription), false)
	if rec.Code != http.StatusOK {
		t.Fatalf("the phone could not subscribe: %d %s", rec.Code, rec.Body)
	}
	var subscribed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &subscribed); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if subscribed.ID == "" {
		t.Fatalf("the answer names no subscription: %s", rec.Body)
	}
	drop := `{"id":"` + subscribed.ID + `"}`

	// This machine's own token, saying it is a device: judged as one, and a
	// row that is not its own is answered as though it were not there.
	if rec := s.call(t, s.local, "/v1/push/unsubscribe", drop, true); rec.Code != http.StatusOK {
		t.Fatalf("unsubscribe as a device: %d %s", rec.Code, rec.Body)
	}
	if left := s.rows(t); len(left) != 1 {
		t.Fatalf("a Cloud viewer removed a subscription that is not its own: %d rows left", len(left))
	}

	// The same token without that word is this machine, and the script's
	// exemption is still there.
	if rec := s.call(t, s.local, "/v1/push/unsubscribe", drop, false); rec.Code != http.StatusOK {
		t.Fatalf("unsubscribe as this machine: %d %s", rec.Code, rec.Body)
	}
	if left := s.rows(t); len(left) != 0 {
		t.Fatalf("this machine's own token could not clean up after itself: %d rows left", len(left))
	}
}

// TestTheActorHeaderClosesTheLocalDoorToo is the same property where the gate
// decides it, for every route that asks whether the caller is this machine
// rather than a person. `/v1/settings` is that question in its narrowest form:
// the hotkey is a global keyboard grab, so only this machine's own token
// changes it.
func TestTheActorHeaderClosesTheLocalDoorToo(t *testing.T) {
	f, h := newGateFixture(t)
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{{
		name: "this machine's own token changes this machine's settings",
		headers: map[string]string{"Authorization": "Bearer " + f.local,
			"Content-Type": "application/json"},
		want: http.StatusOK,
	}, {
		name: "and changes nothing once it says it is a device",
		headers: map[string]string{"Authorization": "Bearer " + f.local,
			actorHeader: actorDevice, "Content-Type": "application/json"},
		want: http.StatusForbidden,
	}, {
		name: "a paired device that may send was never this machine",
		headers: map[string]string{"Authorization": "Bearer " + f.send,
			"Content-Type": "application/json"},
		want: http.StatusForbidden,
	}} {
		rec := call{path: "/v1/settings", body: `{}`, headers: tc.headers}.do(h)
		if rec.Code != tc.want {
			t.Errorf("%s: %d %s, want %d", tc.name, rec.Code, rec.Body, tc.want)
		}
	}
}

// TestACloudSubscriptionOpensTheHostedConsole is the whole of what the origin
// stored with a Cloud registration is for: a notification that arrives on a
// phone has to open the page the person was looking at.
//
// It is measured at the two places the value is used rather than only at the
// column it is written into — the store's row, and the envelope
// `adapterpush.Build` selects from it. An iPhone's endpoint is
// `web.push.apple.com`, so a row with a trustworthy origin takes Apple's
// Declarative Web Push envelope, and `navigate` is resolved against that
// origin. WebKit displays and navigates without waking the service worker, so
// `sw.js`'s `notificationclick` is not a second chance at this: the address in
// the envelope is the address the tap opens.
func TestACloudSubscriptionOpensTheHostedConsole(t *testing.T) {
	s := newPushStandIn(t)

	answer, _ := s.ask(t, 1, map[string]any{"type": "push-subscribe",
		"session": cloudops.MachineReplySession, "request": "req-sub",
		"subscription": cloudSubscription(t, "https://web.push.apple.com/QWxpY2U")})
	if !answer.OK() {
		t.Fatalf("the viewer could not subscribe: %d/%q %s",
			answer.Status, answer.Code, answer.Payload)
	}
	rows := s.rows(t)
	if len(rows) != 1 {
		t.Fatalf("one registration wrote %d subscriptions", len(rows))
	}
	if rows[0].Origin != adaptercloud.DefaultAppOrigin {
		t.Errorf("the stored origin is %q, want the hosted console's %q",
			rows[0].Origin, adaptercloud.DefaultAppOrigin)
	}

	message, _, ok := adapterpush.Build(adapterpush.Notification{
		Title: "Clawdline", Body: "一個 session 在等你", URL: "/"}, rows[0])
	if !ok {
		t.Fatal("no envelope could be built for this subscription")
	}
	if message.ContentType != "application/notification+json" {
		t.Fatalf("an Apple endpoint with an origin took the %q envelope", message.ContentType)
	}
	var declared struct {
		Notification struct {
			Navigate string `json:"navigate"`
		} `json:"notification"`
	}
	if err := json.Unmarshal(message.Plaintext, &declared); err != nil {
		t.Fatalf("the declarative payload does not parse: %v", err)
	}
	if want := adaptercloud.DefaultAppOrigin + "/"; declared.Notification.Navigate != want {
		t.Errorf("tapping the notification opens %q, want %q", declared.Notification.Navigate, want)
	}
}

// TestACloudSubscriptionFollowsAConsoleSomebodyMoved is the same property for
// a machine pointed at a console of its own.
//
// The value is `cloud_app_origin`, which the link reads at startup and hands
// to its router, so a person running their own console — or a build pointed at
// a staging one — is followed there rather than to the constant. Writing the
// constant into the route would have passed the test above and stranded
// exactly those people.
func TestACloudSubscriptionFollowsAConsoleSomebodyMoved(t *testing.T) {
	const moved = "https://console.example"
	s := newPushStandInFor(t, moved)

	answer, _ := s.ask(t, 1, map[string]any{"type": "push-subscribe",
		"session": cloudops.MachineReplySession, "request": "req-sub",
		"subscription": cloudSubscription(t, "https://web.push.apple.com/QWxpY2U")})
	if !answer.OK() {
		t.Fatalf("the viewer could not subscribe: %d/%q %s",
			answer.Status, answer.Code, answer.Payload)
	}
	rows := s.rows(t)
	if len(rows) != 1 {
		t.Fatalf("one registration wrote %d subscriptions", len(rows))
	}
	if rows[0].Origin != moved {
		t.Errorf("the stored origin is %q, want the console this machine was pointed at, %q",
			rows[0].Origin, moved)
	}
}

// TestTheCSRFAnswerIsUnchanged is the other half of the header this change
// stopped overloading, held still.
//
// `LocalAuthorizer` stamps `Origin: http://127.0.0.1` because for "where did
// this change come from" the answer genuinely is this daemon, and the gate
// refuses a change that cannot say. Nothing here may move that: a Cloud write
// that arrived without a same-origin answer would be refused by the gate, and
// the way to find that out must not be a person's session going quiet.
func TestTheCSRFAnswerIsUnchanged(t *testing.T) {
	var seen *http.Request
	router := cloud.Router{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = r
			w.WriteHeader(http.StatusNoContent)
		}),
		Authorize: cloud.LocalAuthorizer("local-token", "machine-token"),
		AppOrigin: "https://console.example",
	}
	if _, err := router.Do(context.Background(), cloudops.LocalRequest{
		Method: http.MethodPost, Path: "/v1/push/subscribe", Body: []byte("{}")}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := seen.Header.Get("Origin"); got != "http://127.0.0.1" {
		t.Errorf("the request came from %q; the gate is told this daemon, and a Cloud "+
			"write with any other answer is refused as cross-origin", got)
	}
	if got := seen.Header.Get("Sec-Fetch-Site"); got != "same-origin" {
		t.Errorf("Sec-Fetch-Site is %q", got)
	}
	// And the console travels beside it, where no socket can put it.
	if got := cloud.AppOriginOf(seen.Context()); got != "https://console.example" {
		t.Errorf("the router named the console %q", got)
	}
	if got := seen.Header.Get("X-Clawdline-App-Origin"); got != "" {
		t.Errorf("the console travelled as a header (%q), which a tunnelled browser could send too", got)
	}
}
