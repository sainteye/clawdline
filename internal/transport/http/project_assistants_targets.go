package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This independent route leaves every valid Bot ID (including "projects")
// available under /v1/project-assistants/. It never initializes a Host/provider.
func (s *Server) projectAssistantTargetsRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	encoded, err := s.projectAssistantTargetsReply(r)
	if err != nil {
		paWriteHTTPFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(encoded)
}

func (s *Server) projectAssistantTargetsAuthority(ctx context.Context, machine string) (app.Authority, error) {
	i, err := s.projectAssistantIdentity()
	if err != nil {
		return app.Authority{}, err
	}
	if machine == "" || machine != i.MachineID {
		return app.Authority{}, paHTTPFailure(pa.ScopeDenied, "machine_id")
	}
	a, ok := ctx.Value(accessKey{}).(access)
	if !ok {
		return app.Authority{}, paHTTPFailure(pa.ScopeDenied, "gate_proof")
	}
	if _, _, _, _, err = s.projectAssistantHuman(ctx, a, i); err != nil {
		return app.Authority{}, err
	}
	s.projectAssistants.mu.RLock()
	host := s.projectAssistants.host
	s.projectAssistants.mu.RUnlock()
	if host == nil {
		// A default-off metadata read must not install feature tables or enable
		// anything. Retained history uses the existing Source access policy.
		return app.Authority{}, paHTTPFailure(pa.Disabled, "source_feature")
	}
	authority, err := s.projectAssistantAuthority(ctx, pa.BotKey{MachineID: machine}, false)
	if err != nil {
		return authority, err
	}
	if err = pa.CheckSource(authority.Source, false); err != nil {
		return authority, err
	}
	if authority.Source.MachineID != machine || authority.PrincipalID == "" || authority.ReaderID == "" || authority.AuthorizationRevision == "" {
		return authority, paHTTPFailure(pa.ScopeDenied, "source_authority")
	}
	if authority.Source.State != pa.SourceOnline || !authority.Source.Fresh {
		return authority, paHTTPFailure(pa.Offline, "source")
	}
	if authority.Source.Access == pa.Hidden {
		return authority, paHTTPFailure(pa.Disabled, "source_feature")
	}
	return authority, nil
}

// Only the explicit registry is read: no provider history, project content,
// canonical scope resolution or per-directory stat belongs to this catalog.
func (s *Server) projectAssistantTargetCatalog(machine string) ([]contract.ProjectAssistantProjectOption, string, error) {
	rows, err := projects.OpenPlaceRegistry(s.cfg.Dir, swiftDirs()...).List()
	if err != nil {
		return nil, "", paHTTPFailure(pa.StaleVersion, "project_registry")
	}
	if int64(len(rows)) > CapacityLimit(capacity.PlacesRegistered) {
		return nil, "", paHTTPFailure(pa.CapacityFull, "project_registry")
	}
	sort.Slice(rows, func(i, j int) bool { return projects.PlaceID(rows[i].Path) < projects.PlaceID(rows[j].Path) })
	items := make([]contract.ProjectAssistantProjectOption, 0, len(rows))
	for _, row := range rows {
		id := projects.PlaceID(row.Path)
		if len(items) != 0 && items[len(items)-1].Target.ProjectID == id {
			return nil, "", paHTTPFailure(pa.ScopeDenied, "ambiguous_project")
		}
		label := filepath.Base(row.Path)
		if !utf8.ValidString(label) || strings.TrimSpace(label) == "" || strings.ContainsFunc(label, unicode.IsControl) {
			return nil, "", paHTTPFailure(pa.StaleVersion, "project_label")
		}
		grid, _ := s.icons.Resolve(row.Path)
		items = append(items, contract.ProjectAssistantProjectOption{Label: label, Icon: wireIcon(grid),
			Target: contract.ProjectAssistantProjectSelection{MachineID: machine, ProjectID: id}})
	}
	// Include every registered metadata field, even one not displayed as a
	// label. Removing/re-registering or changing canonical metadata expires a
	// cursor without claiming that the selected directory remains usable.
	metadata, err := json.Marshal(rows)
	if err != nil {
		return nil, "", paHTTPFailure(pa.StaleVersion, "project_registry")
	}
	revision := sha256.Sum256(metadata)
	return items, hex.EncodeToString(revision[:]), nil
}

func paTargetsBinding(authority app.Authority, revision string) ([sha256.Size]byte, error) {
	// Observation time changes on each fresh read; every authorization and
	// source-policy field remains part of the binding and publication fence.
	authority.Source.ObservedAt = time.Time{}
	encoded, err := json.Marshal(struct {
		Version   string
		Authority app.Authority
		Revision  string
	}{"project-assistant-targets.v1", authority, revision})
	return sha256.Sum256(encoded), err
}

func paTargetsCursor(key []byte, binding [sha256.Size]byte, after string) string {
	data := append(append([]byte{}, binding[:]...), []byte(after)...)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("project-assistant-targets.v1\x00"))
	_, _ = mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(append(data, mac.Sum(nil)...))
}

