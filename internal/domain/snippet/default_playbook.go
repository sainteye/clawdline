package snippet

// DefaultZeroReviewID identifies the editable starter Playbook on a machine.
// The seed ledger keeps a person's deletion from being undone on restart.
const DefaultZeroReviewID = "00000000-0000-4000-8000-000000000101"

const DefaultZeroReviewTitle = "Zero-Based Review"

const DefaultZeroReviewBody = `Run a zero-based review of this Project. Create an Epic to track it, then assign independent reviews from the relevant roles: product, UX, architecture, code quality, efficiency, documentation, and evidence.
Require evidence for every finding. Compare the current design with mature expert practice, and distinguish deliberate trade-offs from accumulated complexity.
Find confusing behavior, redundant steps or reads and writes, and missing capabilities.
Synthesize a report and a target process. Turn improvements into role-owned child items, and list decisions that need my input separately.
Collect shared facts once and include them in each review brief so reviewers do not repeat the same reading.`

// IsDefaultZeroReview reports whether the seeded text is still unmodified.
// After a person edits it, the console shows their exact words in every locale.
func IsDefaultZeroReview(r Record) bool {
	return BuiltinPlaybook(r) == "zero_review"
}

// DefaultCrossReviewID identifies the single-reviewer integration checklist.
const DefaultCrossReviewID = "00000000-0000-4000-8000-000000000102"
const DefaultCrossReviewTitle = "Cross-Surface Code Review"

// ReviewAssessment and IntegrationReviewInstructions also shape the one gate checker.
const ReviewAssessment = `First freeze BASE and HEAD as full commit SHAs. Review only git diff BASE..HEAD. List affected surfaces from diff/stat: server/API, client/UI, wire/sync/relay, CLI, persisted schema/migration, auth/permission gates, docs/translations, background jobs.
Use CROSS-SURFACE if A: at least two surfaces are touched; B: permission/visibility defaults, auth/pairing, deletion/purge, migration, cross-machine receipts/idempotency change, or a running path is removed/stopped; or C: over 800 changed lines excluding tests, generated files and translation catalogs. Otherwise use SINGLE. Report the decision, triggers and evidence. An unclear range or unavailable evidence is unverified, not evidence for SINGLE.`

const IntegrationReviewInstructions = `Check integration completeness, including unchanged paths required by the change: callers and producers/consumers; local HTTP vs remote/Cloud wire; local and hosted clients and CLI; permission gates on new routes; background jobs (goroutines, timers, pushes, sweeps); removed/stopped paths and their dependents; docs, comments and translations now untrue. Read both ends of each required connection.
Do not rationalize omissions for the author. If a surface that should carry the behavior is untouched or still refuses, never dismiss it as intentional or matching commit intent. Report INTENDED? author must confirm: with the concrete user-visible consequence if unintended. Only the author may confirm intent. Keep unresolved intent, plausible issues and confirmed defects separate. An intent question alone is not a confirmed defect.`

const ReviewEvidenceLimits = `Correctness that depends on a real browser, platform, timing or recorded data requires actual execution evidence; otherwise mark it unverified and name what is missing. Reading alone is not a pass.
Evidence limit: D hit 4/4 on one historical Cloud omission; C hit 3/4 across rounds 2-3 and the pair 2/2. D added 2-3 intent questions/run, including unresolved ones; one refactor control had zero findings. A later 18-session role-text pilot showed partial gains only. Small repeated samples do not prove general superiority, equivalence to two reviewers, or runtime correctness.`

const DefaultCrossReviewBody = `Review a committed change. If BASE/HEAD is unclear or uncommitted, ask and stop. Assess scope before dispatch. A cross-surface review uses a broader lens, not automatically more reviewers.

` +
	ReviewAssessment +
	`

Dispatch ONE read-only Clawdline reviewer by default. SINGLE uses general review; CROSS-SURFACE adds the integration pass and omission rule below (arm D). Do not automatically add reviewers for A/B/C. An extra reviewer needs a concrete independent question, justification and authorization under current rules; this Playbook grants no spending permission.
Load the clawdline skill and read clawdline guide dispatch. Use clawdline dispatch --kind review --claims "" --persona code-reviewer --permission-mode ask --task-id LOWERCASE_UUID --title "REVIEW_TITLE". Brief the repository, BASE, HEAD and these rules; reuse the ID for retries. If unavailable/refused, report and stop; do not substitute provider-native agents.

Every reviewer: read-only; at most 8 substantive findings, severity first. Each needs severity, file:line, defect, concrete failure scenario and confidence (confirmed-by-reading, plausible or unverified). List completed checks; no padding.

` +
	IntegrationReviewInstructions +
	`

` +
	ReviewEvidenceLimits +
	`

Synthesize by reading each cited location yourself, merging duplicates and labeling confirmed, plausible, unresolved intent, or rejected with reasons. Do not edit code. Report the scope decision/evidence, task IDs, reviewer count, ranked findings, intent questions and unverified claims. When explaining findings and required decisions to the user, translate technical nuances into plain, conversational language: summarize what needs deciding, the current situation, real-world risks, and clear options in the plainest words instead of reciting raw line numbers or jargon. Ask which findings to fix.`

