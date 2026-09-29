package http

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

// An ordinary shell is not an Agent Session, on any route under
// /v1/sessions/{id}/. The list never showed one (sessions.go), but every route
// below it resolved its id against the whole inventory, so a device that may
// send could type a command into a person's bash, and a read-only one could
// read the diff of whatever directory that bash sat in.

// touchPane is a pane that also records the writes the receipts pane does
// not: an interrupt, a typed line, a reveal and a screen read for a person.
type touchPane struct{ *pane }

func (p touchPane) Interrupt(ctx context.Context, s session.Session) error {
	p.act("interrupt")
	return nil
}
func (p touchPane) Type(ctx context.Context, s session.Session, text string) error {
	p.act("type:" + text)
	return nil
}
func (p touchPane) Reveal(ctx context.Context, s session.Session, activate bool) error {
	p.act("reveal")
	return nil
}
func (p touchPane) Screen(ctx context.Context, s session.Session, lines int) (string, bool) {
	p.act("screen")
	return p.pane.Screen(ctx, s, lines)
}

// shellPane is tmux pane %0 running bash: no assistant in it.
func shellPane(cwd string) touchPane {
	return touchPane{&pane{s: session.Session{ID: "%0", Backend: session.BackendTmux, CWD: cwd,
		State: session.StateIdle}, screen: "bash-3.2$ "}}
}

// wholeServer is a daemon behind its real gate, with one pane on it, and this
// machine's own token and machine token.
func wholeServer(t *testing.T, host ports.TerminalHost, screen ports.ScreenHost) (*Server, http.Handler, string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(t.TempDir(), "swift"))
	t.Setenv("CLAWDLINE_NEXT_WEB", "")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hosts := []ports.TerminalHost{host}
	s := &Server{cfg: config.Config{Dir: dir, Port: 7757}, store: st, icons: &icon.Registry{},
		terminals: hosts, inventory: app.Inventory{Terminals: hosts, Screen: screen},
		broker:  &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir},
		screens: app.NewScreens(hosts, nil, nil)}
	t.Cleanup(s.screens.Stop)
	handler := s.Handler()
	local, machine, err := s.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	return s, handler, local, machine
}

// sessionRouteShapes is every `/v1/sessions/{id}/…` shape this package's own
// code recognises, read out of the code rather than typed here: the verbs
// handed to sessionVerbIs, the cases of a `switch verb`, and every segment a
// path reader compares at a fixed index (`parts[1] == "documents"`,
// `parts[2] != "diff"`) in a function that names `/v1/sessions/`. A route
// added later in any of those shapes is in this list without anybody
// remembering to put it here.
func sessionRouteShapes(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	shapes := map[string]bool{}
	literal := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(lit.Value)
		return v, err == nil && v != ""
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			namesSessions := false
			positional := map[int]string{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.BasicLit:
					if v, ok := literal(n); ok && v == "/v1/sessions/" {
						namesSessions = true
					}
				case *ast.CallExpr:
					if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "sessionVerbIs" && len(n.Args) > 1 {
						if v, ok := literal(n.Args[1]); ok {
							shapes[v] = true
						}
					}
				case *ast.SwitchStmt:
					if tag, ok := n.Tag.(*ast.Ident); ok && tag.Name == "verb" {
						for _, stmt := range n.Body.List {
							for _, e := range stmt.(*ast.CaseClause).List {
								if v, ok := literal(e); ok {
									shapes[v] = true
								}
							}
						}
					}
				case *ast.BinaryExpr:
					if n.Op != token.EQL && n.Op != token.NEQ {
						return true
					}
					index, ok := n.X.(*ast.IndexExpr)
					if !ok {
						return true
					}
					at, ok := index.Index.(*ast.BasicLit)
					if !ok || at.Kind != token.INT {
						return true
					}
					k, _ := strconv.Atoi(at.Value)
					if v, ok := literal(n.Y); ok && k > 0 {
						positional[k] = v
					}
				}
				return true
			})
			if !namesSessions || len(positional) == 0 {
				continue
			}
			last := 0
			for k := range positional {
				last = max(last, k)
			}
			segments := make([]string, last)
			for k := 1; k <= last; k++ {
				segments[k-1] = "x"
				if v, ok := positional[k]; ok {
					segments[k-1] = v
				}
			}
			shapes[strings.Join(segments, "/")] = true
		}
	}
	out := make([]string, 0, len(shapes))
	for shape := range shapes {
		out = append(out, shape)
	}
	sort.Strings(out)
	return out
}

