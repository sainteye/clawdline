package cloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reads++
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{row}})
	}))
	defer server.Close()
	l := &Link{roster: adaptercloud.NewRoster(server.URL, "credential", time.Now)}
	check := func(wantSender, wantCap bool) {
		t.Helper()
		a := l.transcriptAuthority(context.Background(), "viewer", public)
		if a.RosterAllowsSender != wantSender || a.ReadTranscriptAllows != wantCap || !a.RosterReadable {
			t.Fatalf("roster result: %+v", a)
		}
	}
	check(true, true)
	row.Caps = nil
	check(true, false)
	row.Caps = []string{"read_transcript"}
	row.PublicKey = base64.StdEncoding.EncodeToString(other)
	check(false, false)
	row.PublicKey = base64.StdEncoding.EncodeToString(public)
	revoked := "2026-01-01T00:00:00Z"
	row.RevokedAt = &revoked
	check(false, false)
	if reads != 4 {
		t.Fatalf("fresh roster reads=%d, want four", reads)
	}
}
