package cloud

// The production path, with production standing in this process.
//
// docs/cloud-cutover.md stops before the person's account is touched, so the
// production endpoints have never answered this code. What can be proven
// without them is everything on this side of the wire: that the default
// settings name `https://api.clawdline.com` and
// `wss://relay.clawdline.com/v1/connect`, that the TLS handshake checks those
// names, that the requests carry them as Host and SNI, and that every way the
// far end says no — the five the cutover names and the one it does not — comes
// back to the person as a named failure with a remedy.
//
// The two servers here answer in the production shapes, copied from the
// source that is deployed (the Cloud service's own routes, its error handler and its
// entitlement rules), under a certificate for the production
// hostnames issued by a CA that only this test trusts.
//
// **No byte leaves this machine.** Both clients dial through
// fakeProduction.dial, which maps the two production host:port pairs to
// loopback listeners and refuses everything else, recording every address it
// was asked for; each test ends by asserting the record holds nothing else.
// TestTheHarnessRefusesEveryOtherAddress is the control that shows that
// assertion can fail.

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/nextconfig"
	domain "github.com/sainteye/clawdline-go/internal/domain/cloud"
)

const (
	productionAPIHost   = "api.clawdline.com"
	productionRelayHost = "relay.clawdline.com"
)

// fakeProduction is the control plane and the relay, as the defaults name them.
type fakeProduction struct {
	t     *testing.T
	api   *httptest.Server
	relay *fakeRelay
	// relayServer is the TLS listener in front of relay.connect.
	relayServer *httptest.Server
	pool        *x509.CertPool
	routes      map[string]string

	mu sync.Mutex
	// dialed is every host:port a client asked for, mapped or not.
	dialed []string
	// startAnswer is "" (a code), "not_found", or "html".
	startAnswer string
	// polls is what successive polls answer; the last repeats.
	polls []string
	// tokenRefusal is "" or the api code a token request is refused with.
	tokenRefusal string
	// upgradeStatus answers the relay upgrade with this plain HTTP status.
	upgradeStatus int
	// starts, pollCount and mints count the control plane's three routes.
	starts, pollCount, mints int
	// hosts and sni are what the far ends saw, to prove the production names
	// were on the wire.
	apiHosts, relayHosts, relaySNI []string
}

func newFakeProduction(t *testing.T, identity Identity, publicKey ed25519.PublicKey) *fakeProduction {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "the D1 test CA, trusted by this test only"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("ca parse: %v", err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: productionAPIHost},
		DNSNames:     []string{productionAPIHost, productionRelayHost},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("leaf: %v", err)
	}
	leaf := tls.Certificate{Certificate: [][]byte{leafDER, caDER}, PrivateKey: leafKey}
	pool := x509.NewCertPool()
	pool.AddCert(ca)

	fp := &fakeProduction{t: t, pool: pool}
	fp.api = httptest.NewUnstartedServer(http.HandlerFunc(fp.serveAPI))
	fp.api.TLS = &tls.Config{Certificates: []tls.Certificate{leaf}}
	fp.api.StartTLS()
	t.Cleanup(fp.api.Close)

	fp.relay = &fakeRelay{account: identity.AccountID, device: identity.MachineID, publicKey: publicKey, ackStatus: AckDelivered}
	fp.relayServer = httptest.NewUnstartedServer(http.HandlerFunc(fp.serveRelay))
	fp.relayServer.TLS = &tls.Config{Certificates: []tls.Certificate{leaf}}
	fp.relayServer.StartTLS()
	t.Cleanup(fp.relayServer.Close)

	fp.routes = map[string]string{
		productionAPIHost + ":443":   fp.api.Listener.Addr().String(),
		productionRelayHost + ":443": fp.relayServer.Listener.Addr().String(),
	}
	t.Cleanup(fp.assertOnlyFakesDialed)
	return fp
}

