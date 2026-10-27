package http

// workV2CardDescriptionRunesLimit is the Board card's readable description preview.
// The complete description is retained by the existing one-item read.
const workV2CardDescriptionRunesLimit = 200

// workV2CardOf is an allowlist: new detail fields do not silently enter lists.
// Full item, Session ownership and legacy list reads keep their original shapes.
func workV2CardOf(i workV2ItemWire) map[string]any {
	description := []rune(i.Description)
	if len(description) > workV2CardDescriptionRunesLimit {
		description = description[:workV2CardDescriptionRunesLimit]
	}
	out := map[string]any{
		"summary": true, "id": i.ID, "project": i.Project, "kind": i.Kind, "title": i.Title,
		"description": string(description), "acceptance_present": i.AcceptanceCriteria != "",
		"acceptance_version": i.AcceptanceVersion, "acceptance_digest": i.AcceptanceDigest,
		"phase": i.Phase, "condition": i.Condition, "user_action": i.UserAction, "area": i.Area,
		"deployment_policy": i.DeploymentPolicy, "review_required": i.ReviewRequired,
		"owner_session": i.OwnerSession, "created_by": i.CreatedBy,
		"created_at": i.CreatedAt, "updated_at": i.UpdatedAt, "closed_at": i.ClosedAt,
		"phase_entered_at": i.PhaseEnteredAt, "cycle": i.Cycle,
		"gate_snapshot_cycle": i.GateSnapshotCycle, "gate_snapshot_at": i.GateSnapshotAt,
		"planning_gate": i.PlanningGate, "verify_gate": i.VerifyGate, "version": i.Version,
	}
	if i.Verification != nil {
		out["verification"] = i.Verification
	}
	if i.CreatedVia != nil {
		out["created_via"] = i.CreatedVia
	}
	if i.ClaimedVia != nil {
		out["claimed_via"] = i.ClaimedVia
	}
	if i.DecisionID != "" {
		out["decision_id"] = i.DecisionID
	}
	if i.ParentID != "" {
		out["parent_id"] = i.ParentID
	}
	if i.DeploymentEvidence != "" {
		out["deployment_evidence"] = i.DeploymentEvidence
	}
	if i.NoDeploymentReason != "" {
		out["no_deployment_reason"] = i.NoDeploymentReason
	}
	if i.NoLandingReason != "" {
		out["no_landing_reason"] = i.NoLandingReason
	}
	return out
}
