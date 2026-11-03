# Issue sync: mirroring a Board item into GitHub or Jira

Status (2026-11-01): decided by the owner (D73). Steps 1–6 are done: both trackers were exercised
end to end against the real services (§5, step 6).

This page supersedes the 2026-09-19 rulings G4 ("Clawdline does not open issues") and G6 ("no
two-way sync") in `work-system.md` §6 and `github-issues.md` §7. Their concern still holds and
shapes the design: local self-management states (Backlog returns, `unconfirmed`, conditions) never
reach a tracker, and the tracker never becomes a second source of "done".

## 1. What the person sees

- Creating an item, or assigning it to an existing or a new Session, offers **Sync to GitHub** or
  **Sync to Jira** for the item's Project.
- If this machine holds no authorization for that Project's target, the option can still be
  checked, but **sending the form first runs the authorization**. The item is created or assigned
  only after the authorization succeeds; cancelling the authorization leaves the form open with
  nothing written. There is no item waiting for an authorization.
- Before the first send, the form shows the exact title and body that will leave the machine, and
  warns when the GitHub repository is public.
- When the item reaches `implementing`, an issue is opened. The card shows `#N` or `PROJ-123`
  with a link.
- When the item is `done`, the issue is closed as completed with the landing commit in a comment.
  When it is `cancelled`, the issue is closed as not planned. A reopened item reopens it.
- A failed tracker call never blocks the item. The card shows the typed reason and a Retry action.

"Clearing" an issue means **closing** it. Deleting is irreversible: GitHub's REST API cannot delete
an issue and GraphQL deletion needs repository admin; Jira deletion loses the record of which
commit resolved it.

## 2. Direction and authority

| Fact | Writer |
|---|---|
| Item lifecycle, owner, landing | Clawdline (broker facts and the person's commands); unchanged |
| Whether an item syncs, and to where | The person, at create or assign |
| Issue exists / open / closed | Clawdline's outbox, derived only from the item's phase |
| Issue title, body, labels after creation | The tracker; Clawdline does not edit them again |

Nothing read from a tracker changes an item. Its state is an echo, not evidence (D01).

## 3. Rules (step 1, implemented)

`NextIssueOp(phase, observed)`:

| Item phase | Issue not created | Issue open | Issue closed |
|---|---|---|---|
| created, assigning, assigned | — | — | reopen |
| implementing, verifying, merging, deploying | create | — | reopen |
| done | — | close (completed) | — |
| cancelled | — | close (not planned) | — |

An item that is completed or cancelled without reaching implementation leaves nothing in the
tracker. `IssueSyncGate` refuses an unauthorized send with `issue_sync_authorization_required`,
which the Console answers by authorizing and resending; an unaddressable target is
`issue_sync_target_invalid`. Targets: GitHub `owner/repo` under GitHub's naming rules; Jira Cloud
`https://<name>.atlassian.net`, a project key and an issue type.

Neither tracker accepts an idempotency key. A create therefore writes `IssueMarker(item id)` into
the body, and a retry searches for it before creating.

## 4. Data and boundaries (steps 2–3)

- **Project binding**: the GitHub repository comes from the Project's remote (`projectlinks`); the
  Jira site, project key and issue type are chosen by the person once per Project.
- **Credentials**: GitHub through Clawdline's OAuth App device flow (client id `Ov23lioodvbqh76e5rtY`,
  scope `repo`, expiring user tokens). It works from the phone: the person sees a code and opens
  `github.com/login/device`. Neither authorizing nor refreshing needs a client secret, and none
  ships. Jira Cloud uses an API token (step 5): Jira offers no device flow, and its OAuth 2.0 (3LO)
token exchange needs a client secret that an open-source daemon cannot hold, so a one-click Jira
connection would have to route every token through Clawdline Cloud. The owner chose the API token
on 2026-11-01; the form links to Atlassian's token page, and the token is checked against
`/rest/api/3/myself` before it is saved under `<state dir>/integrations/jira/`. Both secrets are kept the way `cloudkeys` keeps this
  daemon's own secrets — an owner-only file under `<state dir>/integrations/github/` (0700/0600,
  atomic rename, symlinks refused, unreadable is a failure, not "disconnected"), because that is
  the one store all three platforms have; a platform keystore can replace it behind the same
  type. No route answers the token or the device code, and Cloud never carries them. Processes
  running as the same OS user are inside the endpoint trust boundary
  (`platform-trust-boundary.md`), as they are for every other secret on the machine.
