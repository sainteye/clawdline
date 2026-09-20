package auth

import (
	"context"
	"crypto/pbkdf2"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type memStore struct {
	state  State
	token  string
	audit  []string
	fail   bool
	saves  int
	loaded bool
}

func (m *memStore) Load() (State, error) { m.loaded = true; return m.state, nil }
func (m *memStore) Save(s State) error {
	if m.fail {
		return errors.New("disk full")
	}
	m.saves++
	m.state = s
	return nil
}
func (m *memStore) Audit(event string, fields map[string]string) {
	line := event
	for k, v := range fields {
		line += " " + k + "=" + v
	}
	m.audit = append(m.audit, line)
}
func (m *memStore) ReadLocalToken() (string, error) {
	if m.token == "" {
		return "", os.ErrNotExist
	}
	return m.token, nil
}
func (m *memStore) WriteLocalToken(t string) error { m.token = t; return nil }

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newAuthority(t *testing.T, store *memStore) (*Authority, *clock) {
	t.Helper()
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a, err := New(store, Options{Now: c.now})
	if err != nil {
		t.Fatal(err)
	}
	return a, c
}

// The code a watcher is told, which is the only way anything outside the
// authority learns it.
func codeFor(t *testing.T, a *Authority, id string) string {
	t.Helper()
	ch, cancel := a.Watch()
	defer cancel()
	select {
	case n := <-ch:
		if n.ID != id {
			t.Fatalf("watcher saw pairing %s, want %s", n.ID, id)
		}
		return n.Code
	default:
		t.Fatal("no pairing open for a new watcher")
		return ""
	}
}

func wrong(code string) string {
	if code == "000000" {
		return "000001"
	}
	return "000000"
}

func TestFiveGuessesThenGone(t *testing.T) {
	store := &memStore{}
	a, _ := newAuthority(t, store)
	p, err := a.BeginPairing("  Phone on the sofa  ")
	if err != nil {
		t.Fatal(err)
	}
	code := codeFor(t, a, p.ID)
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		t.Fatalf("code %q is not six digits", code)
	}
	for want := 4; want >= 1; want-- {
		r, err := a.ConfirmPairing(p.ID, wrong(code))
		if err != nil || r.Kind != WrongCode || r.Left != want {
			t.Fatalf("guess with %d left: %+v %v", want, r, err)
		}
	}
	// The fifth wrong guess ends it, and the sixth — even the right code —
	// finds nothing.
	if r, _ := a.ConfirmPairing(p.ID, wrong(code)); r.Kind != Expired {
		t.Fatalf("fifth wrong guess: %+v", r)
	}
	if r, _ := a.ConfirmPairing(p.ID, code); r.Kind != Expired {
		t.Fatalf("sixth guess: %+v", r)
	}
	if a.IsConfigured() {
		t.Fatal("a locked pairing made a device")
	}
	joined := strings.Join(store.audit, "\n")
	if !strings.Contains(joined, "pair.locked device=Phone on the sofa") {
		t.Fatalf("audit: %s", joined)
	}
	if strings.Contains(joined, code) {
		t.Fatal("the code reached the audit")
	}
}

func TestRightCodePairsReadOnly(t *testing.T) {
	store := &memStore{}
	a, _ := newAuthority(t, store)
	p, _ := a.BeginPairing("")
	code := codeFor(t, a, p.ID)
	if r, _ := a.ConfirmPairing(p.ID, wrong(code)); r.Kind != WrongCode {
		t.Fatalf("wrong: %+v", r)
	}
	r, err := a.ConfirmPairing(p.ID, " "+code+"\t")
	if err != nil || r.Kind != Paired || len(r.Token) != 43 {
		t.Fatalf("right code: %+v %v", r, err)
	}
	v := a.Verify(r.Token)
	if !v.Allowed || v.Local || v.Caps.Has(Send) || !v.Caps.Has(Read) {
		t.Fatalf("verdict %+v", v)
	}
	devices := a.Devices()
	if len(devices) != 1 || devices[0].Name != DefaultName {
		t.Fatalf("devices %+v", devices)
	}
	for _, d := range store.state.Devices {
		if d.Hash == r.Token || !ConstantTimeEquals(d.Hash, Hash(r.Token)) {
			t.Fatal("the store holds something other than the token's hash")
		}
	}
	if joined := strings.Join(store.audit, "\n"); strings.Contains(joined, r.Token) || strings.Contains(joined, code) {
		t.Fatalf("a secret reached the audit: %s", joined)
	}
	// Used once, gone.
	if again, _ := a.ConfirmPairing(p.ID, code); again.Kind != Expired {
		t.Fatalf("replay: %+v", again)
	}
	if !a.IsConfigured() {
		t.Fatal("a paired device does not count as configured")
	}
}

