package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/opsdocs"
	"github.com/sainteye/clawdline/internal/adapters/projectfiles"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	domain "github.com/sainteye/clawdline/internal/domain/opsdocs"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// The Ops documents list (docs/ops-documents.md): a Project's own files and
// links, under /v1/projects/{place}/ops for the console and under /v1/ops for
// a Session's CLI, which knows its terminal rather than a place.

// opsBodyLimit is the largest Ops write body: a saved file's text plus its
// JSON. It is projectfiles.MaxWriteBytes, registered there.
const opsBodyLimit = projectfiles.MaxWriteBytes

var (
	opsByServer     sync.Map
	opsProberByHome sync.Map
)

func (s *Server) ops() *app.OpsDocuments {
	if v, ok := opsByServer.Load(s); ok {
		return v.(*app.OpsDocuments)
	}
	home, _ := os.UserHomeDir()
	v, _ := opsByServer.LoadOrStore(s, &app.OpsDocuments{Store: s.store, Home: home})
	return v.(*app.OpsDocuments)
}

func (s *Server) opsProber() *opsdocs.Prober {
	if v, ok := opsProberByHome.Load(s); ok {
		return v.(*opsdocs.Prober)
	}
	p := &opsdocs.Prober{Client: &http.Client{Timeout: opsdocs.GitHubProbeMillisecondsLimit * time.Millisecond},
		Token: func(ctx context.Context) string {
			conn := s.githubIntegration()
			if conn == nil || !conn.Authorized() {
				return ""
			}
			token, err := conn.AccessToken(ctx)
			if err != nil {
				return ""
			}
			return token
		}}
	v, _ := opsProberByHome.LoadOrStore(s, p)
	return v.(*opsdocs.Prober)
}

// SettleOpsRenames finishes or undoes Ops renames a stopped daemon left.
func (s *Server) SettleOpsRenames(ctx context.Context) error {
	return s.ops().SettleRenames(ctx)
}

// opsRoute is /v1/projects/{place}/ops and everything under it.
func (s *Server) opsRoute(w http.ResponseWriter, r *http.Request, placeID string, rest []string) {
	project, found := s.workV2Project(r.Context(), placeID)
	if !found {
		writeRefusal(w, http.StatusNotFound, "project_not_found", "Choose a Project that this machine currently lists.")
		return
	}
	key, ok := projects.CanonicalProjectKey(project.Path)
	if !ok {
		key = project.Path
	}
	switch {
	case len(rest) == 0:
		switch r.Method {
		case http.MethodGet:
			if !opsNoQueryNoBody(w, r) {
				return
			}
			list, err := s.ops().List(r.Context(), key)
			if err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			writeJSON(w, list)
		case http.MethodPost:
			s.opsWrite(w, r, func(w http.ResponseWriter, body []byte) { s.opsCreate(w, r, key, body) })
		default:
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Read the Ops list with GET or add to it with POST.")
		}
	case len(rest) == 1 && rest[0] == "setup":
		s.opsSetupRoute(w, r, key)
	case len(rest) == 1 && rest[0] != "":
		id := rest[0]
		switch r.Method {
		case http.MethodGet:
			if !opsNoQueryNoBody(w, r) {
				return
			}
			read, err := s.ops().Read(r.Context(), key, id)
			if err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			writeJSON(w, read)
		case http.MethodPatch:
			s.opsWrite(w, r, func(w http.ResponseWriter, body []byte) { s.opsUpdate(w, r, key, id, body) })
		case http.MethodDelete:
			s.opsWrite(w, r, func(w http.ResponseWriter, body []byte) {
				if len(bytes.TrimSpace(body)) > 0 {
					writeRefusal(w, http.StatusBadRequest, "bad_request", "Removing an Ops document takes no body.")
					return
				}
				if err := s.ops().Delete(r.Context(), key, id); err != nil {
					s.writeWorkV2Error(w, err)
					return
				}
				writeJSON(w, contract.OpsDeleted{OK: true, Deleted: id})
			})
		default:
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Read, change or remove one Ops document.")
		}
	case len(rest) == 2 && rest[1] == "resolve-request":
		if r.Method != http.MethodPost {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Ask a Session to read a link with POST.")
			return
		}
		id := rest[0]
		s.opsWrite(w, r, func(w http.ResponseWriter, body []byte) { s.opsResolveRequest(w, r, key, id, body) })
	default:
		writeNoSuchRoute(w, r)
	}
}

func opsNoQueryNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.ContentLength > 0 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "This Ops read takes no query and no body.")
		return false
	}
	return true
}

// opsMayWrite is a device that may send, or this machine's own CLI.
func opsMayWrite(r *http.Request) bool { return maySend(r) || machineAuthed(r) }

// opsWrite is the door every Ops change passes: send access, a key, a
// bounded body, and a receipt whose digest includes the body.
func (s *Server) opsWrite(w http.ResponseWriter, r *http.Request, do func(http.ResponseWriter, []byte)) {
	if !opsMayWrite(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This device may read the Ops list but cannot change it.")
		return
	}
	if r.URL.RawQuery != "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "An Ops change takes no query fields.")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeRefusal(w, http.StatusBadRequest, "idempotency_key_required", "An Ops change needs an Idempotency-Key.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, opsBodyLimit+1))
	if err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The Ops change could not be read.")
		return
	}
	if len(body) > opsBodyLimit {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large", "The Ops change is too large.")
		return
	}
	receipt := store.ReceiptKey{Scope: scopePlaces, Actor: actorOf(r), Key: key}
	s.receipted(w, r, receipt, requestDigest([]byte(r.Method), []byte(routePath(r)), body),
		func(status int) bool { return status != http.StatusTooManyRequests && status < 500 },
		func(w http.ResponseWriter) { do(w, body) })
}

func decodeOps(body []byte, into any) bool {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	return dec.Decode(into) == nil && dec.Decode(new(any)) == io.EOF
}

func (s *Server) opsCreate(w http.ResponseWriter, r *http.Request, key string, body []byte) {
	var in struct {
		Kind       string `json:"kind"`
		URL        string `json:"url"`
		Path       string `json:"path"`
		Name       string `json:"name"`
		Content    string `json:"content"`
		Title      string `json:"title"`
		Collection string `json:"collection"`
		Git        string `json:"git"`
	}
	if !decodeOps(body, &in) {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Add a link, a file, a Markdown file or a collection.")
		return
	}
	ctx := r.Context()
	switch in.Kind {
	case "collection":
		if in.URL != "" || in.Path != "" || in.Content != "" || in.Title != "" || in.Collection != "" || in.Git != "" {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "A collection takes only a name.")
			return
		}
		c, err := s.ops().CreateCollection(ctx, key, in.Name)
		if err != nil {
			s.writeWorkV2Error(w, err)
			return
		}
		writeJSONStatus(w, http.StatusCreated, contract.OpsCollectionAnswer{Collection: c})
		return
	case "markdown":
		if in.URL != "" || in.Path != "" || in.Title != "" {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "A Markdown file takes a name, content, a collection and a git choice.")
			return
		}
		e, err := s.ops().CreateMarkdown(ctx, key, app.NewOpsMarkdown{Name: in.Name, Content: in.Content,
			Collection: in.Collection, Git: in.Git})
		if err != nil {
			s.writeWorkV2Error(w, err)
			return
		}
		writeJSONStatus(w, http.StatusCreated, contract.OpsEntryAnswer{Entry: e})
		return
	case domain.KindLink, domain.KindFile:
		if in.Name != "" || in.Content != "" || in.Git != "" || (in.Kind == domain.KindLink) == (in.URL == "") ||
			(in.Kind == domain.KindFile) == (in.Path == "") {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "A link takes a url; a file takes a path.")
			return
		}
		e, err := s.ops().Create(ctx, key, app.NewOpsEntry{Kind: in.Kind, URL: in.URL, Path: in.Path,
			Title: in.Title, Collection: in.Collection})
		if err != nil {
			s.writeWorkV2Error(w, err)
			return
		}
		writeJSONStatus(w, http.StatusCreated, contract.OpsEntryAnswer{Entry: e})
		return
	}
	writeRefusal(w, http.StatusBadRequest, "bad_request", "Add a link, a file, a Markdown file or a collection.")
}