// dial is the only way out of this test. It answers the two production
// names with the fakes and refuses everything else.
func (fp *fakeProduction) dial(network, address string) (net.Conn, error) {
	fp.mu.Lock()
	fp.dialed = append(fp.dialed, address)
	to, ok := fp.routes[address]
	fp.mu.Unlock()
	if !ok || network != "tcp" {
		return nil, fmt.Errorf("the test network reaches only the fakes, not %s", address)
	}
	return net.Dial("tcp", to)
}

// set changes the fakes' answers under their lock.
func (fp *fakeProduction) set(change func()) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	change()
}

// relaySet changes the relay's answers under its lock.
func (fp *fakeProduction) relaySet(change func(*fakeRelay)) {
	fp.relay.mu.Lock()
	defer fp.relay.mu.Unlock()
	change(fp.relay)
}

// unroute makes a production name answer "connection refused", as a relay
// that is down does.
func (fp *fakeProduction) unroute(host string) {
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fp.t.Fatalf("listen: %v", err)
	}
	address := closed.Addr().String()
	_ = closed.Close()
	fp.mu.Lock()
	fp.routes[host+":443"] = address
	fp.mu.Unlock()
}

func (fp *fakeProduction) assertOnlyFakesDialed() {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if err := onlyProductionNames(fp.dialed); err != nil {
		fp.t.Error(err)
	}
}

// onlyProductionNames is the harness's own check, separate so that its
// control can call it on a record it did not produce.
func onlyProductionNames(dialed []string) error {
	for _, address := range dialed {
		if address != productionAPIHost+":443" && address != productionRelayHost+":443" {
			return fmt.Errorf("a client asked for %s, which is not one of the two production names this harness answers", address)
		}
	}
	return nil
}

// httpClient is the control plane client's transport: the production name,
// the fake's socket, the test CA and nothing from the environment (no proxy).
func (fp *fakeProduction) httpClient(trusted bool) *http.Client {
	config := &tls.Config{}
	if trusted {
		config.RootCAs = fp.pool
	}
	return &http.Client{
		Timeout: OpeningTimeout,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
				return fp.dial(network, address)
			},
			TLSClientConfig: config,
		},
	}
}

func (fp *fakeProduction) accountClient(settings Settings) *AccountClient {
	client := NewAccountClient(settings.APIBase)
	client.HTTP = fp.httpClient(true)
	return client
}

func apiRefusal(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// serveAPI answers the three routes a machine calls, in the shapes
// the Cloud service's auth and token routes answer.
func (fp *fakeProduction) serveAPI(w http.ResponseWriter, r *http.Request) {
	fp.mu.Lock()
	fp.apiHosts = append(fp.apiHosts, r.Host)
	startAnswer, tokenRefusal := fp.startAnswer, fp.tokenRefusal
	fp.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/device/start":
		fp.mu.Lock()
		fp.starts++
		fp.mu.Unlock()
		switch startAnswer {
		case "not_found":
			// A control plane without this route: the api's not-found handler.
			apiRefusal(w, http.StatusNotFound, "not_found", "No such route")
			return
		case "html":
			// Something that is not a control plane at all, answering 200.
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><title>Welcome</title>"))
			return
		}
		_ = json.NewEncoder(w).Encode(LoginStart{
			DeviceCode: "device-code", UserCode: "WXYZ-2345",
			VerificationURI:         "https://clawdline.com/connect",
			VerificationURIComplete: "https://clawdline.com/connect?user_code=WXYZ-2345",
			ExpiresIn:               600, Interval: 5,
		})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/device/poll":
		fp.mu.Lock()
		status := LoginPending
		if len(fp.polls) > 0 {
			index := fp.pollCount
			if index >= len(fp.polls) {
				index = len(fp.polls) - 1
			}
			status = fp.polls[index]
		}
		fp.pollCount++
		fp.mu.Unlock()
		switch status {
		case LoginComplete:
			_ = json.NewEncoder(w).Encode(LoginPoll{Status: LoginComplete, AccountID: "usr_prod", MachineID: "mac_prod", MachineCredential: "machine-credential"})
		case LoginSlowDown:
			_ = json.NewEncoder(w).Encode(LoginPoll{Status: LoginSlowDown, RetryAfterSeconds: 7})
		default:
			_ = json.NewEncoder(w).Encode(LoginPoll{Status: status})
		}
	case r.Method == http.MethodPost && r.URL.Path == "/v1/tokens/device":
		fp.mu.Lock()
		fp.mints++
		fp.mu.Unlock()
		if tokenRefusal != "" {
			// `requireAccount` finds no machine for the credential and falls
			// through to `requireSession`: 401 no_session, "Sign in first".
			apiRefusal(w, http.StatusUnauthorized, tokenRefusal, "Sign in first")
			return
		}
		_ = json.NewEncoder(w).Encode(DeviceToken{
			Token: "a-device-token", TokenType: "Bearer", ExpiresIn: 300,
			ExpiresAt: time.Now().Add(5 * time.Minute), Kid: "k1",
			RelayURL: "wss://" + productionRelayHost + "/v1/connect",
		})
	default:
		apiRefusal(w, http.StatusNotFound, "not_found", "No such route")
	}
}

