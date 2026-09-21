package cloud

// The acceptance test against a relay and control plane running on this
// machine. It is skipped unless both are pointed at, because it is the one
// test here that needs something outside this process:
//
//	CLAWDLINE_LIVE_API=http://127.0.0.1:8180 \
//	CLAWDLINE_LIVE_RELAY=ws://127.0.0.1:8787/v1/connect \
//	go test ./internal/adapters/cloud/ -run TestLive -v
//
// Both come from the cloud service, which is not part of this repository; how
// to run it locally is written down there, not here.
//
// **The local relay must be pointed at the local control plane.** Left at its
// default it asks the production one, `https://api.clawdline.com`, about
// entitlements and keys.
//
// The test registers its own account through the api's development stub OAuth,
// so it never touches anybody's real one.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

type liveEnvironment struct {
	apiBase  string
	relayURL string
	http     *http.Client
	// handle is the stub OAuth identity, and therefore the account. Each run
	// takes a fresh one: the free tier allows one machine per account, so
	// reusing a handle would fail the second run for a reason that has
	// nothing to do with the wire.
	handle string
}

func liveEnv(t *testing.T) *liveEnvironment {
	t.Helper()
	api := os.Getenv("CLAWDLINE_LIVE_API")
	relay := os.Getenv("CLAWDLINE_LIVE_RELAY")
	if api == "" || relay == "" {
		t.Skip("set CLAWDLINE_LIVE_API and CLAWDLINE_LIVE_RELAY to run the live acceptance test")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	var seed [6]byte
	if _, err := rand.Read(seed[:]); err != nil {
		t.Fatalf("handle: %v", err)
	}
	return &liveEnvironment{
		apiBase:  strings.TrimRight(api, "/"),
		relayURL: relay,
		http:     client,
		handle:   "live" + base64.RawURLEncoding.EncodeToString(seed[:]),
	}
}

// signIn walks the development stub OAuth: start redirects straight to the
// callback carrying `code=stub:<handle>`, and the callback sets the browser
// ticket this test then uses to approve its own machine.
func (e *liveEnvironment) signIn(t *testing.T) {
	t.Helper()
	noRedirect := *e.http
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	resp, err := noRedirect.Get(e.apiBase + "/v1/auth/oauth/start")
	if err != nil {
		t.Fatalf("oauth start: %v", err)
	}
	_ = resp.Body.Close()
	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatalf("oauth start answered %d with no redirect", resp.StatusCode)
	}
	// The stub answers `code=stub:demo`; this test wants its own account.
	location = strings.Replace(location, "code=stub%3Ademo", "code=stub%3A"+e.handle, 1)

	resp, err = noRedirect.Get(location)
	if err != nil {
		t.Fatalf("oauth callback: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Fatalf("oauth callback answered %d", resp.StatusCode)
	}
}

func (e *liveEnvironment) post(t *testing.T, path string, body any, out any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		reader = strings.NewReader(string(encoded))
	}
	req, err := http.NewRequest(http.MethodPost, e.apiBase+path, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.http.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		t.Fatalf("%s answered %d: %s", path, resp.StatusCode, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("%s answered unreadable JSON: %v", path, err)
		}
	}
}