func paTargetsStart(cursor string, key []byte, binding [sha256.Size]byte, items []contract.ProjectAssistantProjectOption) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	// Fixed cryptographic framing, not a new configurable capacity bound. Test
	// the encoded length before allocating from an untrusted query value.
	size := sha256.Size*2 + len(projects.PlaceID(""))
	if len(cursor) != base64.RawURLEncoding.EncodedLen(size) {
		return 0, paHTTPFailure(pa.CursorExpired, "projects_cursor")
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil || len(data) != size {
		return 0, paHTTPFailure(pa.CursorExpired, "projects_cursor")
	}
	after := string(data[sha256.Size : size-sha256.Size])
	if !hmac.Equal([]byte(cursor), []byte(paTargetsCursor(key, binding, after))) {
		return 0, paHTTPFailure(pa.CursorExpired, "projects_cursor")
	}
	for i, item := range items {
		if item.Target.ProjectID == after && i+1 < len(items) {
			return i + 1, nil
		}
	}
	// A cursor always names a real nonterminal boundary. Never fabricate an
	// empty page for a deleted, invented or final row.
	return 0, paHTTPFailure(pa.CursorExpired, "projects_cursor")
}

func paTargetsEncodePage(response contract.ProjectAssistantProjectsResponse) ([]byte, bool, error) {
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, false, err
	}
	encoded = append(encoded, '\n') // the HTTP JSON encoder's complete body
	// machinePlan replies on read:<request>. Existing requestName accepts at
	// most 128 printable bytes; '<' has the maximum JSON expansion per byte.
	// This derives an upper bound from that existing rule, not an extra limit.
	// The paired Cloud budget test fails if requestName or machinePlan changes.
	replyName := "read:" + strings.Repeat("<", 128)
	cloudSize, err := cloudops.ProjectAssistantReadReplySize(replyName, http.StatusOK, encoded)
	if err != nil {
		return nil, false, err
	}
	fits := len(encoded) <= cloudops.ReadPlaintextBytesLimit && cloudSize <= cloudops.ReadPlaintextBytesLimit
	return encoded, fits, nil
}

func (s *Server) projectAssistantTargetsReply(r *http.Request) ([]byte, error) {
	if r.Method != http.MethodGet || r.URL.Path != "/v1/project-assistant-targets" {
		return nil, paHTTPFailure(pa.Invalid, "route")
	}
	query, err := paHTTPQuery(r, "machine_id", "cursor", "page_size")
	if err != nil {
		return nil, err
	}
	size, err := paHTTPNumber(query, "page_size")
	if err != nil || size < 1 || size > pa.MaxPageRows {
		return nil, paHTTPFailure(pa.Invalid, "page_size")
	}
	machine := query.Get("machine_id")
	authority, err := s.projectAssistantTargetsAuthority(r.Context(), machine)
	if err != nil {
		return nil, err
	}
	items, revision, err := s.projectAssistantTargetCatalog(machine)
	if err != nil {
		return nil, err
	}
	binding, err := paTargetsBinding(authority, revision)
	if err != nil {
		return nil, err
	}
	// Read an existing daemon secret only. A metadata read creates no token,
	// cursor cache or per-principal state; rotating the token expires cursors.
	secret, err := s.gate().files.ReadLocalToken()
	if err != nil || secret == "" {
		return nil, paHTTPFailure(pa.Revoked, "cursor_key")
	}
	start, err := paTargetsStart(query.Get("cursor"), []byte(secret), binding, items)
	if err != nil {
		return nil, err
	}
	source, err := paWireSourceStatus(authority.Source)
	if err != nil {
		return nil, err
	}
	response := contract.ProjectAssistantProjectsResponse{ContractVersion: pa.ContractVersion, Source: source,
		Agents:   []contract.ProjectAssistantAgent{contract.ProjectAssistantAgentClaude, contract.ProjectAssistantAgentCodex},
		Revision: revision, Items: []contract.ProjectAssistantProjectOption{}}
	var encoded []byte
	if start == len(items) {
		var fits bool
		encoded, fits, err = paTargetsEncodePage(response)
		if err != nil {
			return nil, err
		}
		if !fits {
			return nil, paHTTPFailure(pa.CapacityFull, "projects_reply")
		}
	}
	for end := start + 1; end <= len(items) && end-start <= int(size); end++ {
		response.Items = items[start:end]
		response.HasMore = end < len(items)
		response.NextCursor = ""
		if response.HasMore {
			response.NextCursor = paTargetsCursor([]byte(secret), binding, items[end-1].Target.ProjectID)
		}
		candidate, fits, err := paTargetsEncodePage(response)
		if err != nil {
			return nil, err
		}
		if fits {
			encoded = candidate
		}
		// Check all allowed prefixes: the final catalog row removes the
		// cursor and can fit even when the preceding prefix narrowly cannot.
	}
	if encoded == nil {
		return nil, paHTTPFailure(pa.CapacityFull, "projects_reply")
	}
	if s.projectAssistantTargetsBeforePublish != nil {
		s.projectAssistantTargetsBeforePublish()
	}
	_, currentRevision, err := s.projectAssistantTargetCatalog(machine)
	if err != nil {
		return nil, err
	}
	if revision != currentRevision {
		return nil, paHTTPFailure(pa.CursorExpired, "project_registry_changed")
	}
	current, err := s.projectAssistantTargetsAuthority(r.Context(), machine)
	if err != nil {
		return nil, err
	}
	currentBinding, err := paTargetsBinding(current, currentRevision)
	if err != nil {
		return nil, err
	}
	if currentBinding != binding {
		return nil, paHTTPFailure(pa.Revoked, "source_authority_changed")
	}
	return encoded, r.Context().Err()
}
