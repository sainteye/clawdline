package http

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/domain/notificationrule"
)

type notificationRuleChange struct {
	Action string                 `json:"action"`
	Rule   *notificationrule.Rule `json:"rule,omitempty"`
	ID     string                 `json:"id,omitempty"`
	Text   string                 `json:"text,omitempty"`
	Key    string                 `json:"key,omitempty"`
	Value  *bool                  `json:"value,omitempty"`
}

var notificationPreferenceKeys = map[string]bool{
	"push_on_model_capacity": true, "push_on_waiting": true, "orchestrator_agent_notify": true,
	"push_on_board_decision": true, "push_on_board_done": true,
	"push_on_schedule_failure": true, "push_on_capacity": true,
	"push_on_dead_letter": true, "push_on_app_release_success": true,
	"push_on_cloud_release_success": true, "push_on_release_failure": true,
	"push_on_deploy": true,
}

func notificationRulesAnswer(v nextconfig.Values) (map[string]any, error) {
	rules, err := notificationRules(v)
	if err != nil {
		return nil, err
	}
	preferences := map[string]bool{}
	for key := range notificationPreferenceKeys {
		if value, ok := v.Bool(key); ok {
			preferences[key] = value
		}
	}
	return map[string]any{"rules": rules, "preferences": preferences}, nil
}

func notificationRules(v nextconfig.Values) ([]notificationrule.Rule, error) {
	return notificationrule.Decode(v.Raw[notificationrule.FileKey])
}

func (s *Server) notificationRulesRoute(w http.ResponseWriter, r *http.Request) {
	f := s.settingsFile()
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		v, err := f.Read()
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		answer, err := notificationRulesAnswer(v)
		if err != nil {
			writeRawRefusal(w, http.StatusConflict, "notification_rules_invalid", "The saved notification rules are invalid.")
			return
		}
		writeJSON(w, answer)
	case http.MethodPost:
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "a notification rule change is application/json")
			return
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
		dec.DisallowUnknownFields()
		var change notificationRuleChange
		if err := dec.Decode(&change); err != nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "invalid notification rule change")
			return
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "invalid notification rule change")
			return
		}
		if change.Action == "preview" {
			if change.Rule != nil || change.ID != "" || change.Key != "" || change.Value != nil {
				writeRefusal(w, http.StatusBadRequest, "invalid_notification_rule", "The notification situation is invalid.")
				return
			}
			preview, err := notificationrule.ParseSituation(change.Text)
			if err != nil {
				writeRawRefusal(w, http.StatusUnprocessableEntity, "notification_situation_unsupported", "This situation cannot send a notification yet. Try: When a Session needs my answer, notify me.")
				return
			}
			writeJSON(w, map[string]any{"preview": preview})
			return
		}
		if (change.Action == "create" || change.Action == "update") && (change.Rule == nil || !change.Rule.Valid() || change.ID != "" || change.Key != "" || change.Value != nil) ||
			change.Action == "delete" && (change.ID == "" || change.Rule != nil || change.Key != "" || change.Value != nil) ||
			change.Action == "preference" && (!notificationPreferenceKeys[change.Key] || change.Value == nil || change.Rule != nil || change.ID != "") ||
			change.Action != "create" && change.Action != "update" && change.Action != "delete" && change.Action != "preference" {
			writeRefusal(w, http.StatusBadRequest, "invalid_notification_rule", "The notification rule condition is invalid.")
			return
		}
		if change.Text != "" {
			writeRefusal(w, http.StatusBadRequest, "invalid_notification_rule", "Text belongs to a preview request.")
			return
		}
		var invalid = errors.New("invalid rule change")
		v, err := f.Change(func(current nextconfig.Values) (map[string]any, error) {
			rules, err := notificationRules(current)
			if err != nil {
				return nil, notificationrule.ErrInvalid
			}
			switch change.Action {
			case "preference":
				return map[string]any{change.Key: *change.Value}, nil
			case "create":
				for _, old := range rules {
					if old.ID == change.Rule.ID {
						if old == *change.Rule {
							return map[string]any{notificationrule.FileKey: rules}, nil
						}
						return nil, invalid
					}
				}
				if len(rules) >= notificationrule.MaximumRules {
					return nil, invalid
				}
				rules = append(rules, *change.Rule)
			case "update":
				found := false
				for i := range rules {
					if rules[i].ID == change.Rule.ID {
						rules[i] = *change.Rule
						found = true
						break
					}
				}
				if !found {
					return nil, invalid
				}
			case "delete":
				for i := range rules {
					if rules[i].ID == change.ID {
						rules = append(rules[:i], rules[i+1:]...)
						break
					}
				}
			}
			return map[string]any{notificationrule.FileKey: rules}, nil
		})
		if err != nil {
			if errors.Is(err, notificationrule.ErrInvalid) {
				writeRawRefusal(w, http.StatusConflict, "notification_rules_invalid", "The saved notification rules are invalid.")
				return
			}
			if errors.Is(err, invalid) {
				writeRawRefusal(w, http.StatusConflict, "notification_rule_conflict", "The notification rule changed or the limit was reached.")
				return
			}
			writeSettingsFailure(w, f, err)
			return
		}
		answer, _ := notificationRulesAnswer(v)
		writeJSON(w, answer)
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST")
	}
}
