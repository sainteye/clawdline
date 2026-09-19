package cloud

// A stand-in control plane and relay for driving the real CLI by hand.
//
// production_test.go proves the named failures in-process. This is the same
// set of answers on a loopback socket, so that a person can watch the real
// `clawdline cloud login` and `clawdline cloud connect` print each one — which
// is what docs/cloud-cutover.md §6 quotes. It is skipped unless asked for:
//
//	CLAWDLINE_D1_STANDIN=127.0.0.1:18180 go test ./internal/adapters/cloud -run TestStandInForTheCLI -timeout 30m
//
// then point an **empty, throwaway** CLAWDLINE_NEXT_DIR at it:
//
//	{"cloud_api_base":"http://127.0.0.1:18180",
//	 "cloud_relay_url":"ws://127.0.0.1:18180/v1/connect",
//	 "cloud_app_origin":"http://127.0.0.1:18180"}
//
// and pick what it answers with `curl -X POST 127.0.0.1:18180/__scenario?name=<scenario>`;
// `POST /__stop` ends it. It listens on loopback only and refuses any other
// address. It is not production-shaped TLS — the CLI cannot be handed a test
// CA — so the TLS and hostname half of the path is production_test.go's.

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

// standInScenarios is every answer the stand-in knows, and what it does.
var standInScenarios = map[string]string{
	"approve":          "login: pending once, then approved; connect: a full handshake",
	"deny":             "login: the person declines",
	"expire":           "login: the code expires unapproved (also what a full plan looks like from the Mac)",
	"pending":          "login: nobody ever approves (run login with a short --wait)",
	"start_not_found":  "login: a control plane without /v1/auth/device/start",
	"start_html":       "login: a web page answering 200 where the control plane should be",
	"token_no_session": "connect: the control plane no longer knows this machine's credential",
	"forbidden":        "connect: the relay knows this device as revoked",
	"over_capacity":    "connect: the plan connects one machine at a time and another holds it",
	"unauthorized":     "connect: the relay does not accept the token (another environment's)",
	"relay_v2":         "connect: a relay at protocol version 2",
	"upgrade_426":      "connect: the upgrade is answered 426",
}

type standIn struct {
	relay *fakeRelay

	mu       sync.Mutex
	scenario string
	polls    int
	stop     chan struct{}
	stopped  bool
}

func TestStandInForTheCLI(t *testing.T) {
	address := os.Getenv("CLAWDLINE_D1_STANDIN")
	if address == "" {
		t.Skip("set CLAWDLINE_D1_STANDIN=127.0.0.1:<port> to serve the stand-in for the CLI")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || (host != "127.0.0.1" && host != "::1" && host != "localhost") {
		t.Fatalf("the stand-in listens on loopback only, not %q", address)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &standIn{
		scenario: "approve",
		stop:     make(chan struct{}),
		relay:    &fakeRelay{account: "usr_standin", device: "mac_standin", ackStatus: AckDelivered},
	}
	server := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Logf("stand-in on %s; scenarios: %v", address, standInScenarios)
	<-s.stop
	_ = server.Close()
}

func (s *standIn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	scenario := s.scenario
	s.mu.Unlock()

	switch r.URL.Path {
	case "/__scenario":
		name := r.URL.Query().Get("name")
		if _, ok := standInScenarios[name]; !ok {
			http.Error(w, "unknown scenario", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.scenario, s.polls = name, 0
		s.mu.Unlock()
		s.relay.mu.Lock()
		s.relay.refuseAll, s.relay.challengeVersion = nil, 0
		switch name {
		case "forbidden":
			s.relay.refuseAll = &ErrorFrame{Type: FrameError, Code: CodeForbidden, Message: "this device has been revoked"}
		case "over_capacity":
			s.relay.refuseAll = &ErrorFrame{Type: FrameError, Code: CodeOverCapacity, Message: "this plan connects 1 machine at a time"}
		case "unauthorized":
			s.relay.refuseAll = &ErrorFrame{Type: FrameError, Code: CodeUnauthorized, Message: "issuer is not ours"}
		case "relay_v2":
			s.relay.challengeVersion = 2
		}
		s.relay.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case "/__stop":
		s.mu.Lock()
		if !s.stopped {
			s.stopped = true
			close(s.stop)
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case "/v1/auth/device/start":
		switch scenario {
		case "start_not_found":
			apiRefusal(w, http.StatusNotFound, "not_found", "No such route")
			return
		case "start_html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><title>Welcome</title>"))
			return
		}
		var body struct {
			PublicKey string `json:"public_key"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			apiRefusal(w, http.StatusBadRequest, "bad_machine", "name, platform and public_key are required")
			return
		}
		key, err := base64.StdEncoding.DecodeString(body.PublicKey)
		if err != nil || len(key) != ed25519.PublicKeySize {
			apiRefusal(w, http.StatusBadRequest, "bad_public_key", "public_key must be a base64 Ed25519 public key (32 bytes)")
			return
		}
		// The relay half verifies the hello against the key the machine
		// declared here, as the real pair does through the token's `pk`.
		s.relay.mu.Lock()
		s.relay.publicKey = ed25519.PublicKey(key)
		s.relay.mu.Unlock()
		page := "http://" + r.Host + "/connect"
		_ = json.NewEncoder(w).Encode(LoginStart{
			DeviceCode: "stand-in-device-code", UserCode: "STND-1234",
			VerificationURI: page, VerificationURIComplete: page + "?user_code=STND-1234",
			ExpiresIn: 600, Interval: 1,
		})
	case "/v1/auth/device/poll":
		s.mu.Lock()
		s.polls++
		polls := s.polls
		s.mu.Unlock()
		status := LoginPending
		switch scenario {
		case "approve", "token_no_session", "forbidden", "over_capacity", "unauthorized", "relay_v2", "upgrade_426":
			if polls > 1 {
				_ = json.NewEncoder(w).Encode(LoginPoll{Status: LoginComplete, AccountID: "usr_standin", MachineID: "mac_standin", MachineCredential: "stand-in-credential"})
				return
			}
		case "deny":
			status = LoginDenied
		case "expire":
			status = LoginExpired
		}
		_ = json.NewEncoder(w).Encode(LoginPoll{Status: status})
	case "/v1/tokens/device":
		if scenario == "token_no_session" {
			apiRefusal(w, http.StatusUnauthorized, "no_session", "Sign in first")
			return
		}
		_ = json.NewEncoder(w).Encode(DeviceToken{
			Token: "stand-in-token", TokenType: "Bearer", ExpiresIn: 300,
			ExpiresAt: time.Now().Add(5 * time.Minute), Kid: "stand-in",
		})
	case "/v1/connect":
		if scenario == "upgrade_426" {
			w.WriteHeader(http.StatusUpgradeRequired)
			return
		}
		s.relay.connect(w, r)
	default:
		apiRefusal(w, http.StatusNotFound, "not_found", "No such route")
	}
}
