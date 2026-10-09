package http

// The local viewer is owned by the daemon. The CLI and DoorGate console both
// use this same line, signing sequence and replay window. A browser never
// receives a Cloud credential or machine content key.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
)

func (s *Server) viewerSettings() (adaptercloud.Settings, *cloudkeys.Files, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return adaptercloud.Settings{}, nil, err
	}
	foreign := filepath.Join(home, ".config", "clawdline")
	settings, err := adaptercloud.ReadSettings(nextconfig.Open(s.cfg.Dir, foreign))
	if err != nil {
		return adaptercloud.Settings{}, nil, err
	}
	keys, err := cloudkeys.Open(s.cfg.Dir, foreign)
	return settings, keys, err
}

// withViewer holds the service lock for an operation. This keeps a file change
// from replacing the line mid-read and prevents two callers using one reply
// name concurrently. New login or pairing files take effect on the next call.
func (s *Server) withViewer(ctx context.Context, use func(*adaptercloud.ViewerClient) (any, error)) (any, error) {
	s.viewerMu.Lock()
	defer s.viewerMu.Unlock()
	settings, keys, err := s.viewerSettings()
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, adaptercloud.ErrDisabled
	}
	identity, found, err := adaptercloud.NewViewerIdentityStore(keys.Dir()).Load()
	if err != nil {
		return nil, err
	}
	if !found || identity.APIBase != settings.APIBase {
		return nil, adaptercloud.ErrNoIdentity
	}
	signer, found, err := keys.ViewerKey()
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, adaptercloud.ErrNoIdentity
	}
	pins, err := adaptercloud.NewViewerPinStore(keys.Dir()).Load(identity)
	if err != nil {
		return nil, err
	}
	state, _ := json.Marshal(struct {
		Settings adaptercloud.Settings
		Identity adaptercloud.ViewerIdentity
		Key      string
		Pins     map[string]adaptercloud.ViewerMachinePin
	}{settings, identity, signer.Fingerprint(), pins})
	signature := fmt.Sprintf("%x", sha256.Sum256(state))
	if signature != s.viewerSignature || s.viewer == nil {
		client, err := adaptercloud.NewViewerClient(adaptercloud.ViewerClientOptions{
			Settings: settings, Identity: identity, Signer: signer,
			Fence: adaptercloud.NewFileFence(keys.Dir(), identity.DeviceID), Pins: pins,
		})
		if err != nil {
			return nil, err
		}
		if s.viewerCancel != nil {
			s.viewerCancel()
			s.viewer.Stop()
		}
		lineContext, cancel := context.WithCancel(context.Background())
		s.viewer, s.viewerSignature, s.viewerCancel = client, signature, cancel
		go func() { _ = client.Run(lineContext) }()
	}
	return use(s.viewer)
}

func writeViewerError(w http.ResponseWriter, err error) {
	code, status := "viewer_unavailable", http.StatusServiceUnavailable
	var refusal adaptercloud.ViewerRefusal
	var api *adaptercloud.APIError
	switch {
	case errors.As(err, &refusal):
		code = refusal.Code
		status = http.StatusConflict
		if refusal.Status == http.StatusForbidden || refusal.Status == http.StatusUnauthorized {
			status = refusal.Status
		}
	case errors.Is(err, adaptercloud.ErrDisabled):
		code, status = "cloud_disabled", http.StatusConflict
	case errors.Is(err, adaptercloud.ErrNoIdentity):
		code, status = "viewer_login_required", http.StatusConflict
	case errors.Is(err, adaptercloud.ErrViewerRevoked):
		code, status = "viewer_machine_revoked", http.StatusForbidden
	case errors.Is(err, adaptercloud.ErrViewerNotPaired):
		code, status = "viewer_machine_not_paired", http.StatusForbidden
	case errors.Is(err, adaptercloud.ErrViewerOffline):
		code = "viewer_machine_offline"
	case errors.Is(err, adaptercloud.ErrViewerStatusUnavailable):
		code = "viewer_status_unavailable"
	case errors.As(err, &api) && (api.Status == http.StatusUnauthorized || api.Status == http.StatusForbidden):
		code, status = "no_permission", http.StatusForbidden
		if api.Code == "revoked" {
			code = "viewer_authorization_revoked"
		}
	}
	writeAuthRefusal(w, status, code, code)
}

func viewerDestination(r *http.Request) adaptercloud.ViewerDestination {
	q := r.URL.Query()
	return adaptercloud.ViewerDestination{MachineID: q.Get("machine"), SessionID: q.Get("session"), ExecutionGeneration: q.Get("generation")}
}

func (s *Server) cloudViewerRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	path := r.URL.Path
	if path == "/v1/cloud/viewer/status" {
		if r.Method != http.MethodGet {
			writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "bad_request")
			return
		}
		settings, keys, err := s.viewerSettings()
		if err != nil {
			writeViewerError(w, err)
			return
		}
		identity, found, err := adaptercloud.NewViewerIdentityStore(keys.Dir()).Load()
		if err != nil {
			writeViewerError(w, err)
			return
		}
		writeJSON(w, map[string]any{"enabled": settings.Enabled, "authorized": found && identity.APIBase == settings.APIBase})
		return
	}
	if path == "/v1/cloud/viewer/actions" && r.Method != http.MethodPost ||
		path != "/v1/cloud/viewer/actions" && r.Method != http.MethodGet {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "bad_request")
		return
	}
	result, err := s.withViewer(ctx, func(client *adaptercloud.ViewerClient) (any, error) {
		switch path {
		case "/v1/cloud/viewer/machines":
			return client.MachineStates(ctx)
		case "/v1/cloud/viewer/sessions":
			if r.URL.Query().Get("machine") == "" {
				return nil, adaptercloud.ViewerRefusal{Code: "execution_target_required"}
			}
			return client.ReadMachine(ctx, r.URL.Query().Get("machine"))
		case "/v1/cloud/viewer/detail":
			if values, present := r.URL.Query()["before"]; present {
				if len(values) != 1 {
					return nil, adaptercloud.ViewerRefusal{Code: "bad_request"}
				}
				before, err := strconv.ParseInt(values[0], 10, 64)
				if err != nil || before <= 0 {
					return nil, adaptercloud.ViewerRefusal{Code: "bad_request"}
				}
				return client.ReadTranscriptPage(ctx, viewerDestination(r), before)
			}
			return client.ReadDetail(ctx, viewerDestination(r))
		case "/v1/cloud/viewer/skills":
			return client.ReadSkills(ctx, viewerDestination(r))
		case "/v1/cloud/viewer/actions":
			var input adaptercloud.ViewerMutation
			r.Body = http.MaxBytesReader(w, r.Body, sendBodyLimit)
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					return nil, adaptercloud.ViewerRefusal{Code: "image_too_large"}
				}
				return nil, adaptercloud.ViewerRefusal{Code: "bad_request"}
			}
			if decoder.Decode(&struct{}{}) != io.EOF {
				return nil, adaptercloud.ViewerRefusal{Code: "bad_request"}
			}
			return client.Mutate(ctx, input)
		case "/v1/cloud/viewer/receipts":
			return client.ReadReceipt(ctx, viewerDestination(r), r.URL.Query().Get("action"),
				r.URL.Query().Get("request"), r.URL.Query().Get("query"))
		default:
			return nil, adaptercloud.ViewerRefusal{Code: "bad_request"}
		}
	})
	if err != nil {
		writeViewerError(w, err)
		return
	}
	writeJSON(w, result)
}
