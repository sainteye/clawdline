package http

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
)

// settingsFiles holds one settings file per state directory, so every request
// for the same file waits on the same lock. A test server with a directory of
// its own gets a file of its own.
var settingsFiles sync.Map // dir -> *nextconfig.File

// A paired browser may change these machine settings through the daemon's own
// console. Native-only input bar controls and the global keyboard grab stay on
// the shell's local-token route.
var browserSettings = map[string]bool{
	"language": true, "terminal": true,
	"mascot": true, "notch": true, "voice_language": true,
	"codex_auto_name": true, "auto_name_assistant": true,
	"remote": true, "tailscale_hostname": true, "cloudflare_tunnel_retired": true,
	"push_on_delivery": true, "push_on_fanout": true,
	"smart_notifications": true, "push_on_deploy": true,
	"push_on_model_capacity": true, "push_on_waiting": true, "push_on_board_decision": true, "push_on_board_done": true,
	"push_on_schedule_failure": true, "push_on_capacity": true, "push_on_dead_letter": true,
	"push_on_app_release_success": true, "push_on_cloud_release_success": true,
	"push_on_release_failure":   true,
	"orchestrator_agent_notify": true, "orchestrator_enabled": true,
	"orchestrator_max_children": true, "orchestrator_permission": true,
	"orchestrator_notify_root": true, "orchestrator_child_linger": true,
	"claude_auto_compact_window": true, "iterm_scan": true, "iterm_integration": true, "session_auto_discovery": true,
	"setup_mode": true, "setup_completed_at": true, "setup_app_device": true, "setup_skipped": true,
}

// notificationSettings is the machine-wide event policy. A device's ability
// to subscribe is independent of these switches.
var notificationSettings = map[string]bool{
	"push_on_model_capacity": true, "push_on_waiting": true, "orchestrator_agent_notify": true,
	"push_on_board_decision": true, "push_on_board_done": true,
	"push_on_schedule_failure": true, "push_on_capacity": true,
	"push_on_dead_letter": true, "push_on_app_release_success": true,
	"push_on_cloud_release_success": true, "push_on_release_failure": true,
}

// settingsRequestBodyLimit bounds both the full local settings write and the
// two-key route carried to Cloud. The latter has the same bound before and
// after transport (internal/app/cloudops.defaultModelsCloudBodyLimit).
const settingsRequestBodyLimit = 64 << 10

// settingsFile is this daemon's own config.json. The retired Swift app's
// directory is refused, spelled as the fixed path that app always used.
func (s *Server) settingsFile() *nextconfig.File {
	if f, ok := settingsFiles.Load(s.cfg.Dir); ok {
		return f.(*nextconfig.File)
	}
	f, _ := settingsFiles.LoadOrStore(s.cfg.Dir, nextconfig.Open(s.cfg.Dir, swiftDirs()...))
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
	browser := routePath(r) == "/v1/settings/browser"
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
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
		if err := dec.Decode(&body); err != nil || body == nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a settings change")
			return
		}
		changes, refusal := settingsChanges(body)
		if refusal != nil {
			writeRawRefusal(w, http.StatusBadRequest, refusal.code, refusal.message)
			return
		}
		if browser {
			for name, value := range changes {
				// Dismissing the Cloudflare notice removes the retired
				// keys; the browser asked for cloudflare_tunnel_retired.
				if value == nextconfig.Remove {
					continue
				}
				if !browserSettings[name] {
					writeRawRefusal(w, http.StatusForbidden, "browser_setting_unavailable",
						"This setting is not available in the browser: "+name)
					return
				}
			}
		}
		// A key that turns on what this machine has no way to do is refused
		// by name, not stored for nothing to act on (capabilities.go, W7).
		if refusal := platformSettingRefusal(s.desktopHost().Capabilities(r.Context()), runtime.GOOS, changes); refusal != nil {
			writeRawRefusal(w, http.StatusNotImplemented, refusal.code, refusal.message)
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
		// The Tailscale address follows `remote` and its own name from this
		// very write, so turning either off refuses the next request.
		if tailscaleSettingChanged(changes) {
			s.gate().setTailscale(v)
		}
		_, scanChanged := changes["iterm_scan"]
		_, integrationChanged := changes["iterm_integration"]
		if scanChanged || integrationChanged {
			applyITermScan(v)
		}
		writeJSON(w, settingsSnapshot(f, v))
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST")
	}
}

