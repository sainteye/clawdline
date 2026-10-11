package cloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
)

func TestContentAuthorityRefreshesCapabilityKeyAndRevocation(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	row := adaptercloud.RosterDevice{ID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(public), Caps: []string{"read_transcript"}}
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		reads++
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{row}})
	}))
	defer server.Close()
	l := &Link{opts: LinkOptions{Now: clock}, roster: adaptercloud.NewRoster(server.URL, "credential", clock)}
	check := func(wantSender, wantCap bool) {
		t.Helper()
		a := l.transcriptAuthority(context.Background(), "viewer", public)
		if a.RosterAllowsSender != wantSender || a.ReadTranscriptAllows != wantCap || !a.RosterReadable {
			t.Fatalf("roster result: %+v", a)
		}
	}
	// Each change is seen on the first read after the window, which is what
	// makes a withdrawn capability, a changed key and a revocation all arrive.
	check(true, true)
	row.Caps = nil
	advance(CloudTerminalRosterRefreshLimit * time.Second)
	check(true, false)
	row.Caps = []string{"read_transcript"}
	row.PublicKey = base64.StdEncoding.EncodeToString(other)
	advance(CloudTerminalRosterRefreshLimit * time.Second)
	check(false, false)
	row.PublicKey = base64.StdEncoding.EncodeToString(public)
	revoked := "2026-01-01T00:00:00Z"
	row.RevokedAt = &revoked
	advance(CloudTerminalRosterRefreshLimit * time.Second)
	check(false, false)
	mu.Lock()
	got := reads
	mu.Unlock()
	if got != 4 {
		t.Fatalf("fresh roster reads=%d, want four", got)
	}
}

// A read used to carry a GET /v1/devices of its own, which was most of what a
// Session read cost once the request itself took the direct carrier. The
// person chose the terminal lane's rule for reads too (2026-10-11): a roster
// read that succeeded within CloudTerminalRosterRefreshLimit stands behind the
// answer, and one read serves both lanes.
func TestASessionReadInsideTheRosterWindowCostsNoDeviceRead(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	row := adaptercloud.RosterDevice{ID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(public), Caps: []string{"read_transcript"}}
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		reads++
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{row}})
	}))
	defer server.Close()
	l := &Link{opts: LinkOptions{Now: clock}, roster: adaptercloud.NewRoster(server.URL, "credential", clock)}
	read := func() {
		t.Helper()
		a := l.transcriptAuthority(context.Background(), "viewer", public)
		if !a.RosterAllowsSender || !a.ReadTranscriptAllows {
			t.Fatalf("roster result: %+v", a)
		}
	}
	read()
	mu.Lock()
	first := reads
	mu.Unlock()
	if first != 1 {
		t.Fatalf("the first read made %d device reads, want one", first)
	}
	// Five more reads inside the window, which is what reading a conversation
	// and its to-dos in one screenful looks like.
	for i := 0; i < 5; i++ {
		mu.Lock()
		now = now.Add(300 * time.Millisecond)
		mu.Unlock()
		read()
	}
	mu.Lock()
	within := reads
	mu.Unlock()
	if within != 1 {
		t.Fatalf("six reads within %v made %d device reads, want one", CloudTerminalRosterRefreshLimit*time.Second, within)
	}
	// And a terminal frame in the same window reuses that read rather than
	// starting one of its own.
	if !l.rosterFresh() {
		t.Fatal("the terminal lane did not trust the read a Session read had just made")
	}
	mu.Lock()
	shared := reads
	mu.Unlock()
	if shared != 1 {
		t.Fatalf("the terminal lane made the device read again: %d reads", shared)
	}
	// Past the window the next read pays for a fresh one, which is what keeps
	// a revocation arriving.
	mu.Lock()
	now = now.Add(CloudTerminalRosterRefreshLimit * time.Second)
	mu.Unlock()
	read()
	mu.Lock()
	after := reads
	mu.Unlock()
	if after != 2 {
		t.Fatalf("a read past the window made %d device reads in all, want two", after)
	}
}