func (s *Server) opsUpdate(w http.ResponseWriter, r *http.Request, key, id string, body []byte) {
	var in struct {
		Title           *string `json:"title"`
		Collection      *string `json:"collection"`
		Position        *int64  `json:"position"`
		Name            *string `json:"name"`
		Rename          *string `json:"rename"`
		Content         *string `json:"content"`
		ExpectedVersion *string `json:"expected_version"`
	}
	if !decodeOps(body, &in) {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Change a title, collection, position, name or file.")
		return
	}
	patch := app.OpsPatch{Title: in.Title, Collection: in.Collection, Position: in.Position, Name: in.Name,
		Rename: in.Rename, Content: in.Content, ExpectedVersion: in.ExpectedVersion}
	if domain.ValidCollectionID(id) {
		c, err := s.ops().UpdateCollection(r.Context(), key, id, patch)
		if err != nil {
			s.writeWorkV2Error(w, err)
			return
		}
		writeJSON(w, contract.OpsCollectionAnswer{Collection: c})
		return
	}
	e, version, err := s.ops().Update(r.Context(), key, id, patch)
	if err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	writeJSON(w, contract.OpsEntryAnswer{Entry: e, Version: version})
}

func (s *Server) opsSetupRoute(w http.ResponseWriter, r *http.Request, key string) {
	switch r.Method {
	case http.MethodGet:
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || r.ContentLength > 0 || len(query) > 1 || len(query) == 1 && (len(query["probe"]) != 1 || query.Get("probe") != "github") {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "The Ops setup read takes only probe=github.")
			return
		}
		setup := opsdocs.Setup(r.Context(), key)
		if query.Get("probe") == "github" && setup.Git == contract.OpsGitStateRepo {
			s.opsProber().Probe(r.Context(), key, &setup)
		} else if query.Get("probe") == "github" {
			setup.Visibility, setup.VisibilityDetail = contract.OpsVisibilityUnknown, "not a git repository"
		}
		writeJSON(w, setup)
	case http.MethodPatch:
		s.opsWrite(w, r, func(w http.ResponseWriter, body []byte) {
			var in struct {
				Git string `json:"git"`
			}
			if !decodeOps(body, &in) || in.Git != "include" && in.Git != "exclude" {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "Say whether git includes or excludes the clawdline-ops folder.")
				return
			}
			if err := opsdocs.SetExcluded(r.Context(), key, in.Git == "exclude"); err != nil {
				if err == opsdocs.ErrNoRepository {
					writeRefusal(w, http.StatusConflict, "ops_not_git", "This Project is not in a git repository.")
					return
				}
				writeRawRefusal(w, http.StatusInternalServerError, "ops_exclude_failed", "The exclude file could not be changed: "+err.Error())
				return
			}
			writeJSON(w, opsdocs.Setup(r.Context(), key))
		})
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Read the Ops setup with GET or change it with PATCH.")
	}
}