- **Refresh**: GitHub retires the old token pair as soon as it answers a refresh, so the new pair
  is saved before use and refreshes are serialized. A refused refresh is `reauthorize`.
  Disconnecting forgets the token on this machine; GitHub keeps the grant until the person revokes
  it under Settings → Applications, because revoking needs the client secret.
- **Link record** per item: provider, target, observed state, external key, URL, typed last error,
  attempts, version. Every change writes an item event.
- **Reconciler, not an outbox**: the link row (`work_v2_issue_links`) records the target and the
  last observed tracker state; `internal/app/issuesync` compares it with the item's phase
  (`work.NextIssueOp`) every 15 seconds and on a wake after create, assign and retry, at most
  `MaxLinksPerPass` links a pass. A create first looks for an issue carrying the item's marker
  (GitHub: the body marker among the newest issues; Jira: the label `clawdline-item-<id>`), so a lost
  answer never opens a second issue. Failures back off from one minute to an hour, or follow
  `Retry-After`, and are typed (`github_rate_limited`, `jira_reauthorization_required`,
  `issue_target_not_found`, `jira_transition_missing`, …); each new code writes one item event.
  Bounds are registered in `internal/domain/capacity` (`work.issue_sync_*`).
- **What is sent**: the item's title and description, plus a line saying Clawdline opened it. The
  form says so before sending and warns that a public repository's issue is public. No automatic
  privacy filter runs on them; the person decides by ticking the option.

## 5. Delivery

1. Decision, this page, pure rules and tests. *(done)*
2. Credential store, GitHub device-flow authorization, Project binding. *(done:
   `internal/adapters/githubissues`; `GET`/`DELETE /v1/integrations/github` reads or forgets the
   connection, `GET`/`POST /v1/integrations/github/device` starts the flow and reads its code.
   Writes need this machine or a paired device that may send. `TargetFor` reads `owner/repo` from
   the Project's `origin`, accepting only the host `github.com`.)*
3. GitHub outbox worker: create, close, reopen; retry, offline, rate-limit and duplicate-create
   failure-injection tests.
4. Console: the option, authorize-on-send, preview, card badge and Retry, integration settings;
   desktop and phone; English and Taiwan Traditional Chinese; Cloud relay fields via `contract-gen`.
5. Jira Cloud: API token, project and issue-type choice, discovery of the done transition.
   *(steps 3–5 done: `internal/adapters/githubissues/issues.go`, `internal/adapters/jiraissues`,
   `internal/app/issuesync`, `POST /v1/issue-sync/<id>/retry`, `/v1/integrations/jira` and
   `/v1/integrations/jira/projects`; Settings → Session and access → Issue trackers.)*
6. End-to-end on a real test repository and Jira test project, with browser evidence. *(Jira done
   2026-11-01 on an isolated daemon against a real Jira Cloud site: an item created with Jira sync
   stayed `waiting`; at `implementing` the reconciler opened the issue with the title, description
   and marker label; completing the item moved the issue to the done category. Localized issue-type
   names, here Traditional Chinese, worked as given by the projects route. GitHub done the same day
   against this repository, from an isolated daemon connected by the device flow: the item's
   repository came from the Project's `origin`; at `implementing` the issue opened with the body
   marker, and completing the item closed it with the reason `completed`.)*

Available in the free edition: it runs on one machine's daemon and involves no other machine.
