package http

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/sainteye/clawdline-go/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// settingsFiles holds one settings file per state directory, so every request
// for the same file waits on the same lock. A test server with a directory of
// its own gets a file of its own.
var settingsFiles sync.Map // dir -> *nextconfig.File

// settingsFile is this daemon's own config.json. The Swift app's directory is
// refused by every name it may go by: the one swiftstore reads, and the fixed
// one that app always uses whatever this process's environment says.
func (s *Server) settingsFile() *nextconfig.File {
	if f, ok := settingsFiles.Load(s.cfg.Dir); ok {
		return f.(*nextconfig.File)
	}
	foreign := []string{swiftstore.Dir()}
	if home, err := os.UserHomeDir(); err == nil {
		foreign = append(foreign, filepath.Join(home, ".config", "clawdline"))
	}
	f, _ := settingsFiles.LoadOrStore(s.cfg.Dir, nextconfig.Open(s.cfg.Dir, foreign...))
	return f.(*nextconfig.File)
}

// settingsRoute reads or changes this app's settings file.
//
// A write must say it is JSON. That is not pedantry: a form on any web page can
// post plain text to a loopback port without asking, but it cannot send
// `application/json` without the browser first asking this daemon, which does
// not answer yes. The hotkey is a global keyboard grab; it is not something a
// page elsewhere gets to set.
func (s *Server) settingsRoute(w http.ResponseWriter, r *http.Request) {
	f := s.settingsFile()
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		v, err := f.Read()
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		writeJSON(w, settingsSnapshot(f, v))
	case http.MethodPost:
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
				"a settings write is application/json")
			return
		}
		var body contract.SettingsRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a settings change")
			return
		}
		changes := map[string]any{}
		if body.Hotkey != nil {
			if !nextconfig.ValidHotkey(*body.Hotkey) {
				writeRefusal(w, http.StatusBadRequest, "invalid_hotkey",
					"not a combination the shell can register: "+*body.Hotkey)
				return
			}
			changes["hotkey"] = *body.Hotkey
		}
		if body.ScopeApp != nil {
			if !nextconfig.ValidScope(*body.ScopeApp) {
				writeRefusal(w, http.StatusBadRequest, "invalid_scope",
					"scope_app is bundle identifiers separated by commas")
				return
			}
			changes["scope_app"] = *body.ScopeApp
		}
		var (
			v   nextconfig.Values
			err error
		)
		// Nothing to change is a read, not a write: the file is not touched and
		// its modification time does not move.
		if len(changes) == 0 {
			v, err = f.Read()
		} else {
			v, err = f.Set(changes)
		}
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		writeJSON(w, settingsSnapshot(f, v))
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST")
	}
}

func settingsSnapshot(f *nextconfig.File, v nextconfig.Values) contract.SettingsSnapshot {
	out := contract.SettingsSnapshot{Exists: v.Exists, Path: f.Path()}
	if s, ok := v.String("hotkey"); ok {
		out.Hotkey = &s
	}
	if s, ok := v.String("scope_app"); ok {
		out.ScopeApp = &s
	}
	return out
}

// writeSettingsFailure names which of the failures this was. A file somebody
// broke by hand is theirs to fix and says where it is; the rest are this
// daemon's.
func writeSettingsFailure(w http.ResponseWriter, f *nextconfig.File, err error) {
	switch {
	case errors.Is(err, nextconfig.ErrNotObject):
		writeRefusal(w, http.StatusConflict, "settings_file_invalid", f.Path()+" is not a JSON object; it was left as it is")
	case errors.Is(err, nextconfig.ErrForeignDir):
		writeRefusal(w, http.StatusInternalServerError, "settings_dir_refused", err.Error())
	default:
		writeRefusal(w, http.StatusInternalServerError, "settings_unavailable", err.Error())
	}
}