func (fp *fakeProduction) serveRelay(w http.ResponseWriter, r *http.Request) {
	fp.mu.Lock()
	fp.relayHosts = append(fp.relayHosts, r.Host)
	if r.TLS != nil {
		fp.relaySNI = append(fp.relaySNI, r.TLS.ServerName)
	}
	status := fp.upgradeStatus
	fp.mu.Unlock()
	if r.URL.Path != "/v1/connect" {
		http.NotFound(w, r)
		return
	}
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	fp.relay.connect(w, r)
}

// productionSettings reads a settings file that says nothing: the defaults
// are the production endpoints, and that is the path under test.
func productionSettings(t *testing.T) Settings {
	t.Helper()
	settings, err := ReadSettings(nextconfig.Open(t.TempDir()))
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if settings.APIBase != "https://"+productionAPIHost || settings.RelayURL != "wss://"+productionRelayHost+"/v1/connect" {
		t.Fatalf("the defaults are not the production endpoints: %+v", settings)
	}
	return settings
}

type productionLine struct {
	fp        *fakeProduction
	transport *Transport
	status    *StatusRecorder
	done      chan error
	cancel    context.CancelFunc
}

// newProductionLine builds the transport exactly as the daemon does from
// production settings, with the harness's dialer and CA.
func newProductionLine(t *testing.T, challengeAccount string) *productionLine {
	t.Helper()
	settings := productionSettings(t)
	key, err := domain.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatalf("device key: %v", err)
	}
	identity := Identity{AccountID: "usr_prod", MachineID: "mac_prod", MachineCredential: "machine-credential", APIBase: settings.APIBase}
	challengeIdentity := identity
	if challengeAccount != "" {
		challengeIdentity.AccountID = challengeAccount
	}
	fp := newFakeProduction(t, challengeIdentity, key.PublicKey())
	status := NewStatusRecorder(time.Now())
	transport, err := New(Options{
		RelayURL:     settings.RelayURL,
		Role:         "machine",
		Token:        &TokenSource{Client: fp.accountClient(settings), Credential: identity.MachineCredential},
		Identity:     identity,
		Signer:       key,
		Status:       status,
		Replay:       domain.NewReplayWindow(0),
		PublicKeyFor: func(string) (ed25519.PublicKey, bool) { return nil, false },
		Jitter:       func() float64 { return 0.5 },
		TLSConfig:    &tls.Config{RootCAs: fp.pool},
		NetDial:      fp.dial,
	})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	return &productionLine{fp: fp, transport: transport, status: status}
}

