package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

type remoteInspector interface {
	InspectPeer(context.Context, agenthandoff.Endpoint, string, string) ([]byte, error)
}

// PeerInventory uses the same Project IDs that dispatch resolves locally, and
// the same Session projection as the Console. It never returns a transcript.
func (s *Server) PeerInventory(ctx context.Context) ([]byte, error) {
	inventory := s.freshReading(ctx)
	catalog := s.workV2Projects(ctx)
	projects := make([]workV2ProjectWire, 0, len(catalog))
	for _, p := range catalog {
		projects = append(projects, p)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
	return json.Marshal(map[string]any{"machine_id": s.executionMachine(), "at": time.Now().Unix(),
		"projects": projects, "sessions": s.sessionsPayloadFrom(ctx, inventory), "assistants": s.availableAssistants()})
}

func (s *Server) remotePair(ctx context.Context, machine, id string) (string, error) {
	st, err := peerstore.Open(filepath.Join(s.cfg.Dir, cloudkeys.DirName))
	if err != nil {
		return "", errors.New("remote_store_unavailable")
	}
	defer st.Close()
	pairs, err := st.ListPairs(ctx)
	if err != nil {
		return "", errors.New("remote_store_unavailable")
	}
	selected := ""
	for _, p := range pairs {
		if p.SourceMachineID != s.executionMachine() || p.TargetMachineID != machine ||
			p.TargetSignature == "" || !p.RevokedAt.IsZero() || !time.Now().Before(p.ExpiresAt) || (id != "" && id != p.ID) {
			continue
		}
		if selected != "" {
			return "", errors.New("remote_pair_ambiguous")
		}
		selected = p.ID
	}
	if selected == "" {
		return "", errors.New("remote_pair_inactive")
	}
	return selected, nil
}

func (s *Server) remoteInspectRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST")
		return
	}
	var input struct {
		MachineID     string `json:"machine_id"`
		PairID        string `json:"pair_id"`
		SourceSession string `json:"source_session"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, agenthandoff.MaxBodyBytes))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || input.MachineID == "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "A target machine and current source Session are required.")
		return
	}
	source, err := s.currentRemoteSession(r.Context(), input.SourceSession)
	if err != nil {
		writeRefusal(w, http.StatusConflict, "remote_source_unverified", "The source Session is not current.")
		return
	}
	pair, err := s.remotePair(r.Context(), input.MachineID, input.PairID)
	if err != nil {
		writeRefusal(w, http.StatusConflict, err.Error(), "No unique active outgoing pair covers this machine.")
		return
	}
	line, ok := cloudLines.Load(s.cfg.Dir)
	inspector, supported := line.(remoteInspector)
	if !ok || !supported {
		writeRefusal(w, http.StatusServiceUnavailable, "remote_discovery_unavailable", "The machine inventory line is unavailable.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	body, err := inspector.InspectPeer(ctx, source, input.MachineID, pair)
	if err != nil {
		writeRefusal(w, http.StatusConflict, err.Error(), "The remote inventory could not be verified; no work was started.")
		return
	}
	var result map[string]any
	if json.Unmarshal(body, &result) != nil {
		writeRefusal(w, http.StatusBadGateway, "remote_inventory_invalid", "The machine returned an invalid inventory.")
		return
	}
	if code, _ := result["error"].(string); code != "" {
		writeRefusal(w, http.StatusServiceUnavailable, code, "The target could not read its inventory.")
		return
	}
	result["pair_id"] = pair
	result["source"] = source
	writeJSON(w, result)
}

func (s *Server) remoteInboxRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET")
		return
	}
	current, err := s.currentRemoteSession(r.Context(), r.URL.Query().Get("session_id"))
	if err != nil {
		writeRefusal(w, http.StatusConflict, "remote_source_unverified", "The current Session cannot be verified.")
		return
	}
	id := strings.TrimPrefix(routePath(r), "/v1/remote/inbox/")
	st, err := peerstore.Open(filepath.Join(s.cfg.Dir, cloudkeys.DirName))
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "remote_store_unavailable", "The inbox is unavailable.")
		return
	}
	defer st.Close()
	item, err := st.Inbox(r.Context(), id)
	if err != nil || item.Request.Target != current {
		writeRefusal(w, http.StatusNotFound, "peer_request_missing", "No message belongs to this Session execution.")
		return
	}
	writeJSON(w, item)
}
