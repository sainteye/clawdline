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
	return r.ID == DefaultZeroReviewID && r.Scope == Playbook && r.Project == "" &&
		r.Title == DefaultZeroReviewTitle && r.Body == DefaultZeroReviewBody
}
