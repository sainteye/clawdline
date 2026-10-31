/*
 * The two decisions IssueSync.tsx makes without a screen: whether a choice is
 * complete enough to send, and which sentence explains a failed link. This
 * file imports nothing, so a test can run it as it is.
 */

export type IssueSyncDraft = { provider: "github" | "jira"; jira_project?: string; jira_issue_type?: string }
export type IssueSyncChoice = { provider: "github" } | { provider: "jira"; jira_project: string; jira_issue_type: string }

/** A Jira choice is complete only with a project and an issue type. */
export function readyChoice(d: IssueSyncDraft): IssueSyncChoice | null {
  if (d.provider === "github") return { provider: "github" }
  return d.jira_project && d.jira_issue_type ? { provider: "jira", jira_project: d.jira_project, jira_issue_type: d.jira_issue_type } : null
}

/** The copy key that says why a link failed, by the daemon's typed code. */
export function failureKey(code: string | undefined):
  "issueSyncErrAuth" | "issueSyncErrRate" | "issueSyncErrNotFound" | "issueSyncErrRefused" | "issueSyncErrTransition" | "issueSyncErrUnreachable" {
  switch (code) {
    case "issue_sync_authorization_required": case "github_reauthorization_required": case "jira_reauthorization_required":
      return "issueSyncErrAuth"
    case "github_rate_limited": case "jira_rate_limited": return "issueSyncErrRate"
    case "issue_target_not_found": case "issue_link_broken": return "issueSyncErrNotFound"
    case "issue_target_refused": return "issueSyncErrRefused"
    case "jira_transition_missing": return "issueSyncErrTransition"
  }
  return "issueSyncErrUnreachable"
}
