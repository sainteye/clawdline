package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/codegraph"
)

func sessionSubresourcePath(r *http.Request, name string) (string, bool) {
	parts := strings.Split(strings.TrimPrefix(routePath(r), "/v1/sessions/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != name {
		return "", false
	}
	return decodeSegment(parts[0]), true
}

// The machine switch is checked before resolving a directory or reading an index.
func (s *Server) codegraphAdmission(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeRefusal(w, 405, "method_not_allowed", "GET or POST only")
		return false
	}
	if r.URL.RawQuery != "" {
		writeRefusal(w, 400, "bad_request", "CodeGraph takes no query fields.")
		return false
	}
	if r.Method == http.MethodPost {
		if !maySend(r) || actorOf(r) == "" {
			writeRefusal(w, 403, "forbidden", "Building an index needs send access to this machine.")
			return false
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len(key) > store.AntigravityConsentIdentityBytesLimit || strings.ContainsFunc(key, func(c rune) bool { return c < 33 || c > 126 }) {
			writeRefusal(w, 400, "bad_request", "An index build needs a visible ASCII Idempotency-Key of at most 128 bytes.")
			return false
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, codegraph.BuildBodyBytesLimit+1))
		var input map[string]any
		if err != nil || len(body) > codegraph.BuildBodyBytesLimit || json.Unmarshal(body, &input) != nil || input == nil || len(input) != 0 {
			writeRefusal(w, 400, "bad_request", "An index build accepts only an empty JSON object.")
			return false
		}
		r.Body = io.NopCloser(bytes.NewReader([]byte("{}")))
	} else if r.Body != nil {
		probe, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(probe) > 0 {
			writeRefusal(w, 400, "bad_request", "A CodeGraph read takes no body.")
			return false
		}
	}
	values, err := s.settingsFile().Read()
	if err != nil {
		writeSettingsFailure(w, s.settingsFile(), err)
		return false
	}
	enabled, _ := values.Bool("codegraph_enabled")
	if !enabled {
		writeRefusal(w, 403, "codegraph_disabled", "Enable CodeGraph in this machine's integration settings to view a graph.")
		return false
	}
	return true
}

func (s *Server) projectCodeGraphRoute(w http.ResponseWriter, r *http.Request, place string) {
	if !s.codegraphAdmission(w, r) {
		return
	}
	project, found := s.workV2Project(r.Context(), place)
	if !found {
		writeRefusal(w, 404, "project_not_found", "Choose a Project that this machine currently lists.")
		return
	}
	s.codegraphResponse(w, r, project.Path)
}

func (s *Server) sessionCodeGraphRoute(w http.ResponseWriter, r *http.Request, id string) {
	if !s.codegraphAdmission(w, r) {
		return
	}
	item, err := s.actions().FindForRead(r.Context(), id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	if item.CWD == "" {
		writeRefusal(w, 404, "codegraph_directory_missing", "This Session has no local working directory.")
		return
	}
	if !s.admitPinnedTarget(w, r, id) {
		return
	}
	s.codegraphResponse(w, r, item.CWD)
}

func (s *Server) codegraphEnabled() bool {
	values, err := s.settingsFile().Read()
	enabled, _ := values.Bool("codegraph_enabled")
	return err == nil && enabled
}
func (s *Server) codegraphBuilds() *codegraph.Builder {
	s.codegraphMu.Lock()
	defer s.codegraphMu.Unlock()
	if s.codegraphBuilder == nil {
		s.codegraphBuilder = codegraph.NewBuilder(filepath.Join(s.cfg.Dir, "integrations", "codegraph"), s.codegraphEnabled, nil)
	}
	return s.codegraphBuilder
}

func (s *Server) StopCodeGraph(ctx context.Context) error { return s.codegraphBuilds().Close(ctx) }
func (s *Server) codegraphResponse(w http.ResponseWriter, r *http.Request, root string) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		s.writeCodeGraphView(w, r, root)
		return
	}
	// Durable HTTP acceptance receipts prevent a lost answer or daemon restart
	// from running the same command again. Current progress is always a GET.
	key := requestDigest([]byte(routePath(r)), []byte(r.Header.Get("Idempotency-Key")))
	s.receipted(w, r, store.ReceiptKey{Scope: scopePlaces, Actor: actorOf(r), Key: key}, requestDigest([]byte(r.Method), []byte(routePath(r)), []byte("{}")),
		func(status int) bool { return status != 429 && status != 503 }, func(w http.ResponseWriter) {
			out, err := s.codegraphBuilds().Start(root, key)
			if err != nil {
				var typed codegraph.BuildError
				code := "failed"
				if errors.As(err, &typed) {
					code = string(typed)
				}
				status := 409
				if code == "busy" || code == "history_full" {
					status = 429
				}
				if code == "disabled" {
					status = 403
				}
				if code == "unavailable" {
					status = 503
				}
				writeRefusal(w, status, "codegraph_"+code, "The local index build was not started. Read its status before retrying.")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(out)
		})
}

func (s *Server) writeCodeGraphView(w http.ResponseWriter, r *http.Request, root string) {
	view, err := s.codegraphBuilds().View(r.Context(), root)
	if err != nil {
		writeRefusal(w, 503, "codegraph_unavailable", "The graph could not be checked. Try again.")
		return
	}
	// Recheck after scanning, so an off switch during the read does not publish data.
	values, err := s.settingsFile().Read()
	enabled, _ := values.Bool("codegraph_enabled")
	if err != nil || !enabled {
		writeRefusal(w, 403, "codegraph_disabled", "CodeGraph is disabled or its setting is unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, view)
}

// This route is deliberately local-orchestrator-only. Cloud and device tokens
// cannot supply filesystem roots or invoke the native query service.
func (s *Server) codegraphQueryRoute(w http.ResponseWriter, r *http.Request) {
	if !machineAuthed(r) {
		writeRefusal(w, 403, "forbidden", "Only this machine may query a local code index.")
		return
	}
	if r.Method != http.MethodPost {
		writeRefusal(w, 405, "method_not_allowed", "POST only")
		return
	}
	if !s.codegraphEnabled() {
		writeRefusal(w, 403, "codegraph_disabled", "CodeGraph is disabled or its setting is unavailable.")
		return
	}
	if r.URL.RawQuery != "" {
		writeRefusal(w, 400, "bad_request", "A local code query takes no URL query fields.")
		return
	}
	var in struct {
		Directory string `json:"directory"`
		Query     string `json:"query"`
	}
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, codegraph.QueryBodyBytesLimit+1))
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if readErr != nil || len(raw) > codegraph.QueryBodyBytesLimit || decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF || !filepath.IsAbs(in.Directory) || len(in.Query) == 0 || len(in.Query) > codegraph.QueryBytesLimit {
		writeRefusal(w, 400, "bad_request", "A local code query needs an absolute directory and a bounded question.")
		return
	}
	answer, err := s.codegraphBuilds().Query(r.Context(), in.Directory, in.Query)
	if err != nil {
		var typed codegraph.BuildError
		code := "unavailable"
		if errors.As(err, &typed) {
			code = string(typed)
		}
		writeRefusal(w, 409, "codegraph_"+code, "The local code query was refused. Use current source search or refresh the index.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, answer)
}
