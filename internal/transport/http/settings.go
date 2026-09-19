package http

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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
//
// The body is read key by key against `nextconfig.Settables` rather than into
// the generated struct's thirty-six pointers. What each key accepts is a fact
// about the file — the shell on this platform and the shell on the next one
// have to agree with it — so it is stated once, where the file is, and this
// route is the same four lines however many rows the settings window grows.
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
		var body map[string]json.RawMessage
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		if err := dec.Decode(&body); err != nil || body == nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a settings change")
			return
		}
		changes, refusal := settingsChanges(body)
		if refusal != nil {
			writeRefusal(w, http.StatusBadRequest, refusal.code, refusal.message)
			return
		}
		// A key that turns on what this machine has no way to do is refused
		// by name, not stored for nothing to act on (capabilities.go, W7).
		if refusal := platformSettingRefusal(s.desktopHost().Capabilities(r.Context()), runtime.GOOS, changes); refusal != nil {
			writeRefusal(w, http.StatusNotImplemented, refusal.code, refusal.message)
			return
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

type settingsRefusal struct{ code, message string }

// settingsChanges turns a request body into the keys to write.
//
// A key the file does not set is refused rather than carried through, which is
// the schema's `additionalProperties: false` and also the only thing standing
// between this route and a page writing `status_dir`. A key whose value is null
// is left as it is, which is what the schema says a null means.
func settingsChanges(body map[string]json.RawMessage) (map[string]any, *settingsRefusal) {
	changes := map[string]any{}
	for name, raw := range body {
		key, known := nextconfig.SettableByName(name)
		if !known {
			return nil, &settingsRefusal{"bad_request", "not a key this app sets: " + name}
		}
		if string(raw) == "null" {
			continue
		}
		value, ok := settingsValue(key, raw)
		if !ok || !key.Allows(value) {
			return nil, &settingsRefusal{settingsRefusalCode(key), settingsRefusalMessage(key, raw)}
		}
		changes[name] = value
	}
	return changes, nil
}

// settingsValue decodes one value in the shape its key holds.
func settingsValue(key nextconfig.Settable, raw json.RawMessage) (any, bool) {
	switch key.Kind {
	case "string":
		var s string
		return s, json.Unmarshal(raw, &s) == nil
	case "bool":
		var b bool
		return b, json.Unmarshal(raw, &b) == nil
	case "number":
		var f float64
		return f, json.Unmarshal(raw, &f) == nil
	case "int":
		var i int64
		// A JSON number with a fraction is not an integer, and `json` would
		// take 1.5 for 1 without saying so.
		if err := json.Unmarshal(raw, &i); err != nil {
			return nil, false
		}
		var f float64
		if json.Unmarshal(raw, &f) == nil && f != float64(i) {
			return nil, false
		}
		return i, true
	}
	return nil, false
}

func settingsRefusalCode(key nextconfig.Settable) string {
	if key.Refusal != "" {
		return key.Refusal
	}
	return "invalid_" + key.Name
}

func settingsRefusalMessage(key nextconfig.Settable, raw json.RawMessage) string {
	said := string(raw)
	if len(said) > 120 {
		said = said[:120] + "…"
	}
	if key.Because != "" {
		return key.Name + ": " + key.Because + ", not " + said
	}
	switch key.Kind {
	case "bool":
		return key.Name + ": true or false, not " + said
	case "number", "int":
		return key.Name + ": a number this file accepts, not " + said
	}
	return key.Name + ": not a value this file accepts"
}

// settingsSnapshotKeys is the set of file keys the snapshot carries, so a hand
// edit that happens to have written `path` into the file cannot set the answer's
// own `path`.
var settingsSnapshotKeys = func() map[string]bool {
	out := map[string]bool{"on_state_change": true}
	for _, key := range nextconfig.Settables {
		out[key.Name] = true
	}
	return out
}()

// settingsSnapshot fills the generated answer from the file.
//
// By the json tag rather than field by field, so the answer cannot fall behind
// the schema it was generated from: a key added to `settings.schema.json` and
// to `Settables` is carried here the moment the types are regenerated. A value
// whose JSON is the wrong shape for its field is left out, which is what the
// schema's null means — the file says something this app does not read.
func settingsSnapshot(f *nextconfig.File, v nextconfig.Values) contract.SettingsSnapshot {
	out := contract.SettingsSnapshot{Exists: v.Exists, Path: f.Path()}
	value := reflect.ValueOf(&out).Elem()
	fields := value.Type()
	for i := 0; i < fields.NumField(); i++ {
		name := strings.Split(fields.Field(i).Tag.Get("json"), ",")[0]
		if !settingsSnapshotKeys[name] {
			continue
		}
		raw, ok := v.Raw[name]
		if !ok {
			continue
		}
		field := value.Field(i)
		if err := json.Unmarshal(raw, field.Addr().Interface()); err != nil {
			// A half-decoded slice is worse than an absent one.
			field.SetZero()
		}
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
