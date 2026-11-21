package cloud

// The content-free trace of one direct attempt (docs/cloud-terminal-wire.md,
// Direct attempt trace).
//
// Until this file, an attempt that did not open left one line, "the data
// channel did not open in time", whichever of a dozen things had gone wrong:
// the offer may have carried nothing this machine would send to, the checks
// may have failed, or ICE may have connected and DTLS or SCTP never finished.
// This keeps, per attempt, only what tells those apart — candidate types and
// address families as counts, the ICE states with their times, the selected
// pair's types and families, and the stage it stopped at — and writes it as
// one line when the attempt ends. The attempt id is the connection id the
// browser chose for the offer, so the browser's own summary of the same
// attempt (`cloud.direct.attempt`, a viewer event) lands beside it.
//
// It never holds an address, a port, SDP text, an ICE username fragment or
// password, or a key: what is parsed from a candidate line is its protocol,
// its type and the family of its address, and the line itself is dropped.

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// directWhyOpenTimeout is the reason a peer is closed when its data channel
// has not opened within CloudTerminalDirectNegotiateSecondsLimit.
const directWhyOpenTimeout = "the data channel did not open in time"

// The stages an attempt can stop at, in the order directTraceStage tests them.
const (
	directStageOpen               = "open"
	directStageNoAnswer           = "no_answer"
	directStageAnswerNotSent      = "answer_not_sent"
	directStageConnectedNoChannel = "connected_no_channel"
	directStageNoRemoteCandidates = "no_remote_candidates"
	directStageChecksFailed       = "checks_failed"
	directStageTimedOut           = "timed_out"
	directStageClosedEarly        = "closed_early"
)

// directTrace is one attempt's facts. Every field is a count, a state name, a
// duration or a fixed word; nothing in it came from a peer verbatim.
type directTrace struct {
	attempt string
	kind    string
	start   time.Time
	now     func() time.Time

	mu            sync.Mutex
	offerKept     map[string]int // "type/family"
	offerDropped  map[string]int // "reason:type/family"
	answerKept    map[string]int
	answerDropped map[string]int
	gather        string // "complete" | "timeout"
	answered      bool   // an answer was made
	sent          bool   // the answer left this machine
	refused       string // the code an attempt was refused with before it had an answer
	// ice holds each ICE connection state once, at the first time it was
	// reached: there are seven states, so the list is bounded by the enum.
	ice     []directStateAt
	pair    string
	opened  bool
	timeout bool
	logged  bool
}

type directStateAt struct {
	state string
	at    time.Duration
}

func newDirectTrace(attempt string, carrier bool, now func() time.Time) *directTrace {
	if now == nil {
		now = time.Now
	}
	kind := "terminal"
	if carrier {
		kind = "carrier"
	}
	return &directTrace{attempt: attempt, kind: kind, start: now(), now: now,
		offerKept: map[string]int{}, offerDropped: map[string]int{},
		answerKept: map[string]int{}, answerDropped: map[string]int{}}
}

// directCandidateShape reads the protocol, type and address family of one
// `a=candidate:` line's fields (without the prefix). The address itself is
// read only to name its family.
func directCandidateShape(fields []string) (protocol, shape string) {
	if len(fields) < 5 {
		return "", "malformed/unknown"
	}
	return strings.ToLower(fields[2]), directCandidateType(directCandidateTyp(fields)) + "/" + directAddressFamily(fields[4])
}

func directCandidateType(typ string) string {
	switch typ {
	case "host", "srflx", "prflx", "relay":
		return typ
	}
	return "unknown"
}

// directAddressFamily names what an address is without saying which one:
// ipv4, ipv6, mdns (a `.local` name) or name (any other name).
func directAddressFamily(address string) string {
	if ip := net.ParseIP(address); ip != nil {
		if ip.To4() != nil {
			return "ipv4"
		}
		return "ipv6"
	}
	if strings.HasSuffix(strings.ToLower(address), ".local") {
		return "mdns"
	}
	return "name"
}

func (t *directTrace) offerCandidate(shape, dropped string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if dropped == "" {
		t.offerKept[shape]++
	} else {
		t.offerDropped[dropped+":"+shape]++
	}
}

func (t *directTrace) answerCandidate(shape, dropped string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if dropped == "" {
		t.answerKept[shape]++
	} else {
		t.answerDropped[dropped+":"+shape]++
	}
}

func (t *directTrace) gathered(complete bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.gather = "timeout"
	if complete {
		t.gather = "complete"
	}
}

func (t *directTrace) answerMade() {
	t.mu.Lock()
	t.answered = true
	t.mu.Unlock()
}

