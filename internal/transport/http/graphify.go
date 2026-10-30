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

	"github.com/sainteye/clawdline/internal/adapters/graphify"
)

func sessionSubresourcePath(r *http.Request, name string) (string, bool) {
	parts := strings.Split(strings.TrimPrefix(routePath(r), "/v1/sessions/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != name {
		return "", false
	}
	return decodeSegment(parts[0]), true
}

// The machine switch is checked before resolving a directory or reading an index.
func (s *Server) graphifyAdmission(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeRefusal(w, 405, "method_not_allowed", "GET or POST only")
		return false
	}
	if r.URL.RawQuery != "" {
		writeRefusal(w, 400, "bad_request", "Graphify takes no query fields.")
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
		body, err := io.ReadAll(io.LimitReader(r.Body, graphify.BuildBodyBytesLimit+1))
		var input map[string]any
		if err != nil || len(body) > graphify.BuildBodyBytesLimit || json.Unmarshal(body, &input) != nil || input == nil || len(input) != 0 {
			writeRefusal(w, 400, "bad_request", "An index build accepts only an empty JSON object.")
			return false
		}
		r.Body = io.NopCloser(bytes.NewReader([]byte("{}")))
	} else if r.Body != nil {
		probe, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(probe) > 0 {
			writeRefusal(w, 400, "bad_request", "A Graphify read takes no body.")
			return false
		}
	}
	values, err := s.settingsFile().Read()
	if err != nil {
		writeSettingsFailure(w, s.settingsFile(), err)
		return false
	}
	enabled, _ := values.Bool("graphify_enabled")
	if !enabled {
		writeRefusal(w, 403, "graphify_disabled", "Enable Graphify in this machine's integration settings to view a graph.")
		return false
	}
	return true
}

func (s *Server) projectGraphifyRoute(w http.ResponseWriter, r *http.Request, place string) {
	if !s.graphifyAdmission(w, r) {
		return
	}
	project, found := s.workV2Project(r.Context(), place)
	if !found {
		writeRefusal(w, 404, "project_not_found", "Choose a Project that this machine currently lists.")
		return
	}
	s.graphifyResponse(w, r, project.Path)
}

func (s *Server) sessionGraphifyRoute(w http.ResponseWriter, r *http.Request, id string) {
	if !s.graphifyAdmission(w, r) {
		return
	}
	item, err := s.actions().FindForRead(r.Context(), id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	if item.CWD == "" {
		writeRefusal(w, 404, "graphify_directory_missing", "This Session has no local working directory.")
		return
	}
	if !s.admitPinnedTarget(w, r, id) {
		return
	}
	s.graphifyResponse(w, r, item.CWD)
}

func (s *Server) graphifyEnabled() bool {
	values, err := s.settingsFile().Read()
	enabled, _ := values.Bool("graphify_enabled")
	return err == nil && enabled
}
func (s *Server) graphifyBuilds() *graphify.Builder {
	s.graphifyMu.Lock()
	defer s.graphifyMu.Unlock()
	if s.graphifyBuilder == nil {
		s.graphifyBuilder = graphify.NewBuilder(filepath.Join(s.cfg.Dir, "integrations", "graphify"), s.graphifyEnabled, nil)
	}
	return s.graphifyBuilder
}

func (s *Server) StopGraphify(ctx context.Context) error { return s.graphifyBuilds().Close(ctx) }
func (s *Server) graphifyResponse(w http.ResponseWriter, r *http.Request, root string) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		s.writeGraphifyView(w, r, root)
		return
	}
	// Durable HTTP acceptance receipts prevent a lost answer or daemon restart
	// from running the same command again. Current progress is always a GET.
	key := requestDigest([]byte(routePath(r)), []byte(r.Header.Get("Idempotency-Key")))
	s.receipted(w, r, store.ReceiptKey{Scope: scopePlaces, Actor: actorOf(r), Key: key}, requestDigest([]byte(r.Method), []byte(routePath(r)), []byte("{}")),
		func(status int) bool { return status != 429 && status != 503 }, func(w http.ResponseWriter) {
			out, err := s.graphifyBuilds().Start(root, key)
			if err != nil {
				var typed graphify.BuildError
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
				writeRefusal(w, status, "graphify_"+code, "The local index build was not started. Read its status before retrying.")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(out)
		})
}

func (s *Server) writeGraphifyView(w http.ResponseWriter, r *http.Request, root string) {
	view, err := graphify.View(r.Context(), root)
	if err != nil {
		writeRefusal(w, 503, "graphify_unavailable", "The graph could not be checked. Try again.")
		return
	}
	// Recheck after scanning, so an off switch during the read does not publish data.
	values, err := s.settingsFile().Read()
	enabled, _ := values.Bool("graphify_enabled")
	if err != nil || !enabled {
		writeRefusal(w, 403, "graphify_disabled", "Graphify is disabled or its setting is unavailable.")
		return
	}
	view.Build = s.graphifyBuilds().Status(root)
	view.CanBuild = view.State == "missing" && view.Build.State != "running"
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, view)
}
