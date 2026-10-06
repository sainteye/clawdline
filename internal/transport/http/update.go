package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/adapters/release/updater"
	"github.com/sainteye/clawdline/internal/adapters/updatecheck"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// AutoApplySetting is the per-machine opt-in for installing a newer stable
// release without being asked (docs/updates.md). Off unless set.
const AutoApplySetting = "update_auto_apply"

// updateApplyBodyLimit bounds POST /v1/update/apply's body.
const updateApplyBodyLimit = 4096

// updateRoute is GET /v1/update: whether this machine trails the latest
// build (docs/updates.md). A release install compares release versions from
// the signed manifest and never reads the hosted BUILD.json; anything else
// compares commits with it, as before. It answers from the last background
// read and never waits on the network. Like /v1/capacity it is for any
// paired device and says nothing about where this daemon keeps its state.
func (s *Server) updateRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "Whether this machine is up to date is read with GET.")
		return
	}
	writeJSON(w, s.updateStatus())
}

func (s *Server) updateStatus() contract.UpdateStatus {
	s.updateChecker()
	if s.releaseUpdate != nil {
		return s.releaseUpdate.Status()
	}
	st := s.update.Status()
	st.InstallKind = s.installKind()
	return st
}

// installKind is what this daemon's executable is, for a daemon that is not
// a release install.
func (s *Server) installKind() contract.UpdateInstallKind {
	exe, err := os.Executable()
	if err != nil {
		return contract.UpdateInstallKindNone
	}
	l, err := install.DefaultLayout()
	if err != nil {
		return contract.UpdateInstallKindNone
	}
	return contract.UpdateInstallKind(l.KindOf(exe))
}

// updateApplyRoute is POST /v1/update/apply: install a release on a release
// install. The refusals answer at once with a code (409, or 400 for a body
// that is not a request); an accepted update answers 202 with the status,
// and the rest of it is read from GET /v1/update's `apply`.
func (s *Server) updateApplyRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "an update is started with POST")
		return
	}
	if !machineAuthed(r) && !maySend(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This device may read, and not send.")
		return
	}
	var req contract.UpdateApplyRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, updateApplyBodyLimit+1))
	if err != nil || len(body) > updateApplyBodyLimit {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not an update request")
		return
	}
	if len(body) > 0 {
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not an update request: "+err.Error())
			return
		}
	}
	if req.Force && req.Version == "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "force needs the version it installs")
		return
	}
	s.updateChecker()
	if s.releaseUpdate == nil {
		writeRefusal(w, http.StatusConflict, updater.CodeNotAReleaseInstall,
			"this daemon is not a release install; a source build is updated from its checkout (docs/updates.md)")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*updater.FetchTimeoutSecondsLimit*time.Second)
	defer cancel()
	if _, err := s.releaseUpdate.Start(ctx, updater.Request{Version: req.Version, Force: req.Force}); err != nil {
		var ue *updater.Error
		var re *release.Error
		switch {
		case errors.As(err, &ue):
			status := http.StatusConflict
			if ue.Code == updater.CodeReleaseUnreachable {
				status = http.StatusBadGateway
			}
			writeRefusal(w, status, ue.Code, ue.Detail)
		case errors.As(err, &re):
			writeRefusal(w, http.StatusBadGateway, re.Code, re.Detail)
		default:
			writeRefusal(w, http.StatusInternalServerError, "update_failed", err.Error())
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(s.releaseUpdate.Status())
}

// runUpdateCheck is the background check behind GET /v1/update until ctx
// ends: the release check on a release install, which never asks the hosted
// BUILD.json, and the hosted BUILD.json check otherwise.
func (s *Server) runUpdateCheck(ctx context.Context) {
	s.updateChecker()
	if s.releaseUpdate != nil {
		s.releaseUpdate.Run(ctx)
		return
	}
	s.update.Run(ctx)
}

// updateChecker makes, on first use, the one update source this daemon
// answers from: the release updater when it runs from a release install,
// the hosted BUILD.json check otherwise.
func (s *Server) updateChecker() *updatecheck.Checker {
	s.updateOnce.Do(func() {
		if s.update == nil {
			s.update = updatecheck.New(WebRoot())
		}
		if s.releaseUpdate != nil {
			return
		}
		exe, err := os.Executable()
		if err != nil {
			return
		}
		env, err := updater.DefaultEnv(s.cfg.Dir)
		if err != nil {
			return
		}
		if _, kind := updater.Running(env.Layout, exe); kind != install.KindRelease {
			return
		}
		d := updater.NewDaemon(env, exe)
		d.AutoApply = s.autoApplyOn
		d.Busy = s.sessionsBusy
		d.Logf = log.Printf
		s.releaseUpdate = d
	})
	return s.update
}

// autoApplyOn reads the per-machine setting. An unreadable settings file is
// off: an update is never started on a guess.
func (s *Server) autoApplyOn() bool {
	v, err := nextconfig.Open(s.cfg.Dir).Read()
	if err != nil {
		return false
	}
	on, ok := v.Bool(AutoApplySetting)
	return ok && on
}

// sessionsBusy says whether an assistant session on this machine is working.
// A reading that is incomplete, or a session whose state is unknown, counts
// as busy: auto-apply restarts the daemon, and waits rather than guess.
func (s *Server) sessionsBusy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	inv := s.reading(ctx)
	if !inv.Complete {
		return true
	}
	for _, item := range inv.Sessions {
		if !item.IsAssistant() {
			continue
		}
		if item.State == session.StateWorking || item.State == session.StateUnknown {
			return true
		}
	}
	return false
}