// defaultModelsRoute is the narrow settings surface carried to a paired Cloud
// console. It deliberately does not reuse settingsRoute's GET response: a
// phone choosing the defaults for new Sessions has no reason to receive the
// hotkey, remote hostname, shell or any other machine setting.
func (s *Server) defaultModelsRoute(w http.ResponseWriter, r *http.Request) {
	f := s.settingsFile()
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		v, err := f.Read()
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		writeJSON(w, defaultModelsSnapshot(f, v))
	case http.MethodPost:
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
				"a default model write is application/json")
			return
		}
		var body map[string]json.RawMessage
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
		if err := dec.Decode(&body); err != nil || body == nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a default model change")
			return
		}
		for name := range body {
			if name != "codex_default_model" && name != "claude_default_model" && name != "codex_default_effort" && name != "antigravity_default_model" && name != "antigravity_default_effort" {
				writeRawRefusal(w, http.StatusBadRequest, "bad_request", "not a default model key: "+name)
				return
			}
		}
		changes, refusal := settingsChanges(body)
		if refusal != nil {
			writeRawRefusal(w, http.StatusBadRequest, refusal.code, refusal.message)
			return
		}
		var (
			v   nextconfig.Values
			err error
		)
		if len(changes) == 0 {
			v, err = f.Read()
		} else {
			v, err = f.Set(changes)
		}
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		writeJSON(w, defaultModelsSnapshot(f, v))
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST")
	}
}

// workGateSettingsRoute is the narrow settings surface carried to every
// console. It deliberately exposes only the two defaults a future successful
// Board assignment captures, never the hotkey, remote host, shell, or another
// machine setting.
func (s *Server) workGateSettingsRoute(w http.ResponseWriter, r *http.Request) {
	f := s.settingsFile()
	read := func() (contract.WorkGateSettingsSnapshot, error) {
		v, err := f.Read()
		if err != nil {
			return contract.WorkGateSettingsSnapshot{}, err
		}
		gates := v.WorkGates()
		return contract.WorkGateSettingsSnapshot{PlanningGate: gates.Planning, VerifyGate: gates.Verify}, nil
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		answer, err := read()
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		writeJSON(w, answer)
	case http.MethodPost:
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
				"a work gate settings write is application/json")
			return
		}
		var body map[string]json.RawMessage
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
		if err := dec.Decode(&body); err != nil || body == nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a work gate settings change")
			return
		}
		for name := range body {
			if name != "planning_gate" && name != "verify_gate" {
				writeRawRefusal(w, http.StatusBadRequest, "bad_request", "not a work gate setting key: "+name)
				return
			}
		}
		changes, refusal := settingsChanges(body)
		if refusal != nil {
			writeRawRefusal(w, http.StatusBadRequest, refusal.code, refusal.message)
			return
		}
		var err error
		if len(changes) == 0 {
			_, err = f.Read()
		} else {
			_, err = f.Set(changes)
		}
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		answer, err := read()
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		writeJSON(w, answer)
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST")
	}
}