func (l *productionLine) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.done = make(chan error, 1)
	go func() { l.done <- l.transport.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		l.transport.Stop()
		select {
		case <-l.done:
		case <-time.After(5 * time.Second):
			t.Error("the transport did not stop")
		}
	})
}

// ended waits for Run to return by itself, which only a terminal failure does.
func (l *productionLine) ended(t *testing.T) error {
	t.Helper()
	select {
	case err := <-l.done:
		l.done <- err // for the cleanup
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the line did not stop on a terminal refusal")
		return nil
	}
}

// reconnecting waits until the line has come round the ladder at least n
// times, and answers the status at that point.
func (l *productionLine) reconnecting(t *testing.T, n int) Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if snapshot := l.status.Snapshot(); snapshot.Reconnects >= n {
			return snapshot
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the line did not retry %d times: %+v", n, l.status.Snapshot())
	return Status{}
}

func (fp *fakeProduction) counts() (starts, polls, mints int) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return fp.starts, fp.pollCount, fp.mints
}

// ---------------------------------------------------------------------------

// The success path first, because every refusal below is only meaningful
// against a path that works: default settings, the production names on the
// wire, the TLS check on, a handshake to `ready`.
func TestTheProductionPathConnectsAgainstAStandIn(t *testing.T) {
	t.Parallel()
	line := newProductionLine(t, "")
	line.start(t)
	select {
	case <-line.transport.Ready():
	case <-time.After(10 * time.Second):
		t.Fatalf("no handshake: %+v", line.status.Snapshot())
	}
	fp := line.fp
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.apiHosts) == 0 || fp.apiHosts[0] != productionAPIHost {
		t.Errorf("the control plane saw Host %v", fp.apiHosts)
	}
	if len(fp.relayHosts) == 0 || fp.relayHosts[0] != productionRelayHost {
		t.Errorf("the relay saw Host %v", fp.relayHosts)
	}
	if len(fp.relaySNI) == 0 || fp.relaySNI[0] != productionRelayHost {
		t.Errorf("the relay's TLS saw SNI %v", fp.relaySNI)
	}
}

// Login, the person's step: every way it can end, named.
func TestEveryWayALoginEndsIsNamed(t *testing.T) {
	t.Parallel()
	key, err := domain.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatalf("device key: %v", err)
	}
	for _, c := range []struct {
		name        string
		startAnswer string
		polls       []string
		wait        time.Duration
		kind        FailureKind
		code        string
	}{
		{name: "approved", polls: []string{LoginPending, LoginSlowDown, LoginComplete}, wait: time.Minute},
		{name: "declined in the browser", polls: []string{LoginPending, LoginDenied}, wait: time.Minute, kind: KindNotApproved, code: "login_denied"},
		// What the Mac sees when the approval page refused because the plan
		// is full: the code simply never gets approved.
		{name: "expired, as a full plan looks from here", polls: []string{LoginPending, LoginExpired}, wait: time.Minute, kind: KindNotApproved, code: "login_expired"},
		{name: "nobody came", polls: []string{LoginPending}, wait: 30 * time.Millisecond, kind: KindNotApproved, code: "login_timeout"},
		{name: "a control plane without this route", startAnswer: "not_found", kind: KindVersionMismatch, code: "api_not_found"},
		{name: "not a control plane at all", startAnswer: "html", kind: KindVersionMismatch, code: "incompatible"},
		{name: "a poll status nobody wrote down", polls: []string{"authorization_suspended"}, wait: time.Minute, kind: KindVersionMismatch, code: "incompatible"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			settings := productionSettings(t)
			fp := newFakeProduction(t, Identity{AccountID: "usr_prod", MachineID: "mac_prod"}, key.PublicKey())
			fp.set(func() { fp.startAnswer, fp.polls = c.startAnswer, c.polls })
			client := fp.accountClient(settings)
			ctx := context.Background()

			var sleeps []time.Duration
			sleep := func(_ context.Context, d time.Duration) error {
				sleeps = append(sleeps, d)
				time.Sleep(2 * time.Millisecond)
				return nil
			}
			var got error
			start, err := client.StartLogin(ctx, "desk", "darwin", key.PublicKey(), "test")
			if err != nil {
				got = err
			} else {
				if start.VerificationURIComplete == "" || start.UserCode == "" {
					t.Fatalf("the start answer lost its code: %+v", start)
				}
				_, got = client.WaitForApproval(ctx, start, c.wait, sleep)
			}
			failure := DescribeFailure(got)
			if failure.Kind != c.kind || failure.Code != c.code {
				t.Fatalf("got %q (%v), want %s/%s", failure, got, c.kind, c.code)
			}
			if c.kind != "" && failure.Remedy() == "" {
				t.Errorf("%s has no remedy", failure.Code)
			}
			if c.name == "approved" {
				// The server's pace, not ours: 5 s from start, then the 7 s
				// slow_down asked for.
				if len(sleeps) < 3 || sleeps[0] != 5*time.Second || sleeps[2] != 7*time.Second {
					t.Errorf("the poll did not follow the server's interval: %v", sleeps)
				}
				token, err := client.MintDeviceToken(ctx, "machine-credential")
				if err != nil || token.Token == "" {
					t.Errorf("an approved credential bought no token: %v", err)
				}
			}
		})
	}
}

