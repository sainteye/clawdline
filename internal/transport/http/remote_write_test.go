package http

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

// The settings window's 讓配對過的裝置寫進 session is `remote_write`. On, it
// lets every paired device send, as the Swift app's switch did; off, a device
// keeps only what it was granted itself — a `clawdline open --send` browser
// still sends, a device paired with a code only reads. It is read at every
// request, so turning it off takes effect on the next one.
func TestRemoteWriteLetsPairedDevicesSend(t *testing.T) {
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(t.TempDir(), "swift"))
	dir := filepath.Join(t.TempDir(), "next")
	g := openGate(config.Config{Dir: dir, Port: 7757})
	if g.err != nil {
		t.Fatal(g.err)
	}
	local, err := g.auth.LocalToken()
	if err != nil {
		t.Fatal(err)
	}
	_, sender, err := g.auth.AddDevice("sender", auth.NewCaps(auth.Read, auth.Send), false)
	if err != nil {
		t.Fatal(err)
	}
	_, reader, err := g.auth.AddDevice("reader", auth.NewCaps(auth.Read), false)
	if err != nil {
		t.Fatal(err)
	}
	// Behind the gate, a route that answers the way every session action
	// decides (actions.go): whether this request may send.
	h := g.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := accessOf(r)
		switch {
		case a.verdict.Caps.Has(auth.Admin) && !a.verdict.Local:
			_, _ = io.WriteString(w, "admin")
		case maySend(r):
			_, _ = io.WriteString(w, "send")
		default:
			_, _ = io.WriteString(w, "read")
		}
	}))
	file := nextconfig.Open(dir)
	set := func(on bool) {
		t.Helper()
		if _, err := file.Set(map[string]any{"remote_write": on}); err != nil {
			t.Fatal(err)
		}
	}
	const board = `{"operation":"note","requestId":"r","expectedRevision":0,"actor":"t"}`
	ask := func(token string) string {
		rec := call{method: http.MethodGet, path: "/v1/sessions",
			headers: map[string]string{"Authorization": "Bearer " + token}}.do(h)
		return rec.Body.String()
	}
	write := func(token string) int {
		return call{path: "/v1/board", body: board, headers: map[string]string{
			"Authorization": "Bearer " + token, "Content-Type": "application/json"}}.do(h).Code
	}
	check := func(when string, wantReader string, wantBoard int) {
		t.Helper()
		if got := ask(reader); got != wantReader {
			t.Errorf("%s: a device paired with a code is %q, want %q", when, got, wantReader)
		}
		if got := write(reader); got != wantBoard {
			t.Errorf("%s: its board write answered %d, want %d", when, got, wantBoard)
		}
		if got := ask(sender); got != "send" {
			t.Errorf("%s: a device opened with --send is %q, want send", when, got)
		}
		if got := ask(local); got != "send" {
			t.Errorf("%s: this machine's own token is %q, want send", when, got)
		}
	}

	check("no config.json", "read", http.StatusForbidden)
	set(true)
	check("remote_write on", "send", http.StatusOK)
	set(false)
	check("remote_write off again", "read", http.StatusForbidden)

	// A file that cannot be read is not a yes.
	set(true)
	if err := os.WriteFile(file.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	check("config.json unreadable", "read", http.StatusForbidden)
}