// workflowSettingsRoute exposes only workflow defaults, discovery and iTerm2 consent.
func (s *Server) workflowSettingsRoute(w http.ResponseWriter, r *http.Request) {
	f := s.settingsFile()
	read := func() (contract.WorkflowSettingsSnapshot, error) {
		v, err := f.Read()
		if err != nil {
			return contract.WorkflowSettingsSnapshot{}, err
		}

		integration, _ := v.Bool("iterm_integration")
		return contract.WorkflowSettingsSnapshot{ItermIntegration: &integration, DispatchFullPermission: v.DispatchPermission() == "full", SessionAutoDiscovery: v.SessionAutoDiscovery()}, nil
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		answer, err := read()
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		writeJSON(w, answer)
	case http.MethodPost:
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
				"a workflow settings write is application/json")
			return
		}
		var body map[string]json.RawMessage
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
		if err := dec.Decode(&body); err != nil || body == nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a workflow settings change")
			return
		}
		for name := range body {
			if name != "dispatch_full_permission" && name != "session_auto_discovery" && name != "iterm_integration" {
				writeRawRefusal(w, http.StatusBadRequest, "bad_request", "not a workflow setting key: "+name)
				return
			}
		}
		if raw, exists := body["dispatch_full_permission"]; exists {
			if string(raw) != "null" {
				var on bool
				if err := json.Unmarshal(raw, &on); err != nil {
					writeRefusal(w, http.StatusBadRequest, "bad_request", "dispatch_full_permission must be a boolean")
					return
				}
				mode := "ask"
				if on {
					mode = "full"
				}
				body["orchestrator_permission"], _ = json.Marshal(mode)
			}
			delete(body, "dispatch_full_permission")
		}
		changes, refusal := settingsChanges(body)
		if refusal != nil {
			writeRawRefusal(w, http.StatusBadRequest, refusal.code, refusal.message)
			return
		}
		var err error
		var values nextconfig.Values
		if len(changes) == 0 {
			values, err = f.Read()
		} else {
			values, err = f.Set(changes)
		}
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		if _, changed := changes["iterm_integration"]; changed {
			applyITermScan(values)
		}
		answer, err := read()
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		writeJSON(w, answer)
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST")
	}
}

// notificationSettingsRoute exposes only event switches to a paired console.
// It never returns paths, remote settings, or browser subscription material.
func (s *Server) notificationSettingsRoute(w http.ResponseWriter, r *http.Request) {
	f := s.settingsFile()
	read := func() (map[string]json.RawMessage, error) {
		v, err := f.Read()
		if err != nil {
			return nil, err
		}
		answer := make(map[string]json.RawMessage)
		for key := range notificationSettings {
			if raw, ok := v.Raw[key]; ok {
				var valid bool
				if json.Unmarshal(raw, &valid) == nil && string(raw) != "null" {
					answer[key] = raw
				}
			}
		}
		return answer, nil
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		answer, err := read()
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		writeJSON(w, answer)
	case http.MethodPost:
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "a notification settings write is application/json")
			return
		}
		var body map[string]json.RawMessage
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
		if err := dec.Decode(&body); err != nil || body == nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a notification settings change")
			return
		}
		for key := range body {
			if !notificationSettings[key] {
				writeRawRefusal(w, http.StatusBadRequest, "bad_request", "not a notification setting key: "+key)
				return
			}
		}
		changes, refusal := settingsChanges(body)
		if refusal != nil {
			writeRawRefusal(w, http.StatusBadRequest, refusal.code, refusal.message)
			return
		}
		var err error
		if len(changes) > 0 {
			_, err = f.Set(changes)
		}
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		answer, err := read()
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		writeJSON(w, answer)
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST")
	}
}

type defaultModelsAnswer struct {
	Antigravity       *string                         `json:"antigravity_default_model"`
	AntigravityEffort *string                         `json:"antigravity_default_effort"`
	CodexEffort       *string                         `json:"codex_default_effort"`
	Codex             *string                         `json:"codex_default_model"`
	Claude            *string                         `json:"claude_default_model"`
	Models            map[string][]defaultModelOption `json:"models"`
}

type defaultModelOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

func defaultModelsSnapshot(f *nextconfig.File, v nextconfig.Values) defaultModelsAnswer {
	snapshot := settingsSnapshot(f, v)
	home, _ := os.UserHomeDir()
	models := map[string][]defaultModelOption{"codex": {}, "claude": {}, "antigravity": {}}
	for _, assistant := range []string{"codex", "claude"} {
		for _, model := range transcript.Models(home, assistant) {
			models[assistant] = append(models[assistant], defaultModelOption{Value: model.Command, Label: model.Name})
		}
	}
	// No installed native model-list source: an empty catalog means unknown.
	var nativeModel, nativeEffort *string
	if model, ok := v.String("antigravity_default_model"); ok && nextconfig.ValidDefaultModel(model) {
		nativeModel = &model
	}
	if effort, ok := v.String("antigravity_default_effort"); ok {
		nativeEffort = &effort
	}
	return defaultModelsAnswer{
		Antigravity: nativeModel, AntigravityEffort: nativeEffort,
		Codex: snapshot.CodexDefaultModel, CodexEffort: snapshot.CodexDefaultEffort, Claude: snapshot.ClaudeDefaultModel, Models: models,
	}
}

