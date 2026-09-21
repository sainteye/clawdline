package orchestrator

import (
	"fmt"
	"strings"
	"time"
)

// readAssistantQuota takes the evidence this dispatch was decided with. Quota
// is advice, never admission: every outcome below is a warning and the caller
// continues through the ordinary dispatch path.
func (b *Broker) readAssistantQuota(selected string) (*AssistantQuotaDecision, []Warning) {
	now := b.now()
	decision := &AssistantQuotaDecision{ReadAt: now, Assistants: []AssistantQuotaSnapshot{}}
	if b.AssistantQuotas == nil {
		decision.ReadError = "the assistant quota reader is not configured"
		return decision, quotaWarnings(selected, decision)
	}

	rows, err := b.AssistantQuotas(now)
	if err != nil {
		decision.ReadError = err.Error()
		return decision, quotaWarnings(selected, decision)
	}
	decision.Assistants = cloneQuotaSnapshots(rows)
	return decision, quotaWarnings(selected, decision)
}

func cloneQuotaSnapshots(in []AssistantQuotaSnapshot) []AssistantQuotaSnapshot {
	out := make([]AssistantQuotaSnapshot, len(in))
	for i, row := range in {
		out[i] = row
		out[i].Windows = append([]AssistantQuotaWindow{}, row.Windows...)
	}
	return out
}

// quotaWarnings explains evidence at the moment it can still change a root's
// next dispatch. A constrained account does not stop this one: an explicit
// assistant may have been chosen for reasons quota cannot see.
func quotaWarnings(selected string, decision *AssistantQuotaDecision) []Warning {
	if decision == nil {
		return nil
	}
	if decision.ReadError != "" {
		return []Warning{{
			Code: "assistant_quota_unreadable",
			Message: "The broker could not read assistant quota at dispatch (" + decision.ReadError +
				"). The dispatch continued because quota never refuses work.",
		}}
	}

	var chosen *AssistantQuotaSnapshot
	for i := range decision.Assistants {
		if decision.Assistants[i].ID == selected {
			chosen = &decision.Assistants[i]
			break
		}
	}
	if chosen == nil {
		return []Warning{{
			Code: "assistant_quota_unreadable",
			Message: "The quota reading at dispatch returned no row for the selected assistant " + selected +
				". The dispatch continued because quota never refuses work.",
		}}
	}

	warnings := []Warning{}
	if chosen.Availability == "low" || chosen.Availability == "exhausted" {
		message := "The brief selected " + quotaEvidence(*chosen) + "."
		code := "assistant_quota_constrained"
		if other := betterInstalledAlternative(selected, chosen.Availability, decision.Assistants); other != nil {
			code = "assistant_quota_choice"
			message += " The installed alternative was better: " + quotaEvidence(*other) + "."
		}
		message += " The dispatch continued because quota never refuses work."
		warnings = append(warnings, Warning{Code: code, Message: message})
	}

	unknown := []string{}
	for _, row := range decision.Assistants {
		// An uninstalled alternative is not a candidate. The selected assistant
		// still needs an honest warning: its launcher will make the separate
		// capability decision later.
		if row.Availability == "unknown" && (row.ID == selected || row.Installed) {
			unknown = append(unknown, quotaEvidence(row))
		}
	}
	if len(unknown) > 0 {
		warnings = append(warnings, Warning{
			Code: "assistant_quota_unknown",
			Message: "Quota could not be established at dispatch for " + strings.Join(unknown, "; ") +
				". The dispatch continued because quota never refuses work.",
		})
	}
	return warnings
}

func betterInstalledAlternative(selected, availability string, rows []AssistantQuotaSnapshot) *AssistantQuotaSnapshot {
	selectedRank := quotaRank(availability)
	var best *AssistantQuotaSnapshot
	for i := range rows {
		row := &rows[i]
		if row.ID == selected || !row.Installed || quotaRank(row.Availability) <= selectedRank {
			continue
		}
		if best == nil || quotaRank(row.Availability) > quotaRank(best.Availability) {
			best = row
		}
	}
	return best
}

func quotaRank(availability string) int {
	switch availability {
	case "exhausted":
		return 0
	case "low":
		return 1
	case "ok":
		return 2
	default:
		return -1
	}
}

func quotaEvidence(row AssistantQuotaSnapshot) string {
	label := strings.TrimSpace(row.Label)
	if label == "" {
		label = row.ID
	}
	parts := []string{"availability=" + row.Availability}
	if detail := strings.TrimSpace(row.Detail); detail != "" {
		parts = append(parts, detail)
	}
	if row.ObservedAt != nil {
		parts = append(parts, "observed_at="+time.Unix(*row.ObservedAt, 0).UTC().Format(time.RFC3339))
	}
	if row.AgeSeconds != nil {
		parts = append(parts, fmt.Sprintf("age_seconds=%d", *row.AgeSeconds))
	}
	if row.FreshForSeconds != nil {
		parts = append(parts, fmt.Sprintf("fresh_for_seconds=%d", *row.FreshForSeconds))
	}
	if row.Stale {
		parts = append(parts, "stale=true")
	}
	return label + " (" + strings.Join(parts, ", ") + ")"
}
