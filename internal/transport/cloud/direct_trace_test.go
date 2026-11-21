package cloud

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// A real-shaped offer: what a phone's browser sends, with a private host
// address, an mDNS name, a public srflx IPv4, a global IPv6, a TCP candidate
// and the credentials every SDP carries. The private addresses are built at run
// time, as in TestDirectOfferRefusals, so no address outside the documentation
// ranges sits in the repository.
var (
	traceHome, traceOffice = net.IPv4(192, 168, 1, 23).String(), net.IPv4(10, 0, 0, 5).String()
)

var traceOffer = "v=0\r\n" +
	"o=- 4611731400430051336 2 IN IP4 127.0.0.1\r\n" +
	"s=-\r\n" +
	"a=fingerprint:sha-256 4A:AD:B9:B1:3F:82:18:3B:54:02:12:DF:3E:5D:49:6B:19:E5:7C:AB:4A:AD:B9:B1:3F:82:18:3B:54:02:12:DF\r\n" +
	"m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\n" +
	"c=IN IP4 0.0.0.0\r\n" +
	"a=ice-ufrag:Qx7v\r\n" +
	"a=ice-pwd:k2b0Jz5PqWrYcVdXtLmNnFhG\r\n" +
	"a=candidate:1 1 udp 2122260223 " + traceHome + " 51234 typ host generation 0\r\n" +
	"a=candidate:2 1 udp 2122260223 0f1e2d3c.local 51235 typ host generation 0\r\n" +
	"a=candidate:3 1 udp 1686052607 203.0.113.77 61001 typ srflx raddr " + traceHome + " rport 51234 generation 0\r\n" +
	"a=candidate:4 1 udp 1686052351 2001:db8:85a3::8a2e:370:7334 61002 typ srflx raddr :: rport 0 generation 0\r\n" +
	"a=candidate:5 1 tcp 1518280447 198.51.100.9 9 typ host tcptype active generation 0\r\n" +
	"a=sctp-port:5000\r\n"

// The things no trace line may contain, whatever the offer carried.
var traceSecrets = []string{
	traceHome, traceOffice, "203.0.113.77", "2001:db8", "198.51.100.9", "0f1e2d3c", ".local",
	"51234", "51235", "61001", "61002", "Qx7v", "k2b0Jz5PqWrYcVdXtLmNnFhG", "4A:AD:B9",
	"fingerprint", "ice-ufrag", "ice-pwd", "a=candidate", "v=0", "IN IP4",
}

func TestDirectTraceCarriesNoAddressPortOrSDP(t *testing.T) {
	l := &Link{opts: LinkOptions{Now: time.Now}}
	trace := newDirectTrace("conn-aaaaaaaaaaaaaaaaaaaa", false, time.Now)
	kept, err := l.filterDirectOfferTraced(traceOffer, trace)
	if err != nil {
		t.Fatal(err)
	}
	// Tracing changes nothing about what the filter keeps.
	plain, err := l.filterDirectOffer(traceOffer)
	if err != nil || plain != kept {
		t.Fatalf("the traced filter kept something else: %v\n%s\n---\n%s", err, kept, plain)
	}
	answer := "v=0\r\na=ice-ufrag:Qx7v\r\n" +
		"a=candidate:1 1 udp 2130706431 " + traceOffice + " 50000 typ host\r\n" +
		"a=candidate:2 1 udp 1694498815 203.0.113.77 61001 typ srflx raddr " + traceOffice + " rport 50000\r\n"
	if got, want := l.filterDirectAnswerTraced(answer, trace), l.filterDirectAnswer(answer); got != want {
		t.Fatalf("the traced answer filter answered something else:\n%s\n---\n%s", got, want)
	}
	trace.gathered(true)
	trace.answerMade()
	trace.answerSent()
	trace.iceState("checking")
	trace.iceState("connected")
	trace.selectedPair(&webrtc.ICECandidatePair{
		Local:  &webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeSrflx, Address: "203.0.113.77", Port: 61001},
		Remote: &webrtc.ICECandidate{Typ: webrtc.ICECandidateTypePrflx, Address: "2001:db8:85a3::8a2e:370:7334", Port: 61002},
	})
	line := trace.line(true, false)
	for _, secret := range traceSecrets {
		if strings.Contains(line, secret) {
			t.Errorf("the trace carries %q:\n%s", secret, line)
		}
	}
	for _, want := range []string{
		"attempt=conn-aaaaaaaaaaaaaaaaaaaa", "kind=terminal", "stage=open", "answer=sent",
		"offer=srflx/ipv4:1,srflx/ipv6:1", "offer_dropped=name:host/mdns:1,private:host/ipv4:1,tcp:host/ipv4:1",
		"gather=complete", "answer_cands=srflx/ipv4:1", "answer_dropped=private:host/ipv4:1",
		"pair=srflx/ipv4>prflx/ipv6",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the trace lacks %q:\n%s", want, line)
		}
	}
	if !strings.Contains(line, "ice=checking@") || !strings.Contains(line, ",connected@") {
		t.Errorf("the trace lacks the ICE timeline:\n%s", line)
	}
	if again := trace.line(false, true); again != "" {
		t.Errorf("an attempt was traced twice: %s", again)
	}
}