// opsResolveRequest creates, without sending, the to-do that asks a Session
// of the same Project to read a link.
func (s *Server) opsResolveRequest(w http.ResponseWriter, r *http.Request, key, id string, body []byte) {
	var in struct {
		Terminal string `json:"terminal"`
	}
	if !decodeOps(body, &in) || strings.TrimSpace(in.Terminal) == "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Name the Session terminal that should read the link.")
		return
	}
	ctx := r.Context()
	sess, err := s.actions().Find(ctx, in.Terminal)
	if err != nil {
		writeRefusal(w, http.StatusConflict, "session_unavailable", "The Session is unavailable or has no conversation id.")
		return
	}
	if sessionKey, ok := projects.CanonicalProjectKey(sess.CWD); !ok || sessionKey != key {
		writeRefusal(w, http.StatusConflict, "ops_session_other_project", "Choose a Session working in this Project.")
		return
	}
	conversation, err := s.sessionTodoConversation(ctx, in.Terminal, false)
	if err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	e, todo, err := s.ops().ResolveRequest(ctx, key, id, in.Terminal, conversation, personPrincipal(r))
	if err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	writeJSON(w, contract.OpsResolveRequested{Entry: e, Todo: contract.OpsTodoRef{ID: todo}})
}

// opsAgentRoute is /v1/ops and /v1/ops/: a Session's CLI names its terminal
// or an entry id, never a place.
func (s *Server) opsAgentRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(routePath(r), "/v1/ops")
	rest = strings.TrimPrefix(rest, "/")
	parts := []string{}
	if rest != "" {
		for _, p := range strings.Split(rest, "/") {
			parts = append(parts, decodeSegment(p))
		}
	}
	switch {
	case len(parts) == 0:
		if r.Method != http.MethodGet {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Read a Session's Ops list with GET.")
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(query) != 1 || len(query["session"]) != 1 || query.Get("session") == "" || r.ContentLength > 0 {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "Name one Session with ?session=.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		item, err := s.actions().FindForRead(ctx, query.Get("session"))
		if err != nil || strings.TrimSpace(item.CWD) == "" {
			writeRefusal(w, http.StatusNotFound, "session_not_found", "No Session with that terminal is working in a Project.")
			return
		}
		key, ok := projects.CanonicalProjectKey(item.CWD)
		if !ok {
			writeRefusal(w, http.StatusNotFound, "project_not_found", "That Session is not working in a Project this machine lists.")
			return
		}
		place := ""
		for id, p := range s.workV2Projects(ctx) {
			if p.Path == key {
				place = id
				break
			}
		}
		if place == "" {
			writeRefusal(w, http.StatusNotFound, "project_not_found", "That Session is not working in a Project this machine lists.")
			return
		}
		list, err := s.ops().List(ctx, key)
		if err != nil {
			s.writeWorkV2Error(w, err)
			return
		}
		list.Place = place
		writeJSON(w, list)
	case len(parts) == 1 && parts[0] != "":
		if r.Method != http.MethodGet {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Read one Ops document with GET.")
			return
		}
		if !opsNoQueryNoBody(w, r) {
			return
		}
		read, err := s.ops().Read(r.Context(), "", parts[0])
		if err != nil {
			s.writeWorkV2Error(w, err)
			return
		}
		writeJSON(w, read)
	case len(parts) == 2 && parts[1] == "resolve":
		if r.Method != http.MethodPost {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Report what a link is with POST.")
			return
		}
		// Only the Session that was asked reports, with this machine's own
		// credential; a device or a Cloud viewer is a person, not that agent.
		if !machineAuthed(r) || cloudtransport.CloudViewerOf(r.Context()) != "" {
			writeRefusal(w, http.StatusForbidden, "ops_resolve_agent_only", "Only the Session asked to read the link reports what it is.")
			return
		}
		id := parts[0]
		s.opsWrite(w, r, func(w http.ResponseWriter, body []byte) {
			var in struct {
				Todo        string `json:"todo"`
				Title       string `json:"title"`
				NeedsAccess bool   `json:"needs_access"`
				Reason      string `json:"reason"`
			}
			if !decodeOps(body, &in) {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "Report the to-do and a title, or needs_access and a reason.")
				return
			}
			e, err := s.ops().Resolve(r.Context(), id, app.OpsResolution{Todo: in.Todo, Title: in.Title,
				Reason: in.Reason, NeedsAccess: in.NeedsAccess})
			if err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			writeJSON(w, contract.OpsEntryAnswer{Entry: e})
		})
	default:
		writeNoSuchRoute(w, r)
	}
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