func TestExpiryAndReplacement(t *testing.T) {
	a, c := newAuthority(t, &memStore{})
	first, _ := a.BeginPairing("one")
	code := codeFor(t, a, first.ID)
	c.t = c.t.Add(PairingLifetime)
	if r, _ := a.ConfirmPairing(first.ID, code); r.Kind != Expired {
		t.Fatalf("lapsed: %+v", r)
	}

	older, _ := a.BeginPairing("two")
	olderCode := codeFor(t, a, older.ID)
	newer, _ := a.BeginPairing("three")
	if r, _ := a.ConfirmPairing(older.ID, olderCode); r.Kind != Expired {
		t.Fatalf("replaced pairing still open: %+v", r)
	}
	if r, _ := a.ConfirmPairing(newer.ID, codeFor(t, a, newer.ID)); r.Kind != Paired {
		t.Fatalf("newer: %+v", r)
	}
}

func TestRateLimit(t *testing.T) {
	a, c := newAuthority(t, &memStore{})
	for i := 0; i < PairingRequests; i++ {
		if _, err := a.BeginPairing("x"); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		c.t = c.t.Add(time.Minute)
	}
	if _, err := a.BeginPairing("x"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("fourth request: %v", err)
	}
	// The window rolls from the first request, not from the refusal.
	c.t = c.t.Add(PairingWindow - 3*time.Minute)
	if _, err := a.BeginPairing("x"); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

func TestLocalTokenSurvivesAndRotates(t *testing.T) {
	store := &memStore{}
	a, _ := newAuthority(t, store)
	token, err := a.LocalToken()
	if err != nil {
		t.Fatal(err)
	}
	if v := a.Verify(token); !v.Allowed || !v.Local || !v.Caps.Has(Admin) || !v.Caps.Has(Send) {
		t.Fatalf("local verdict %+v", v)
	}
	if a.IsConfigured() {
		t.Fatal("the local token counts toward the tunnel interlock")
	}

	// A restart reads the same token back rather than minting another.
	b, _ := newAuthority(t, store)
	again, err := b.LocalToken()
	if err != nil || again != token {
		t.Fatalf("restart minted a new token: %v", err)
	}

	// A file that no longer matches is the one reason to start again.
	store.token = "somebody-else"
	c, _ := newAuthority(t, store)
	rotated, err := c.LocalToken()
	if err != nil || rotated == token || store.token != rotated {
		t.Fatalf("mismatch did not rotate: %v", err)
	}
	if c.Verify(token).Allowed {
		t.Fatal("the old local token still verifies")
	}
	locals := 0
	for _, d := range store.state.Devices {
		if d.Local {
			locals++
		}
	}
	if locals != 1 {
		t.Fatalf("%d local devices", locals)
	}
}

func TestRevokeAndCaps(t *testing.T) {
	store := &memStore{}
	a, _ := newAuthority(t, store)
	local, _ := a.LocalToken()
	id, token, err := a.AddDevice("Browser on this machine", NewCaps(Read), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetCapabilities(id, Caps{Admin}); !errors.Is(err, ErrBadCaps) {
		t.Fatalf("admin grant: %v", err)
	}
	caps, err := a.SetCapabilities(id, Caps{Send})
	if err != nil || caps.Joined() != "read+send" || !a.Verify(token).Caps.Has(Send) {
		t.Fatalf("send grant: %v %v", caps, err)
	}
	localID := a.Verify(local).Device
	if err := a.Revoke(localID); !errors.Is(err, ErrLocalDevice) {
		t.Fatalf("revoking the local device: %v", err)
	}
	if _, err := a.SetCapabilities(localID, Caps{Read}); !errors.Is(err, ErrLocalDevice) {
		t.Fatalf("narrowing the local device: %v", err)
	}
	if err := a.Revoke(id); err != nil {
		t.Fatal(err)
	}
	if a.Verify(token).Allowed {
		t.Fatal("a revoked token still verifies")
	}
	if err := a.Revoke(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}

	_, other, _ := a.AddDevice("phone", NewCaps(Read), false)
	n, err := a.RevokeAll()
	if err != nil || n != 1 || a.Verify(other).Allowed || !a.Verify(local).Allowed {
		t.Fatalf("revoke all: n=%d err=%v", n, err)
	}
}

func TestFailedSaveChangesNothing(t *testing.T) {
	store := &memStore{}
	a, _ := newAuthority(t, store)
	p, _ := a.BeginPairing("x")
	code := codeFor(t, a, p.ID)
	store.fail = true
	if _, err := a.ConfirmPairing(p.ID, code); err == nil {
		t.Fatal("a failed save was reported as paired")
	}
	if a.IsConfigured() {
		t.Fatal("memory holds a device the disk does not")
	}
	// The person typed the right code; the failure was this machine's, so the
	// pairing is still there to finish.
	store.fail = false
	if r, err := a.ConfirmPairing(p.ID, code); err != nil || r.Kind != Paired {
		t.Fatalf("retry: %+v %v", r, err)
	}
}

func TestPassword(t *testing.T) {
	store := &memStore{}
	a, _ := newAuthority(t, store)
	ctx := context.Background()
	if _, ok, _ := a.Exchange(ctx, "anything", "x"); ok {
		t.Fatal("no password set, and one was accepted")
	}
	if err := a.SetPassword("correct horse"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := a.Exchange(ctx, "wrong", "laptop"); ok {
		t.Fatal("wrong password accepted")
	}
	token, ok, err := a.Exchange(ctx, "correct horse", "laptop")
	if err != nil || !ok {
		t.Fatalf("right password: %v", err)
	}
	if v := a.Verify(token); !v.Allowed || v.Caps.Has(Send) {
		t.Fatalf("password device %+v", v)
	}
	joined := strings.Join(store.audit, "\n")
	if strings.Contains(joined, "correct horse") || strings.Contains(joined, "wrong") {
		t.Fatalf("a password reached the audit: %s", joined)
	}
	if !strings.Contains(joined, "password.fail device=laptop") {
		t.Fatalf("audit: %s", joined)
	}
}

// A new pairing does not hand out new guesses: the wrong codes typed into the
// one it replaced still count, and five of them close pairing for a day,
// through a revoke-all, whichever pairing they went to.
func TestWrongCodesOutliveReplacement(t *testing.T) {
	store := &memStore{}
	a, c := newAuthority(t, store)
	first, _ := a.BeginPairing("first")
	firstCode := codeFor(t, a, first.ID)
	for want := 4; want >= 2; want-- {
		if r, _ := a.ConfirmPairing(first.ID, wrong(firstCode)); r.Kind != WrongCode || r.Left != want {
			t.Fatalf("first pairing, %d left: %+v", want, r)
		}
	}
	second, err := a.BeginPairing("second")
	if err != nil {
		t.Fatal(err)
	}
	code := codeFor(t, a, second.ID)
	if r, _ := a.ConfirmPairing(second.ID, wrong(code)); r.Kind != WrongCode || r.Left != 1 {
		t.Fatalf("the replacement started a fresh count: %+v", r)
	}
	if r, _ := a.ConfirmPairing(second.ID, wrong(code)); r.Kind != Expired {
		t.Fatalf("fifth wrong code overall: %+v", r)
	}
	if r, _ := a.ConfirmPairing(second.ID, code); r.Kind != Expired {
		t.Fatalf("right code after the fifth: %+v", r)
	}
	if _, err := a.BeginPairing("third"); !errors.Is(err, ErrPairingLocked) {
		t.Fatalf("a request while closed: %v", err)
	}
	if _, err := a.RevokeAll(); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(PairingGuessWindow - time.Minute)
	if _, err := a.BeginPairing("fourth"); !errors.Is(err, ErrPairingLocked) {
		t.Fatalf("closed for less than a day: %v", err)
	}
	c.t = c.t.Add(time.Minute)
	next, err := a.BeginPairing("fifth")
	if err != nil {
		t.Fatalf("a day after the first wrong code: %v", err)
	}
	if r, _ := a.ConfirmPairing(next.ID, codeFor(t, a, next.ID)); r.Kind != Paired {
		t.Fatalf("pairing after the window: %+v", r)
	}
	if !strings.Contains(strings.Join(store.audit, "\n"), "pair.locked device=second") {
		t.Fatalf("audit: %v", store.audit)
	}
}

// fastPassword is a stored password at the lowest cost a store may carry, so
// a test can check it many times.
func fastPassword(t *testing.T, plain string) *Password {
	t.Helper()
	salt := []byte("0123456789abcdef")
	hash, err := pbkdf2.Key(sha256.New, plain, salt, MinPasswordIterations, PasswordHashBytes)
	if err != nil {
		t.Fatal(err)
	}
	return &Password{Hash: hash, Salt: salt, Iterations: MinPasswordIterations}
}

func countAudit(store *memStore, event string) int {
	n := 0
	for _, line := range store.audit {
		if line == event || strings.HasPrefix(line, event+" ") {
			n++
		}
	}
	return n
}

// Ten wrong passwords close the door for everybody, the right password
// included, and a refused attempt hashes and writes nothing.
func TestPasswordBudget(t *testing.T) {
	store := &memStore{state: State{Password: fastPassword(t, "right")}}
	a, c := newAuthority(t, store)
	ctx := context.Background()
	for i := 0; i < PasswordFailures; i++ {
		if _, ok, err := a.Exchange(ctx, "wrong", "guesser"); ok || err != nil {
			t.Fatalf("wrong password %d: ok=%v err=%v", i+1, ok, err)
		}
	}
	for i := 0; i < 3; i++ {
		if _, ok, err := a.Exchange(ctx, "right", "owner"); ok || !errors.Is(err, ErrRateLimited) {
			t.Fatalf("past the budget: ok=%v err=%v", ok, err)
		}
	}
	if n := countAudit(store, "password.fail"); n != PasswordFailures {
		t.Fatalf("%d password.fail lines, want %d", n, PasswordFailures)
	}
	if n := countAudit(store, "password.locked"); n != 1 {
		t.Fatalf("%d password.locked lines, want 1", n)
	}
	c.t = c.t.Add(PasswordWindow)
	if _, ok, err := a.Exchange(ctx, "right", "owner"); !ok || err != nil {
		t.Fatalf("a window later: ok=%v err=%v", ok, err)
	}
}

func checksUnderWay(a *Authority) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.passwordChecks
}

// Checks wait for one another, the ones waiting count against the budget so
// the queue cannot grow past it, and a caller who leaves while waiting is
// neither hashed nor counted.
func TestPasswordChecksWaitBoundedAndGiveUp(t *testing.T) {
	store := &memStore{state: State{Password: fastPassword(t, "right")}}
	a, _ := newAuthority(t, store)
	a.derive <- struct{}{} // somebody else's check is running

	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, PasswordFailures)
	for i := 0; i < PasswordFailures; i++ {
		go func() {
			_, _, err := a.Exchange(ctx, "wrong", "waiting")
			errs <- err
		}()
	}
	deadline := time.Now().Add(5 * time.Second)
	for checksUnderWay(a) < PasswordFailures {
		if time.Now().After(deadline) {
			t.Fatalf("%d checks admitted, want %d", checksUnderWay(a), PasswordFailures)
		}
		time.Sleep(time.Millisecond)
	}
	if _, _, err := a.Exchange(context.Background(), "right", "owner"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("a check past the waiting ones: %v", err)
	}
	cancel()
	for i := 0; i < PasswordFailures; i++ {
		if err := <-errs; !errors.Is(err, context.Canceled) {
			t.Fatalf("a waiter whose caller left: %v", err)
		}
	}
	<-a.derive
	if n := checksUnderWay(a); n != 0 {
		t.Fatalf("%d checks still counted", n)
	}
	if n := countAudit(store, "password.fail"); n != 0 {
		t.Fatalf("callers who left were audited %d times", n)
	}
	if _, ok, err := a.Exchange(context.Background(), "right", "owner"); !ok || err != nil {
		t.Fatalf("the budget after they left: ok=%v err=%v", ok, err)
	}
}

// Whatever name a caller sends, what is audited and stored is DeviceName's.
func TestPasswordNameIsCut(t *testing.T) {
	store := &memStore{state: State{Password: fastPassword(t, "right")}}
	a, _ := newAuthority(t, store)
	long := strings.Repeat("N", 60_000)
	if _, ok, _ := a.Exchange(context.Background(), "wrong", long); ok {
		t.Fatal("wrong password accepted")
	}
	if len(store.audit) != 1 || utf8.RuneCountInString(store.audit[0]) > len("password.fail device=")+nameLimit {
		t.Fatalf("audit line of %d bytes", len(store.audit[0]))
	}
	if _, ok, _ := a.Exchange(context.Background(), "right", long); !ok {
		t.Fatal("right password refused")
	}
	for _, d := range store.state.Devices {
		if utf8.RuneCountInString(d.Name) > nameLimit {
			t.Fatalf("stored a %d-character name", utf8.RuneCountInString(d.Name))
		}
	}
}

// A state that reads but does not make sense is refused before anything acts
// on it.
func TestNewRefusesInvalidState(t *testing.T) {
	good := func(id string, local bool) Device {
		return Device{ID: id, Name: id, Hash: Hash(id), Caps: NewCaps(Read), Approved: true, Local: local}
	}
	pw := fastPassword(t, "x")
	cases := map[string]State{
		"empty id":        {Devices: []Device{good("", false)}},
		"duplicate id":    {Devices: []Device{good("a", false), good("a", false)}},
		"short digest":    {Devices: []Device{{ID: "a", Hash: "abc", Approved: true}}},
		"uppercase":       {Devices: []Device{{ID: "a", Hash: strings.ToUpper(Hash("a")), Approved: true}}},
		"shared digest":   {Devices: []Device{good("a", false), {ID: "b", Hash: Hash("a"), Approved: true}}},
		"unknown cap":     {Devices: []Device{{ID: "a", Hash: Hash("a"), Caps: Caps{"root"}, Approved: true}}},
		"two locals":      {Devices: []Device{good("a", true), good("b", true)}},
		"local pending":   {Devices: []Device{{ID: "a", Hash: Hash("a"), Caps: NewCaps(Read), Local: true}}},
		"short salt":      {Password: &Password{Hash: pw.Hash, Salt: pw.Salt[:8], Iterations: pw.Iterations}},
		"short hash":      {Password: &Password{Hash: pw.Hash[:16], Salt: pw.Salt, Iterations: pw.Iterations}},
		"zero iterations": {Password: &Password{Hash: pw.Hash, Salt: pw.Salt, Iterations: 0}},
		"too many":        {Password: &Password{Hash: pw.Hash, Salt: pw.Salt, Iterations: MaxPasswordIterations + 1}},
		"negative":        {Password: &Password{Hash: pw.Hash, Salt: pw.Salt, Iterations: -1}},
	}
	for name, state := range cases {
		store := &memStore{state: state}
		if _, err := New(store, Options{}); !errors.Is(err, ErrInvalidState) {
			t.Errorf("%s: %v", name, err)
		}
		if store.saves != 0 {
			t.Errorf("%s: saved over a state it refused", name)
		}
	}
	ok := State{Devices: []Device{good("a", true), good("b", false)}, Password: pw}
	if _, err := New(&memStore{state: ok}, Options{}); err != nil {
		t.Fatalf("a sound state: %v", err)
	}
}