type settingsRefusal struct{ code, message string }

// tailscaleSettingChanged is whether a write touched what the gate admits the
// Tailscale address on.
func tailscaleSettingChanged(changes map[string]any) bool {
	_, remote := changes["remote"]
	_, name := changes["tailscale_hostname"]
	return remote || name
}

// settingsChanges turns a request body into the keys to write.
//
// A key the file does not set is refused rather than carried through, which is
// the schema's `additionalProperties: false` and also the only thing standing
// between this route and a page writing `status_dir`. A key whose value is null
// is left as it is, which is what the schema says a null means.
func settingsChanges(body map[string]json.RawMessage) (map[string]any, *settingsRefusal) {
	changes := map[string]any{}
	for name, raw := range body {
		// The built-in Cloudflare tunnel was removed; its keys are named in
		// the refusal rather than called unknown, so a script that still
		// writes them learns where remote access went.
		if nextconfig.RetiredCloudflareKey(name) {
			return nil, &settingsRefusal{"retired_setting", name + ": the built-in Cloudflare tunnel was removed; " +
				"reach this machine through Tailscale Serve (tailscale_hostname) or Clawdline Cloud"}
		}
		// The settings page's "Got it" under the retirement notice: the old
		// keys leave the file, and the notice with them.
		if name == "cloudflare_tunnel_retired" {
			if string(raw) != "false" {
				return nil, &settingsRefusal{"bad_request", "cloudflare_tunnel_retired is only ever written false, to dismiss the notice"}
			}
			for _, key := range nextconfig.RetiredCloudflareKeys {
				changes[key] = nextconfig.Remove
			}
			continue
		}
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
type settingsSnapshotWire struct {
	contract.SettingsSnapshot
	PlanningGate bool `json:"planning_gate"`
	VerifyGate   bool `json:"verify_gate"`
}

func settingsSnapshot(f *nextconfig.File, v nextconfig.Values) settingsSnapshotWire {
	gates := v.WorkGates()
	out := settingsSnapshotWire{SettingsSnapshot: contract.SettingsSnapshot{Exists: v.Exists, Path: f.Path()},
		PlanningGate: gates.Planning, VerifyGate: gates.Verify}
	value := reflect.ValueOf(&out.SettingsSnapshot).Elem()
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
	// A file from before the built-in Cloudflare tunnel was removed still
	// holds its configuration. It starts nothing; the flag lets the settings
	// page say why, once.
	out.CloudflareTunnelRetired = nextconfig.CloudflareTunnelRetired(v)
	return out
}

// writeSettingsFailure names which of the failures this was. A file somebody
// broke by hand is theirs to fix and says where it is; the rest are this
// daemon's.
func writeSettingsFailure(w http.ResponseWriter, f *nextconfig.File, err error) {
	switch {
	case errors.Is(err, nextconfig.ErrNotObject):
		writeRawRefusal(w, http.StatusConflict, "settings_file_invalid", f.Path()+" is not a JSON object; it was left as it is")
	case errors.Is(err, nextconfig.ErrForeignDir):
		writeRawRefusal(w, http.StatusInternalServerError, "settings_dir_refused", err.Error())
	default:
		writeRawRefusal(w, http.StatusInternalServerError, "settings_unavailable", err.Error())
	}
}

// applyITermScan requires explicit consent even for existing installations.
// The old scan key only controls polling after integration has been enabled.
func applyITermScan(v nextconfig.Values) {
	enabled, _ := v.Bool("iterm_integration")
	terminal.SetITermIntegration(enabled)
	on, ok := v.Bool("iterm_scan")
	terminal.SetITermScan(!ok || on)
}