// Connect, the daemon's step: every way the relay or the control plane can
// refuse a machine, named, and each one's effect on the line.
func TestEveryWayAConnectionIsRefusedIsNamed(t *testing.T) {
	t.Parallel()

	t.Run("not signed in: the control plane no longer knows the credential", func(t *testing.T) {
		t.Parallel()
		line := newProductionLine(t, "")
		line.fp.set(func() { line.fp.tokenRefusal = "no_session" })
		line.start(t)
		err := line.ended(t)
		assertNamed(t, err, KindNotSignedIn, "api_no_session")
		if got := line.status.Snapshot(); got.State != StateStopped || got.LastClose != "api_no_session" {
			t.Errorf("status %+v", got)
		}
		line.fp.mu.Lock()
		defer line.fp.mu.Unlock()
		if len(line.fp.relayHosts) != 0 {
			t.Error("the relay was dialled without a token")
		}
	})

	t.Run("device not approved: the account revoked this machine", func(t *testing.T) {
		t.Parallel()
		line := newProductionLine(t, "")
		line.fp.relaySet(func(r *fakeRelay) {
			r.refuseAll = &ErrorFrame{Type: FrameError, Code: CodeForbidden, Message: "this device has been revoked"}
		})
		line.start(t)
		err := line.ended(t)
		assertNamed(t, err, KindNotApproved, "relay_forbidden")
	})

	t.Run("entitlement: the plan connects one machine at a time", func(t *testing.T) {
		t.Parallel()
		line := newProductionLine(t, "")
		// relay/src/lib/entitlements.ts:199-205: the *new* connection is
		// refused; the machine already connected is not touched.
		line.fp.relaySet(func(r *fakeRelay) {
			r.refuseAll = &ErrorFrame{Type: FrameError, Code: CodeOverCapacity, Message: "this plan connects 1 machine at a time"}
		})
		line.start(t)
		status := line.reconnecting(t, 2)
		if status.LastClose != "relay_over_capacity" || KindOfCode(status.LastClose) != KindEntitlement {
			t.Errorf("last close %q (%s)", status.LastClose, KindOfCode(status.LastClose))
		}
		if _, _, mints := line.fp.counts(); mints != 1 {
			// A full plan is not a credential problem: the token is reused.
			t.Errorf("a full plan minted %d tokens", mints)
		}
	})

	t.Run("version mismatch: a relay at protocol version 2", func(t *testing.T) {
		t.Parallel()
		line := newProductionLine(t, "")
		line.fp.relaySet(func(r *fakeRelay) { r.challengeVersion = 2 })
		line.start(t)
		status := line.reconnecting(t, 1)
		if status.LastClose != "incompatible" || KindOfCode(status.LastClose) != KindVersionMismatch {
			t.Errorf("last close %q", status.LastClose)
		}
		if line.fp.relay.Connections() != 0 {
			t.Error("a challenge at another version was answered")
		}
	})

	t.Run("version mismatch: an upgrade answered 426", func(t *testing.T) {
		t.Parallel()
		line := newProductionLine(t, "")
		line.fp.set(func() { line.fp.upgradeStatus = http.StatusUpgradeRequired })
		line.start(t)
		status := line.reconnecting(t, 1)
		if KindOfCode(status.LastClose) != KindVersionMismatch {
			t.Errorf("last close %q (%s)", status.LastClose, KindOfCode(status.LastClose))
		}
	})

	t.Run("relay refused: it does not accept the token", func(t *testing.T) {
		t.Parallel()
		line := newProductionLine(t, "")
		line.fp.relaySet(func(r *fakeRelay) {
			r.refuseAll = &ErrorFrame{Type: FrameError, Code: CodeUnauthorized, Message: "issuer is not ours"}
		})
		line.start(t)
		status := line.reconnecting(t, 2)
		if status.LastClose != "relay_unauthorized" || KindOfCode(status.LastClose) != KindRelayRefused {
			t.Errorf("last close %q", status.LastClose)
		}
		// An in-band `unauthorized` asks for a new token, so every attempt
		// costs one control-plane call; the backoff is what bounds it.
		if _, _, mints := line.fp.counts(); mints < 2 {
			t.Errorf("the token was not re-minted: %d", mints)
		}
	})

	t.Run("relay refused: the challenge names another account", func(t *testing.T) {
		t.Parallel()
		line := newProductionLine(t, "usr_somebody_else")
		line.start(t)
		status := line.reconnecting(t, 1)
		if status.LastClose != "identity_binding" || KindOfCode(status.LastClose) != KindRelayRefused {
			t.Errorf("last close %q", status.LastClose)
		}
	})

	t.Run("unavailable: nothing is listening", func(t *testing.T) {
		t.Parallel()
		line := newProductionLine(t, "")
		line.fp.unroute(productionRelayHost)
		line.start(t)
		status := line.reconnecting(t, 1)
		if status.LastClose != "unreachable" || KindOfCode(status.LastClose) != KindUnavailable {
			t.Errorf("last close %q", status.LastClose)
		}
	})
}

