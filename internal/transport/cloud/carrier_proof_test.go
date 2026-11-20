package cloud

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
)

func TestAnotherViewerPreservesALiveCarrierProof(t *testing.T) {
	for _, terminalOpen := range []bool{true, false} {
		name := "carrier_only"
		if terminalOpen {
			name = "carrier_and_terminal_control"
		}
		t.Run(name, func(t *testing.T) {
			f := newDirectFixture(t)
			f.carrier(t)
			file := nextconfig.Open(t.TempDir())
			if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: true}); err != nil {
				t.Fatal(err)
			}
			f.l.file = file
			f.l.settings.Enabled = true
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{{ID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(f.viewer.PublicKey()), Caps: []string{"read_transcript"}}}})
			}))
			defer server.Close()
			f.l.roster = adaptercloud.NewRoster(server.URL, "credential", f.l.opts.Now)
			f.l.terminalAuthenticated = map[string]ed25519.PublicKey{"viewer": f.viewer.PublicKey()}
			if !terminalOpen {
				f.l.terminalConnections = map[string]*terminalConnection{}
			}
			f.l.terminalRequests = make(chan Inbound, 1)
			before, why := f.l.terminalAuthority("viewer", true)
			t.Logf("before: live_carrier=%v verdict=%v reason=%q", f.l.directPeerOpen("viewer"), before, why)
			if before != TerminalAllowed {
				t.Fatal("the carrier must begin with account authorization")
			}
			f.l.deliverTerminal(Inbound{Sender: "second-viewer", VerifiedKey: f.viewer.PublicKey()})
			after, why := f.l.terminalAuthority("viewer", true)
			t.Logf("after second viewer: live_carrier=%v verdict=%v reason=%q", f.l.directPeerOpen("viewer"), after, why)
			if !terminalOpen {
				f.l.sweepDirectCarriers()
				t.Logf("after sweep: live_carrier=%v", f.l.directPeerOpen("viewer"))
			}
			if !f.l.directPeerOpen("viewer") {
				t.Fatal("another viewer closed the existing carrier")
			}
			if after != TerminalAllowed {
				t.Fatalf("a second viewer erased a live carrier's authentication: %s", why)
			}
		})
	}
}

func TestAPendingCarrierKeepsItsProofUntilItsReservationEnds(t *testing.T) {
	f := newDirectFixture(t)
	file := nextconfig.Open(t.TempDir())
	if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: true}); err != nil {
		t.Fatal(err)
	}
	f.l.file = file
	f.l.settings.Enabled = true
	f.l.terminalConnections = map[string]*terminalConnection{}
	f.l.directPeers = map[string]*directPeer{"viewer": nil}
	f.l.terminalAuthenticated = map[string]ed25519.PublicKey{"viewer": f.viewer.PublicKey()}
	f.l.terminalRequests = make(chan Inbound, 2)
	incoming := Inbound{Sender: "second-viewer", VerifiedKey: f.viewer.PublicKey()}
	f.l.deliverTerminal(incoming)
	if verdict, why := f.l.terminalAuthority("viewer", true); verdict == TerminalDenied {
		t.Fatalf("a pending carrier lost its proof: %s", why)
	}
	f.l.releaseDirectReservation("viewer")
	f.l.deliverTerminal(incoming)
	if verdict, why := f.l.terminalAuthority("viewer", true); verdict != TerminalDenied || why != "this device has not authenticated a terminal envelope for this machine" {
		t.Fatalf("a retired carrier retained its proof: verdict=%v reason=%q", verdict, why)
	}
}