func TestDirectTraceStageNamesWhereAnAttemptStopped(t *testing.T) {
	for _, c := range []struct {
		name                   string
		opened, answered, sent bool
		remote                 int
		ice                    []string
		timeout                bool
		want                   string
	}{
		{"open", true, true, true, 1, []string{"checking", "connected"}, false, directStageOpen},
		{"refused before an answer", false, false, false, 0, nil, false, directStageNoAnswer},
		{"answer never left", false, true, false, 1, nil, false, directStageAnswerNotSent},
		{"ICE connected, channel never opened", false, true, true, 1, []string{"checking", "connected"}, true, directStageConnectedNoChannel},
		{"connected through a peer-reflexive pair", false, true, true, 0, []string{"checking", "completed"}, true, directStageConnectedNoChannel},
		{"nothing to send to", false, true, true, 0, []string{"checking", "failed"}, true, directStageNoRemoteCandidates},
		{"checks failed", false, true, true, 2, []string{"checking", "failed"}, false, directStageChecksFailed},
		{"timed out while checking", false, true, true, 2, []string{"checking"}, true, directStageTimedOut},
		{"closed for another reason", false, true, true, 2, []string{"checking"}, false, directStageClosedEarly},
	} {
		if got := directTraceStage(c.opened, c.answered, c.sent, c.remote, c.ice, c.timeout); got != c.want {
			t.Errorf("%s: stage %s, want %s", c.name, got, c.want)
		}
	}
}

// An attempt that opens on loopback writes one line, with the pair it used,
// and the close that follows writes no second one.
func TestADirectAttemptThatOpensIsTracedOnce(t *testing.T) {
	f := newDirectFixture(t)
	var mu sync.Mutex
	var lines []string
	f.l.opts.Log = func(format string, args ...any) {
		mu.Lock()
		lines = append(lines, fmt.Sprintf(format, args...))
		mu.Unlock()
	}
	t.Cleanup(f.l.closeAllDirectPeers)
	browser, offer := newTestBrowser(t)
	if err := f.l.directOfferAdmitted("viewer"); err != nil {
		t.Fatal(err)
	}
	answer, err := f.l.answerDirectOffer("viewer", testConnection, false, testConnection, offer)
	if err != nil {
		t.Fatal(err)
	}
	f.l.directAnswerSent("viewer")
	if err := browser.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-browser.opened:
	case <-time.After(10 * time.Second):
		t.Fatal("the channel did not open")
	}
	traced := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := []string{}
		for _, line := range lines {
			if strings.HasPrefix(line, "cloud direct attempt:") {
				out = append(out, line)
			}
		}
		return out
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(traced()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	f.l.closeDirectPeerOf("viewer", "test over")
	mu.Lock()
	var closeLine string
	for _, line := range lines {
		if strings.Contains(line, "cloud terminal carrier=direct closed: test over attempt="+f.c.id+" kind=terminal") {
			closeLine = line
		}
	}
	mu.Unlock()
	if closeLine == "" {
		t.Fatal("closing an open peer did not log the same attempt id")
	}
	for _, want := range []string{" ice=", " pc=", " ice_timeline=", " pair=host/ipv4>"} {
		if !strings.Contains(closeLine, want) {
			t.Errorf("the close trace lacks %q: %s", want, closeLine)
		}
	}
	if strings.Contains(closeLine, "127.0.0.1") {
		t.Errorf("the close trace carries an address: %s", closeLine)
	}
	got := traced()
	if len(got) != 1 {
		t.Fatalf("traced %d lines, want 1: %q", len(got), got)
	}
	for _, want := range []string{"attempt=" + testConnection, "stage=open", "answer=sent", "offer=host/ipv4:", "pair=host/ipv4>"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("the trace lacks %q:\n%s", want, got[0])
		}
	}
	if strings.Contains(got[0], "127.0.0.1") {
		t.Errorf("the trace carries an address:\n%s", got[0])
	}
}

// TestADirectAddressFamilyIsOnlyEverAWordFromItsSet holds the family to its
// four words for the addresses that do not parse as an IP: a host name, an
// IPv6 address with a zone id, a bracketed literal, an IPv4-mapped address,
// an mDNS name. A family that fell back to the address itself would put a
// network location into daemon.log, and the pair line is the place it would
// go, so the pair is checked too.
func TestADirectAddressFamilyIsOnlyEverAWordFromItsSet(t *testing.T) {
	words := map[string]bool{"ipv4": true, "ipv6": true, "mdns": true, "name": true}
	addresses := []string{"router.example.net", "fe80::1%en0", "[2001:db8::1]", "::ffff:198.51.100.7",
		strings.Repeat("c6", 4) + "-0000-4000-8000-000000000001" + ".lo" + "cal", "198.51.100.7", "2001:db8::1", ""}
	for _, address := range addresses {
		if family := directAddressFamily(address); !words[family] {
			t.Errorf("directAddressFamily(%q) = %q, not one of its four words", address, family)
		}
	}
	trace := newDirectTrace("conn-bbbbbbbbbbbbbbbbbbbb", false, time.Now)
	trace.selectedPair(&webrtc.ICECandidatePair{
		Local:  &webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeHost, Address: "router.example.net", Port: 61003},
		Remote: &webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeHost, Address: "fe80::1%en0", Port: 61004},
	})
	line := trace.line(false, true)
	for _, secret := range []string{"router.example.net", "fe80", "en0", "61003", "61004"} {
		if strings.Contains(line, secret) {
			t.Errorf("the trace carries %q:\n%s", secret, line)
		}
	}
}