// TestEverySessionRouteRefusesAnOrdinaryShell asks every shape, one level
// deeper as well, by every method a route here answers, through the real
// handler and gate with this machine's own token, about a pane running bash.
// Each answer is 409 not_an_agent_session, and nothing reaches the pane.
func TestEverySessionRouteRefusesAnOrdinaryShell(t *testing.T) {
	shapes := sessionRouteShapes(t)
	// A floor, not the table: the reader above has to keep finding the
	// routes it was written against, or it has quietly stopped reading.
	for _, known := range []string{"send", "key", "interrupt", "close", "archive", "smart-title", "title",
		"screen", "info", "git", "git/diff", "focus", "todos", "skills", "links", "agents", "shells", "documents"} {
		found := false
		for _, shape := range shapes {
			found = found || shape == known
		}
		if !found {
			t.Fatalf("the route reader no longer finds %q; it found %q", known, shapes)
		}
	}
	repo := t.TempDir()
	p := shellPane(repo)
	_, handler, local, _ := wholeServer(t, p, p)
	asked := 0
	for _, shape := range shapes {
		for _, path := range []string{shape, shape + "/x"} {
			for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
				var req *http.Request
				if method == http.MethodPost {
					req = httptest.NewRequest(method, "/v1/sessions/%250/"+path, strings.NewReader(`{"text":"echo PWNED","key":"1"}`))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Idempotency-Key", "probe-"+path)
				} else {
					req = httptest.NewRequest(method, "/v1/sessions/%250/"+path, nil)
				}
				req.Host = "127.0.0.1:7757"
				req.Header.Set("Authorization", "Bearer "+local)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				asked++
				if rec.Code != http.StatusConflict {
					t.Errorf("%s /v1/sessions/%%0/%s answered %d, not 409: %s", method, path, rec.Code, rec.Body)
					continue
				}
				if method != http.MethodHead && codeOf(t, rec) != "not_an_agent_session" {
					t.Errorf("%s /v1/sessions/%%0/%s answered %s", method, path, rec.Body)
				}
			}
		}
	}
	if got := p.done(); len(got) != 0 {
		t.Fatalf("the shell was touched: %q", got)
	}
	t.Logf("%d shapes, %d requests: %q", len(shapes), asked, shapes)
}

// The same send to a pane running Claude Code is typed, through the same
// handler and gate: the refusal is about what is in the pane, not the route.
func TestAnAgentSessionIsStillTypedThroughTheWholeHandler(t *testing.T) {
	p := touchPane{waitingPane("%4", "")}
	_, handler, local, _ := wholeServer(t, p, p)
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/%254/send", strings.NewReader(`{"text":"hello"}`))
	req.Host = "127.0.0.1:7757"
	req.Header.Set("Authorization", "Bearer "+local)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("a send to an Agent Session answered %d: %s", rec.Code, rec.Body)
	}
	if got := p.done(); len(got) != 1 || got[0] != "send:hello" {
		t.Fatalf("typed %q", got)
	}
}

// A Cloud command reaches the same handler in this process (local.go), and
// is refused the same way.
func TestACloudSendToAnOrdinaryShellIsRefused(t *testing.T) {
	p := shellPane("")
	_, handler, local, machine := wholeServer(t, p, p)
	router := cloud.Router{Handler: handler, Authorize: cloud.LocalAuthorizer(local, machine)}
	got, err := router.Do(context.Background(), cloudops.LocalRequest{Method: http.MethodPost,
		Path: "/v1/sessions/%250/send", Body: []byte(`{"text":"echo PWNED"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != http.StatusConflict || !strings.Contains(string(got.Body), "not_an_agent_session") {
		t.Fatalf("a Cloud send to bash answered %d %s", got.Status, got.Body)
	}
	if typed := p.done(); len(typed) != 0 {
		t.Fatalf("the shell was typed into: %q", typed)
	}
}

// The broker types a briefing into a child's pane through Actions, and in the
// child's first seconds that pane's command is still the shell that is
// starting claude. The refusal is on the routes only; this path still types.
func TestABriefingStillReachesAPaneThatIsNotYetClaude(t *testing.T) {
	p := shellPane("")
	s, _, _, _ := wholeServer(t, p, p)
	if err := newBroker(s).Type(context.Background(), "%0", "read CHILD.md"); err != nil {
		t.Fatalf("the briefing was refused: %v", err)
	}
	if got := p.done(); len(got) != 1 || got[0] != "send:read CHILD.md" {
		t.Fatalf("typed %q", got)
	}
}