func (t *directTrace) answerSent() {
	t.mu.Lock()
	t.sent = true
	t.mu.Unlock()
}

func (t *directTrace) iceState(state string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, seen := range t.ice {
		if seen.state == state {
			return
		}
	}
	t.ice = append(t.ice, directStateAt{state: state, at: t.now().Sub(t.start)})
}

func (t *directTrace) selectedPair(pair *webrtc.ICECandidatePair) {
	if pair == nil || pair.Local == nil || pair.Remote == nil {
		return
	}
	t.mu.Lock()
	t.pair = pair.Local.Typ.String() + "/" + directAddressFamily(pair.Local.Address) + ">" +
		pair.Remote.Typ.String() + "/" + directAddressFamily(pair.Remote.Address)
	t.mu.Unlock()
}

func (t *directTrace) closeFacts() (iceTimeline, pair string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return directICETimeline(t.ice), orDash(t.pair)
}

func directICETimeline(states []directStateAt) string {
	timeline := make([]string, 0, len(states))
	for _, state := range states {
		timeline = append(timeline, fmt.Sprintf("%s@%dms", state.state, state.at.Milliseconds()))
	}
	return orDash(strings.Join(timeline, ","))
}

// directTraceStage is where an attempt stopped. Opening wins; then the
// answer; then what ICE reached; then why ICE had nothing to reach.
func directTraceStage(opened, answered, sent bool, remoteKept int, ice []string, timeout bool) string {
	reached := func(states ...string) bool {
		for _, s := range ice {
			for _, want := range states {
				if s == want {
					return true
				}
			}
		}
		return false
	}
	switch {
	case opened:
		return directStageOpen
	case !answered:
		return directStageNoAnswer
	case !sent:
		return directStageAnswerNotSent
	case reached("connected", "completed"):
		return directStageConnectedNoChannel
	case remoteKept == 0:
		return directStageNoRemoteCandidates
	case reached("failed"):
		return directStageChecksFailed
	case timeout:
		return directStageTimedOut
	}
	return directStageClosedEarly
}

// line is the attempt's one log line. It is written once: the second call,
// from a close that follows an open, answers "".
func (t *directTrace) line(opened, timeout bool) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.logged {
		return ""
	}
	t.logged = true
	t.opened = t.opened || opened
	t.timeout = t.timeout || timeout
	states := make([]string, 0, len(t.ice))
	for _, s := range t.ice {
		states = append(states, s.state)
	}
	remote := 0
	for _, n := range t.offerKept {
		remote += n
	}
	stage := directTraceStage(t.opened, t.answered, t.sent, remote, states, t.timeout)
	answer := "none"
	switch {
	case t.sent:
		answer = "sent"
	case t.answered:
		answer = "made"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "cloud direct attempt: attempt=%s kind=%s stage=%s ms=%d answer=%s",
		logToken(t.attempt), t.kind, stage, t.now().Sub(t.start).Milliseconds(), answer)
	if t.refused != "" {
		fmt.Fprintf(&b, " refused=%s", logToken(t.refused))
	}
	fmt.Fprintf(&b, " offer=%s offer_dropped=%s gather=%s answer_cands=%s answer_dropped=%s ice=%s pair=%s",
		directCounts(t.offerKept), directCounts(t.offerDropped), orDash(t.gather),
		directCounts(t.answerKept), directCounts(t.answerDropped), directICETimeline(t.ice), orDash(t.pair))
	return b.String()
}

// directCounts is a count map as `key:n,key:n` in key order, or `-`.
func directCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", k, counts[k]))
	}
	return orDash(strings.Join(parts, ","))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// logToken keeps an id one word long and short; the attempt id is the
// browser's connection id, which is already a 22-character token.
func logToken(s string) string {
	if len(s) > 64 {
		s = s[:64]
	}
	return strings.Map(func(r rune) rune {
		if r <= ' ' || r == 0x7f || r == '=' {
			return '_'
		}
		return r
	}, orDash(s))
}

// logDirectTrace writes the attempt's line once, when it ends.
func (l *Link) logDirectTrace(t *directTrace, opened, timeout bool) {
	if t == nil {
		return
	}
	if line := t.line(opened, timeout); line != "" {
		l.logf("%s", line)
	}
}

// refuseDirectTrace ends an attempt that never had an answer to send.
func (l *Link) refuseDirectTrace(t *directTrace, err error) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.refused = "failed"
	if code, known := terminal.CodeOf(err); known {
		t.refused = string(code)
	}
	t.mu.Unlock()
	l.logDirectTrace(t, false, false)
}