// The certificate check is on: a server this machine has no reason to trust is
// refused before a byte of the request is written.
func TestAnUntrustedCertificateIsRefusedAndNamed(t *testing.T) {
	t.Parallel()
	key, err := domain.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatalf("device key: %v", err)
	}
	settings := productionSettings(t)
	fp := newFakeProduction(t, Identity{AccountID: "usr_prod", MachineID: "mac_prod"}, key.PublicKey())
	client := NewAccountClient(settings.APIBase)
	client.HTTP = fp.httpClient(false)
	_, err = client.StartLogin(context.Background(), "desk", "darwin", key.PublicKey(), "test")
	assertNamed(t, err, KindUnavailable, "tls_untrusted")
	if starts, _, _ := fp.counts(); starts != 0 {
		t.Errorf("the request reached a server whose certificate did not verify")
	}
}

// The control for every test above: the harness's record check goes red on an
// address that is not one of the two production names, and the dialer refuses
// to connect to one.
func TestTheHarnessRefusesEveryOtherAddress(t *testing.T) {
	t.Parallel()
	if err := onlyProductionNames([]string{productionAPIHost + ":443", "example.com:443"}); err == nil {
		t.Fatal("the record check passed an address the harness does not answer")
	}
	key, err := domain.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatalf("device key: %v", err)
	}
	fp := newFakeProduction(t, Identity{AccountID: "a", MachineID: "m"}, key.PublicKey())
	if _, err := fp.dial("tcp", productionAPIHost+":8443"); err == nil {
		t.Fatal("the dialer connected an address it does not map")
	}
	// Take the refused address back out so that this test's own cleanup,
	// which asserts the same thing, does not fail on the control itself.
	fp.mu.Lock()
	fp.dialed = nil
	fp.mu.Unlock()
}