// DefaultBoardDriveID identifies the starter that drives owned Board items to done.
// Only its title is localized; its launch message is English in every locale.
const DefaultBoardDriveID = "00000000-0000-4000-8000-000000000103"
const DefaultBoardDriveTitle = "Drive Board Items to Done"

const DefaultBoardDriveBody = `Drive the open Board items this Session owns to done. Progress means Board steps checked off, not tests run. Load the clawdline skill and read clawdline guide before using item or dispatch commands. Unless this message names item IDs, the targets are the open items this Session owns, including an owned Epic's children.

1. Inventory. Read each target with clawdline item show and clawdline item steps. Report its done, cancelled and pending counts and its next step. Add missing steps from the description with clawdline item step-add.
2. Minimal working version first. Before broadening, choose the fewest steps that make the main path run end to end on the real surface: a CLI through its entry point, a UI in a browser, an API by a real call. A Feature or Epic runs its main flow; an Issue first reproduces the failure, then the smallest fix makes that reproducer pass; documentation or configuration opens or loads. Defer secondary behavior. If no step covers this, add a step "Minimal working version". It is done only after one real run with recorded evidence. If it is blocked on access or a decision, ask once and stop.
3. Grow from the working version. Every later step starts from it and leaves it running; if a change breaks it, repair that first. Do small steps yourself. Dispatch independent steps to Clawdline children, one step or tightly related group each, with the item ID, step titles, write scope and the focused check that proves each step.
4. After every child report or unit of your own work: merge it, then reread the item's steps before anything else. Check off with clawdline item step-done each step whose change is on main and whose focused check passed. Cancel a step that discussion made unnecessary with clawdline item step-cancel and a reason. Then report the checkpoint: steps done/total, what landed, the next step.
5. Test in batches. Between checkpoints run only the focused check for the current step. After every 1-3 newly checked steps, run the repository's commit gate once and rerun the minimal working path end to end. Run complete or end-to-end acceptance once, after every step is resolved. Never rerun a check without a new change. A failure becomes the current step's repair; do not widen testing to hunt for more.
6. Blocked. When any other step needs the person's decision or access, ask once as the repository rules require, mark it waiting in the checkpoint, and continue with other steps. Do not loop on retries.
7. Finish only when every step of every target is done or cancelled with a reason and each item has reached done under its own deployment policy, or when every remaining step waits on the person. Then clean up worktrees as the repository requires, report the final step tally per item, and mark this Playbook to-do done.

This Playbook grants no permission to spend, to deploy beyond an item's policy, to create Board items its procedure does not allow, or to bypass a guard.`

// DefaultPlaybook describes an editable built-in and its deduplication titles.
type DefaultPlaybook struct {
	ID, Key, Title, Body string
	Titles               []string
}

// DefaultPlaybooks returns fresh records so callers cannot mutate the registry.
func DefaultPlaybooks() []DefaultPlaybook {
	return []DefaultPlaybook{
		{ID: DefaultZeroReviewID, Key: "zero_review", Title: DefaultZeroReviewTitle, Body: DefaultZeroReviewBody,
			Titles: []string{DefaultZeroReviewTitle, "歸零審查"}},
		{ID: DefaultCrossReviewID, Key: "cross_surface_review", Title: DefaultCrossReviewTitle, Body: DefaultCrossReviewBody,
			Titles: []string{DefaultCrossReviewTitle, "跨面向 Code Review"}},
		{ID: DefaultBoardDriveID, Key: "board_drive", Title: DefaultBoardDriveTitle, Body: DefaultBoardDriveBody,
			Titles: []string{DefaultBoardDriveTitle, "看板項目推進"}},
	}
}

// BuiltinPlaybook identifies only unchanged, unlinked global starter text.
func BuiltinPlaybook(r Record) string {
	if r.Scope != Playbook || r.Project != "" || r.Source != nil {
		return ""
	}
	for _, d := range DefaultPlaybooks() {
		if r.ID == d.ID && r.Title == d.Title && r.Body == d.Body {
			return d.Key
		}
	}
	return ""
}