// registerMachine does the whole device-authorization flow, with this test
// standing in for the browser at the approval step.
func (e *liveEnvironment) registerMachine(t *testing.T, key domain.DeviceKey) Identity {
	t.Helper()
	client := NewAccountClient(e.apiBase)
	start, err := client.StartLogin(context.Background(), "live-acceptance", "test", key.PublicKey(), "test")
	if err != nil {
		t.Fatalf("device start: %v", err)
	}
	e.post(t, "/v1/auth/device/approve", map[string]any{"user_code": start.UserCode}, nil)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		poll, err := client.PollLogin(context.Background(), start.DeviceCode)
		if err != nil {
			t.Fatalf("device poll: %v", err)
		}
		if poll.Status == LoginComplete {
			return Identity{
				AccountID:         poll.AccountID,
				MachineID:         poll.MachineID,
				MachineCredential: poll.MachineCredential,
				APIBase:           e.apiBase,
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("the machine was never approved")
	return Identity{}
}

// registerViewer creates a viewer device on the same account and answers its
// id and device token.
func (e *liveEnvironment) registerViewer(t *testing.T, public ed25519.PublicKey) (string, string) {
	t.Helper()
	var session struct {
		AccountID string   `json:"account_id"`
		DeviceID  string   `json:"device_id"`
		Caps      []string `json:"caps"`
	}
	e.post(t, "/v1/auth/session", map[string]any{
		"public_key": base64.StdEncoding.EncodeToString(public),
		"caps":       []string{"read_sessions", "read_transcript", "send_prompt", "start_session"},
	}, &session)

	var token DeviceToken
	e.post(t, "/v1/tokens/device", nil, &token)
	if token.Token == "" {
		t.Fatal("no viewer device token")
	}
	return session.DeviceID, token.Token
}

// TestLiveHandshakeAndDedupe is the acceptance run: a machine registers,
// connects, publishes, and then a viewer sends it the same envelope twice.
func TestLiveHandshakeAndDedupe(t *testing.T) {
	env := liveEnv(t)
	env.signIn(t)

	machineKey, err := domain.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatalf("machine key: %v", err)
	}
	secret, err := domain.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatalf("master secret: %v", err)
	}
	// The viewer is registered first, deliberately. `/v1/auth/session` needs
	// the browser login ticket, and approving a machine with that same fresh
	// ticket clears it (`api/src/routes/auth.ts:107`) — so doing it the other
	// way round leaves this test signed out halfway through.
	viewerKey, err := domain.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatalf("viewer key: %v", err)
	}
	viewerID, viewerToken := env.registerViewer(t, viewerKey.PublicKey())
	t.Logf("viewer %s, fingerprint %s", viewerID, viewerKey.Fingerprint())

	identity := env.registerMachine(t, machineKey)
	t.Logf("machine %s on account %s, fingerprint %s", identity.MachineID, identity.AccountID, machineKey.Fingerprint())

	spool, err := NewSpool(DefaultSpoolLimits(), &MemoryFence{}, time.Now)
	if err != nil {
		t.Fatalf("spool: %v", err)
	}
	status := NewStatusRecorder(time.Now())
	// The settings switch has its own tests; this one builds the transport
	// directly, so it says so rather than reporting a line that is off.
	status.SetEnabled(true)
	status.SetIdentity(identity.AccountID, identity.MachineID, env.relayURL)
	transport, err := New(Options{
		RelayURL: env.relayURL,
		Role:     "machine",
		Token:    &TokenSource{Client: NewAccountClient(env.apiBase), Credential: identity.MachineCredential},
		Identity: identity,
		Signer:   machineKey,
		Spool:    spool,
		Status:   status,
		Replay:   domain.NewReplayWindow(0),
		// Pairing is the next wave; for this run the viewer's key is pinned
		// directly, which is what a paired-device roster will answer.
		PublicKeyFor: func(sender string) (ed25519.PublicKey, bool) {
			if sender == viewerID {
				return viewerKey.PublicKey(), true
			}
			return nil, false
		},
		ContentKey: secret,
		Log:        func(line string) { t.Log(line) },
	})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = transport.Run(ctx) }()
	defer func() { transport.Stop(); <-done }()

	select {
	case <-transport.Ready():
	case <-time.After(30 * time.Second):
		t.Fatal("the machine never completed the handshake")
	}
	t.Log("step 1: the machine handshake completed")

	// Step 2: the machine publishes one snapshot and is acked.
	seq, err := spool.ReserveLatestValue(SpoolChannelOrch, "orch/"+identity.MachineID, "orch", 32)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	envelope, err := domain.Seal([]byte(`{"type":"orch","v":1,"tasks":[]}`), domain.SealParams{
		Ch: "orch/" + identity.MachineID, Seq: seq, Ts: uint64(time.Now().UnixMilli()),
		Class: domain.ClassStream, KeyID: MasterKeyID, Sender: identity.MachineID,
		Key: secret, Signer: machineKey, Rand: rand.Reader,
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	sealed, err := envelope.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	if err := spool.Seal(seq, sealed, time.Now()); err != nil {
		t.Fatalf("spool seal: %v", err)
	}
	waitFor(t, 20*time.Second, func() bool { return status.Snapshot().Acked >= 1 })
	t.Logf("step 2: the relay acked seq=%d on orch/%s", seq, identity.MachineID)

	// Step 3: a viewer sends the machine one command, twice, with the same
	// sequence and the same bytes.
	viewer, err := Dial(env.relayURL+"?role=viewer", DialOptions{
		Header:           http.Header{"Authorization": []string{"Bearer " + viewerToken}},
		HandshakeTimeout: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("viewer dial: %v", err)
	}
	defer viewer.Close()
	viewerHandshake(t, viewer, identity.AccountID, viewerID, viewerKey)
	t.Log("step 3: the viewer handshake completed")

	command, err := domain.Seal([]byte(`{"type":"send","v":1,"text":"hello"}`), domain.SealParams{
		Ch: "ctl/" + identity.MachineID, Seq: 4242, Ts: uint64(time.Now().UnixMilli()),
		Class: domain.ClassCtl, KeyID: MasterKeyID, Sender: viewerID,
		Key: secret, Signer: viewerKey, Rand: rand.Reader,
	})
	if err != nil {
		t.Fatalf("seal command: %v", err)
	}
	commandBytes, err := command.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonical command: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := viewer.WriteText(PublishFrame(commandBytes), time.Now().Add(10*time.Second)); err != nil {
			t.Fatalf("viewer publish %d: %v", i+1, err)
		}
	}

	waitFor(t, 20*time.Second, func() bool {
		snapshot := status.Snapshot()
		return snapshot.InboundTotal >= 1 && snapshot.InboundDropped[DropReplay] >= 1
	})
	snapshot := status.Snapshot()
	if snapshot.InboundTotal != 1 {
		t.Fatalf("the machine accepted %d copies of one command, want 1", snapshot.InboundTotal)
	}
	if snapshot.InboundDropped[DropReplay] != 1 {
		t.Fatalf("replay drops: %d, want 1", snapshot.InboundDropped[DropReplay])
	}
	t.Logf("step 4: the identical envelope arrived twice; accepted %d, refused as replay %d",
		snapshot.InboundTotal, snapshot.InboundDropped[DropReplay])

	// Step 5: the connection is dropped from underneath and comes back.
	before := status.Snapshot().Connects
	transport.mu.Lock()
	live := transport.conn
	transport.mu.Unlock()
	if live == nil {
		t.Fatal("no live connection to drop")
	}
	_ = live.Close()
	waitFor(t, 40*time.Second, func() bool { return status.Snapshot().Connects > before })
	final := status.Snapshot()
	t.Logf("step 5: reconnected — connects=%d reconnects=%d last_close=%s",
		final.Connects, final.Reconnects, final.LastClose)

	if encoded, err := json.MarshalIndent(final, "", "  "); err == nil {
		t.Logf("final status:\n%s", encoded)
	}
}

// viewerHandshake answers the relay's challenge as a viewer, which is the same
// two steps the transport does for a machine.
func viewerHandshake(t *testing.T, conn *Conn, account, device string, key domain.DeviceKey) {
	t.Helper()
	data, err := conn.Read(time.Now().Add(20 * time.Second))
	if err != nil {
		t.Fatalf("viewer challenge: %v", err)
	}
	challenge, err := decodeChallenge(data)
	if err != nil {
		t.Fatalf("viewer challenge: %v", err)
	}
	if challenge.Account != account || challenge.Device != device {
		t.Fatalf("the challenge names %s/%s, the viewer is %s/%s",
			challenge.Account, challenge.Device, account, device)
	}
	signature := key.Sign([]byte(ChallengeString(challenge.Account, challenge.Device, challenge.Challenge)))
	hello, err := json.Marshal(HelloFrame{Type: FrameHello, Sig: base64.StdEncoding.EncodeToString(signature)})
	if err != nil {
		t.Fatalf("viewer hello: %v", err)
	}
	if err := conn.WriteText(hello, time.Now().Add(20*time.Second)); err != nil {
		t.Fatalf("viewer hello: %v", err)
	}
	data, err = conn.Read(time.Now().Add(20 * time.Second))
	if err != nil {
		t.Fatalf("viewer ready: %v", err)
	}
	if _, err := decodeReady(data, challenge, "viewer"); err != nil {
		t.Fatalf("viewer ready: %v", err)
	}
	// The relay pushes realign snapshots to a viewer as soon as it is ready;
	// they are drained here so a later read is not answering an old frame.
	go func() {
		for {
			if _, err := conn.Read(time.Time{}); err != nil {
				return
			}
		}
	}()
}
