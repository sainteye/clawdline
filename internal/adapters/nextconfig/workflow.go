package nextconfig

import (
	"encoding/json"
	"errors"
)

// WorkflowProfile resolves old consent without writing settings during reads.
func WorkflowProfile(v Values) (string, error) {
	if raw, present := v.Raw["workflow_profile"]; present {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", errors.New("invalid_workflow_profile")
		}
		id, ok := value.(string)
		if !ok {
			return "", errors.New("invalid_workflow_profile")
		}
		switch id {
		case "clawdline", "superpowers", "poteto":
			return id, nil
		default:
			return "", errors.New("invalid_workflow_profile")
		}
	}
	if raw, present := v.Raw["workflow_skills_enabled"]; present {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", errors.New("invalid_workflow_legacy_setting")
		}
		on, ok := value.(bool)
		if !ok {
			return "", errors.New("invalid_workflow_legacy_setting")
		}
		if on {
			return "superpowers", nil
		}
	}
	return "clawdline", nil
}

// workflowChanges keeps old and new writers consistent in the same atomic write.
// Copying the map preserves the caller's request and its idempotency identity.
func workflowChanges(changes map[string]any) map[string]any {
	_, profile := changes["workflow_profile"]
	_, legacy := changes["workflow_skills_enabled"]
	if !profile && !legacy {
		return changes
	}
	out := make(map[string]any, len(changes))
	for key, value := range changes {
		out[key] = value
	}
	if id, ok := changes["workflow_profile"].(string); ok {
		out["workflow_skills_enabled"] = id == "superpowers"
	} else if !profile {
		if on, ok := changes["workflow_skills_enabled"].(bool); ok {
			out["workflow_profile"] = "clawdline"
			if on {
				out["workflow_profile"] = "superpowers"
			}
		}
	}
	return out
}
