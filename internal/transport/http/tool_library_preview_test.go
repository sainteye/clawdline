package http

import (
	"encoding/json"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in manual UI fixture: never discovers providers or forwards unknown routes.
// The only real writes go to a temporary SQLite store and a synthetic terminal.
func TestToolLibraryBrowserFixture(t *testing.T) {
	if os.Getenv("CLAWDLINE_TOOL_PREVIEW") != "1" {
		t.Skip("manual browser fixture")
	}
	f := newSquadFixture(t)
	p := &pane{s: session.Session{ID: "%tools", Backend: session.BackendTmux, Assistant: session.AssistantClaude, ConversationID: "00000000-0000-4000-8000-000000000777", CWD: t.TempDir(), State: session.StateIdle}}
	hosts := []ports.TerminalHost{p}
	f.s.terminals = hosts
	f.s.inventory = app.Inventory{Terminals: hosts, Screen: p}
	h := f.s.Handler()
	listener, err := net.Listen("tcp", "127.0.0.1:8877")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/fixture-result":
			json.NewEncoder(w).Encode(p.done())
		case r.URL.Path == "/v1/fixture-stop":
			w.Write([]byte(`{}`))
			select {
			case <-done:
			default:
				close(done)
			}
		case strings.HasPrefix(r.URL.Path, "/v1/tool-library") || strings.HasSuffix(r.URL.Path, "/send"):
			r.Header.Set("Authorization", "Bearer "+f.local)
			h.ServeHTTP(w, r)
		case r.URL.Path == "/v1/strings":
			lang := r.URL.Query().Get("lang")
			if lang != "zh-Hant" {
				lang = "en"
			}
			b, e := os.ReadFile(filepath.Join("../../../web/console/public/catalogs", lang+".json"))
			if e != nil {
				t.Error(e)
				w.WriteHeader(500)
				return
			}
			w.Write(b)
		case strings.HasSuffix(r.URL.Path, "/skills"):
			w.Write([]byte(`{"skills":[]}`))
		case strings.HasSuffix(r.URL.Path, "/outputs"):
			w.Write([]byte(`{"files":[]}`))
		case r.URL.Path == "/v1/places":
			json.NewEncoder(w).Encode(map[string]any{"at": time.Now().Unix(), "places": []any{}, "assistants": []any{map[string]any{"id": "claude", "available": true, "installed": true}}})
		case strings.HasSuffix(r.URL.Path, "/transcript"):
			w.Write([]byte(`{"entries":[],"signature":"fixture","evidence":"transcript"}`))
		default:
			w.Write([]byte(`{"rows":[],"assigned_items":[],"recent_items":[],"todos":[],"entries":[],"files":[],"count":0,"ok":true,"done":0,"active":0,"waiting":0}`))
		}
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	t.Log("Synthetic fixture listening on 127.0.0.1:8877")
	select {
	case <-done:
	case <-time.After(15 * time.Minute):
		t.Fatal("preview deadline")
	}
}