func assertNamed(t *testing.T, err error, kind FailureKind, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want %s/%s", kind, code)
	}
	failure := DescribeFailure(err)
	if failure.Kind != kind || failure.Code != code {
		t.Fatalf("named %q, want %s/%s", failure, kind, code)
	}
	if failure.Remedy() == "" {
		t.Errorf("%s has no remedy", code)
	}
}

// Every word FailureCode can say belongs to a kind, and each of the five the
// cutover names is reachable from at least one of them.
func TestEveryFailureWordHasAKind(t *testing.T) {
	t.Parallel()
	named := map[FailureKind]bool{}
	for code, kind := range kindByCode {
		switch kind {
		case "", KindNotSignedIn, KindNotApproved, KindEntitlement, KindVersionMismatch, KindRelayRefused, KindUnavailable, KindUnknown:
		default:
			t.Errorf("%s maps to %q, which is not one of the seven", code, kind)
		}
		named[kind] = true
	}
	for _, kind := range []FailureKind{KindNotSignedIn, KindNotApproved, KindEntitlement, KindVersionMismatch, KindRelayRefused, KindUnavailable} {
		if !named[kind] {
			t.Errorf("no failure word is %s", kind)
		}
	}
	for _, c := range []struct {
		err  error
		code string
		kind FailureKind
	}{
		{ErrNoIdentity, "no_identity", KindNotSignedIn},
		{fmt.Errorf("%w: x", ErrOtherEnvironment), "identity_other_environment", KindNotSignedIn},
		{&APIError{Status: 409, Code: "machine_limit_reached", Message: "free allows 1 machine(s)"}, "api_machine_limit_reached", KindEntitlement},
		{&APIError{Status: 404}, "api_http_404", KindVersionMismatch},
		{&APIError{Status: 401}, "api_http_401", KindNotSignedIn},
		{&APIError{Status: 503, Code: "temporarily_unavailable"}, "api_temporarily_unavailable", KindUnavailable},
		{&APIError{Status: 409, Code: "something_new"}, "api_something_new", KindUnknown},
		{&APIError{Status: 400, Code: "Not A Code"}, "api_unrecognized", KindUnknown},
		{&RelayError{Code: strings.Repeat("x", maxFailureCodeBytes+1)}, "relay_unrecognized", KindRelayRefused},
		{&CloseError{Code: 4429}, "closed_4429", KindEntitlement},
		{&CloseError{Code: 1006}, "closed_1006", KindUnavailable},
		{&UpgradeError{Status: 502}, "upgrade_refused_502", KindUnavailable},
		{ErrTokenRotated, "token_rotation", ""},
		{ErrDisabled, "switched_off", ""},
	} {
		if got := DescribeFailure(c.err); got.Code != c.code && c.kind != "" || got.Kind != c.kind {
			t.Errorf("%v: %q, want %s/%s", c.err, got, c.kind, c.code)
		}
		if got := FailureCode(c.err); got != c.code {
			t.Errorf("%v: code %q, want %q", c.err, got, c.code)
		}
	}
	// A 401 from the control plane is still terminal for the transport: the
	// type that names it did not change what it does.
	if !IsTerminalAuthorization(&APIError{Status: 401, Code: "no_session"}) {
		t.Error("a 401 from the token endpoint no longer stops the line")
	}
	if !errors.Is(&APIError{Status: 403}, ErrUnauthorized) {
		t.Error("a 403 is no longer ErrUnauthorized")
	}
}
