import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from "react"
import { nextWord } from "../../next-strings.js"
import { failureWords } from "./shared.js"
import {
  connectJira, disconnectGitHub, disconnectJira, readGitHubConnection, readJiraConnection, readJiraProjects, retryIssueSync,
  startGitHubConnection, type GitHubConnection, type IssueSyncChoice, type IssueSyncState, type JiraConnection, type JiraProject,
  type WorkV2Item,
} from "./api.js"
import { failureKey, readyChoice as complete, type IssueSyncDraft } from "./issue-sync-logic.js"
import "./issue-sync.css"

/**
 * Issue sync on the Board (docs/issue-sync.md). The person opts an item in
 * when creating or assigning it. Sending an option this machine has no
 * authorization for runs the authorization first, inside the same form, and
 * sends only after it succeeds; cancelling sends nothing.
 */

export type { IssueSyncDraft }

const providerName = (provider: string) => provider === "jira" ? "Jira" : "GitHub"

/** The checkbox and provider choice shown on the create and assign forms. */
export function IssueSyncField({ id, value, disabled, onChange }: {
  id: string
  value: IssueSyncDraft | null
  disabled?: boolean
  onChange: (next: IssueSyncDraft | null) => void
}) {
  const [github, setGitHub] = useState<GitHubConnection | null>(null)
  const [jira, setJira] = useState<JiraConnection | null>(null)
  const [projects, setProjects] = useState<JiraProject[] | null>(null)
  const provider = value?.provider
  useEffect(() => {
    if (!provider) return
    let live = true
    if (provider === "github") void readGitHubConnection().then((c) => { if (live) setGitHub(c) }, () => {})
    else void readJiraConnection().then((c) => {
      if (!live) return
      setJira(c)
      if (c.state === "connected") void readJiraProjects().then((p) => { if (live) setProjects(p.projects) }, () => {})
    }, () => {})
    return () => { live = false }
  }, [provider])
  const account = provider === "github"
    ? github?.state === "connected" ? `@${github.login}` : ""
    : jira?.state === "connected" ? jira.account || jira.email || "" : ""
  const project = projects?.find((p) => p.key === value?.jira_project)
  return <fieldset className="issue-sync-field" disabled={disabled}>
    <label className="issue-sync-toggle">
      <input type="checkbox" checked={!!value} onChange={(e) => onChange(e.target.checked ? { provider: "github" } : null)} />
      <span>{nextWord("issueSyncToggle")}</span>
    </label>
    {value && <div className="issue-sync-body">
      <div className="issue-sync-providers" role="radiogroup" aria-label={nextWord("issueSyncToggle")}>
        {(["github", "jira"] as const).map((p) => <button key={p} type="button" role="radio" aria-checked={value.provider === p}
          className={`chip${value.provider === p ? " on" : ""}`} onClick={() => onChange({ provider: p })}>{providerName(p)}</button>)}
      </div>
      <p className="issue-sync-status" id={`${id}-status`}>
        {account ? nextWord("issueSyncConnectedAs", { account }) : nextWord("issueSyncConnectOnSend", { provider: providerName(value.provider) })}
      </p>
      {value.provider === "jira" && (projects
        ? <JiraTargetPicker id={id} projects={projects} value={value} onChange={onChange} />
        : <p className="issue-sync-status">{nextWord("issueSyncJiraChooseLater")}</p>)}
      <p className="issue-sync-hint">{nextWord("issueSyncHint", { provider: providerName(value.provider) })}
        {value.provider === "github" && <> {nextWord("issueSyncPublicWarning")}</>}</p>
      {project && !project.issue_types.length && <p className="work-note">{nextWord("issueAuthJiraNoProjects")}</p>}
    </div>}
  </fieldset>
}

function JiraTargetPicker({ id, projects, value, onChange }: {
  id: string
  projects: JiraProject[]
  value: IssueSyncDraft
  onChange: (next: IssueSyncDraft) => void
}) {
  const project = projects.find((p) => p.key === value.jira_project)
  if (!projects.length) return <p className="work-note">{nextWord("issueAuthJiraNoProjects")}</p>
  return <div className="issue-sync-jira">
    <label>{nextWord("issueSyncJiraProject")}
      <select className="work-input" id={`${id}-jira-project`} value={value.jira_project ?? ""}
        onChange={(e) => {
          const next = projects.find((p) => p.key === e.target.value)
          onChange({ provider: "jira", jira_project: e.target.value, jira_issue_type: next?.issue_types[0] })
        }}>
        <option value="" disabled>—</option>
        {projects.map((p) => <option key={p.key} value={p.key}>{p.name} ({p.key})</option>)}
      </select>
    </label>
    {project && <label>{nextWord("issueSyncJiraIssueType")}
      <select className="work-input" value={value.jira_issue_type ?? ""}
        onChange={(e) => onChange({ ...value, jira_issue_type: e.target.value })}>
        {project.issue_types.map((t) => <option key={t} value={t}>{t}</option>)}
      </select>
    </label>}
  </div>
}

/**
 * ensure answers the choice to send once this machine is authorized for it:
 * undefined when nothing was chosen, the complete choice, or "cancelled".
 * While it waits, `panel` is the authorization to render inside the form.
 */
export function useIssueSyncSend() {
  const [pending, setPending] = useState<{ draft: IssueSyncDraft; resolve: (c: IssueSyncChoice | "cancelled") => void } | null>(null)
  const ensure = useCallback(async (draft: IssueSyncDraft | null): Promise<IssueSyncChoice | undefined | "cancelled"> => {
    if (!draft) return undefined
    try {
      if (draft.provider === "github") {
        if ((await readGitHubConnection()).state === "connected") return { provider: "github" }
      } else if ((await readJiraConnection()).state === "connected") {
        const ready = complete(draft)
        if (ready) return ready
      }
    } catch {
      // An unreadable status is asked again by the panel itself.
    }
    return new Promise((resolve) => setPending({ draft, resolve }))
  }, [])
  const panel = pending ? <IssueSyncAuthorize draft={pending.draft}
    onDone={(choice) => { pending.resolve(choice); setPending(null) }}
    onCancel={() => { pending.resolve("cancelled"); setPending(null) }} /> : null
  return { ensure, panel, authorizing: !!pending }
}

function IssueSyncAuthorize({ draft, onDone, onCancel }: {
  draft: IssueSyncDraft
  onDone: (choice: IssueSyncChoice) => void
  onCancel: () => void
}) {
  // The panel opens below the form's fields, often under the fold of a long
  // form: bring it into view so pressing Create visibly does something.
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => { box.current?.scrollIntoView?.({ block: "nearest", behavior: "smooth" }) }, [])
  return <div ref={box}>{draft.provider === "github"
    ? <GitHubAuthorize onConnected={() => onDone({ provider: "github" })} onCancel={onCancel} />
    : <JiraAuthorize draft={draft} onDone={onDone} onCancel={onCancel} />}</div>
}

/** The device flow: a code, a link, and a poll until GitHub answers. */
export function GitHubAuthorize({ onConnected, onCancel }: { onConnected: () => void; onCancel: () => void }) {
  const [status, setStatus] = useState<GitHubConnection | null>(null)
  const [failure, setFailure] = useState("")
  const [copied, setCopied] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const done = useRef(onConnected)
  done.current = onConnected
  useEffect(() => {
    let live = true
    let timer = 0
    setStatus(null); setFailure("")
    const poll = async () => {
      try {
        const now = await readGitHubConnection()
        if (!live) return
        setStatus(now)
        if (now.state === "connected") { done.current(); return }
        if (now.state === "pending") timer = window.setTimeout(poll, 3000)
      } catch {
        if (live) timer = window.setTimeout(poll, 5000)
      }
    }
    void startGitHubConnection().then((started) => {
      if (!live) return
      setStatus(started)
      if (started.state === "connected") done.current()
      else timer = window.setTimeout(poll, 3000)
    }, (e) => { if (live) setFailure(failureWords(e)) })
    return () => { live = false; window.clearTimeout(timer) }
  }, [attempt])
  const ended = status && (status.state === "denied" || status.state === "expired" || status.state === "failed")
  return <section className="issue-sync-auth" aria-labelledby="issue-sync-github-title" role="group">
    <h3 id="issue-sync-github-title">{nextWord("issueAuthGitHubTitle")}</h3>
    {status?.state === "pending" && status.user_code && <>
      <p>{nextWord("issueAuthGitHubSteps")}</p>
      <p className="issue-sync-code" aria-live="polite"><code>{status.user_code}</code></p>
      <div className="work-actions">
        <a className="chip on" href={status.verification_uri} target="_blank" rel="noopener noreferrer">{nextWord("issueAuthOpenGitHub")}</a>
        <button className="chip" type="button" onClick={() => {
          void navigator.clipboard?.writeText(status.user_code!).then(() => setCopied(true), () => {})
        }}>{copied ? nextWord("issueAuthCopied") : nextWord("issueAuthCopyCode")}</button>
      </div>
      <p className="issue-sync-status" role="status">{nextWord("issueAuthWaiting")}</p>
    </>}
    {!status && !failure && <p className="issue-sync-status" role="status">{nextWord("issueAuthStarting")}</p>}
    {(failure || ended) && <p className="work-note" role="alert">{failure ||
      nextWord(status!.state === "denied" ? "issueAuthDenied" : status!.state === "expired" ? "issueAuthExpired" : "issueAuthFailed")}</p>}
    <div className="work-actions">
      {(failure || ended) && <button className="chip on" type="button" onClick={() => setAttempt((n) => n + 1)}>{nextWord("issueAuthRetry")}</button>}
      <button className="chip" type="button" onClick={onCancel}>{nextWord("issueAuthCancel")}</button>
    </div>
  </section>
}

/** Site, email and API token; then, when the form had none, the project. */
export function JiraAuthorize({ draft, onDone, onCancel }: {
  draft?: IssueSyncDraft
  onDone: (choice: IssueSyncChoice) => void
  onCancel: () => void
}) {
  const [connected, setConnected] = useState(false)
  const [site, setSite] = useState("https://")
  const [email, setEmail] = useState("")
  const [token, setToken] = useState("")
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState("")
  const [projects, setProjects] = useState<JiraProject[] | null>(null)
  const [target, setTarget] = useState<IssueSyncDraft>(draft ?? { provider: "jira" })
  useEffect(() => {
    let live = true
    void readJiraConnection().then((c) => { if (live && c.state === "connected") setConnected(true) }, () => {})
    return () => { live = false }
  }, [])
  useEffect(() => {
    if (!connected || !draft) return
    let live = true
    void readJiraProjects().then((p) => { if (live) setProjects(p.projects) }, (e) => { if (live) setFailure(failureWords(e)) })
    return () => { live = false }
  }, [connected, draft])
  // A div, not a form: this panel sits inside the create and assign forms.
  const submit = () => {
    if (busy || !site.trim() || !email.trim() || !token.trim()) return
    setBusy(true); setFailure("")
    void connectJira(site, email, token).then(() => { setToken(""); setConnected(true); if (!draft) onDone({ provider: "jira", jira_project: "", jira_issue_type: "" }) },
      (err) => setFailure(failureWords(err))).finally(() => setBusy(false))
  }
  const enter = (e: KeyboardEvent) => { if (e.key === "Enter") { e.preventDefault(); submit() } }
  if (!connected) return <div className="issue-sync-auth" role="group" aria-labelledby="issue-sync-jira-title" onKeyDown={enter}>
    <h3 id="issue-sync-jira-title">{nextWord("issueAuthJiraTitle")}</h3>
    <label>{nextWord("issueAuthJiraSite")}<input className="work-input" type="url" value={site} placeholder="https://your-team.atlassian.net"
      autoComplete="url" onChange={(e) => setSite(e.target.value)} /></label>
    <label>{nextWord("issueAuthJiraEmail")}<input className="work-input" type="email" value={email} autoComplete="email"
      onChange={(e) => setEmail(e.target.value)} /></label>
    <label>{nextWord("issueAuthJiraToken")}<input className="work-input" type="password" value={token} autoComplete="off"
      onChange={(e) => setToken(e.target.value)} /></label>
    <p className="issue-sync-hint">{nextWord("issueAuthJiraTokenHelp")}</p>
    {failure && <p className="work-note" role="alert">{failure}</p>}
    <div className="work-actions">
      <button className="chip on" type="button" disabled={busy || !site.trim() || !email.trim() || !token.trim()} aria-busy={busy}
        onClick={submit}>{busy ? nextWord("issueAuthJiraConnecting") : nextWord("issueAuthJiraConnect")}</button>
      <button className="chip" type="button" disabled={busy} onClick={onCancel}>{nextWord("issueAuthCancel")}</button>
    </div>
  </div>
  const ready = complete(target)
  return <section className="issue-sync-auth" role="group" aria-labelledby="issue-sync-jira-choose">
    <h3 id="issue-sync-jira-choose">{nextWord("issueAuthJiraChoose")}</h3>
    {projects && <JiraTargetPicker id="issue-sync-auth" projects={projects} value={target} onChange={setTarget} />}
    {failure && <p className="work-note" role="alert">{failure}</p>}
    <div className="work-actions">
      <button className="chip on" type="button" disabled={!ready} onClick={() => { if (ready) onDone(ready) }}>{nextWord("issueAuthContinue")}</button>
      <button className="chip" type="button" onClick={onCancel}>{nextWord("issueAuthCancel")}</button>
    </div>
  </section>
}

function failureReason(s: IssueSyncState): string {
  return nextWord(failureKey(s.error), { provider: providerName(s.provider) })
}

/** The card's line for an item's issue. */
export function IssueSyncBadge({ item, onChanged }: { item: Pick<WorkV2Item, "id" | "issue_sync">; onChanged?: () => void }) {
  const s = item.issue_sync
  const [busy, setBusy] = useState(false)
  if (!s) return null
  const provider = providerName(s.provider)
  const link = s.url && s.external_key
    ? <a href={s.url} target="_blank" rel="noopener noreferrer" aria-label={nextWord("issueSyncOpenLabel", { provider, key: s.external_key })}
      onClick={(e) => e.stopPropagation()}>{provider} {s.external_key}</a>
    : null
  return <p className={`issue-sync-badge issue-sync-${s.state}`}>
    {s.state === "waiting" && nextWord("issueSyncWaiting", { provider })}
    {s.state === "open" && link}
    {s.state === "closed" && <>{link ?? provider} <span>{nextWord("issueSyncClosed")}</span></>}
    {s.state === "failed" && <>
      {link}{link && " · "}{nextWord("issueSyncFailed", { provider, reason: failureReason(s) })}
      <button className="chip" type="button" disabled={busy} aria-busy={busy} onClick={(e) => {
        e.stopPropagation(); setBusy(true)
        void retryIssueSync(item.id).then(() => onChanged?.(), () => {}).finally(() => setBusy(false))
      }}>{busy ? nextWord("issueSyncRetrying") : nextWord("issueSyncRetry")}</button>
    </>}
  </p>
}

/** Settings: this machine's GitHub and Jira connections. */
export function IssueTrackersBlock({ shown }: { shown: boolean }) {
  const [github, setGitHub] = useState<GitHubConnection | null>(null)
  const [jira, setJira] = useState<JiraConnection | null>(null)
  const [connecting, setConnecting] = useState<"github" | "jira" | null>(null)
  const [failure, setFailure] = useState("")
  const refresh = useCallback(() => {
    void readGitHubConnection().then(setGitHub, (e) => setFailure(failureWords(e)))
    void readJiraConnection().then(setJira, (e) => setFailure(failureWords(e)))
  }, [])
  useEffect(() => { if (shown) refresh() }, [shown, refresh])
  if (!shown) return null
  const row = (provider: "github" | "jira", account: string, connected: boolean, disconnect: () => Promise<unknown>) =>
    <div className="issue-trackers-row">
      <div><b>{providerName(provider)}</b><span>{connected ? nextWord("issueSyncConnectedAs", { account }) : nextWord("issueTrackersNotConnected")}</span></div>
      {connected
        ? <button className="chip" type="button" onClick={() => { setFailure(""); void disconnect().then(refresh, (e) => setFailure(failureWords(e))) }}>{nextWord("issueTrackersDisconnect")}</button>
        : <button className="chip on" type="button" disabled={!!connecting} onClick={() => setConnecting(provider)}>{nextWord("issueTrackersConnect")}</button>}
    </div>
  return <section className="block settings-workflow issue-trackers" aria-labelledby="issue-trackers-title">
    <b id="issue-trackers-title">{nextWord("issueTrackersTitle")}</b>
    <p className="say">{nextWord("issueTrackersBody")}</p>
    {row("github", github?.login ? `@${github.login}` : "", github?.state === "connected", disconnectGitHub)}
    {github?.state === "connected" && <p className="issue-sync-hint">{nextWord("issueTrackersGitHubRevoke")}</p>}
    {row("jira", jira?.account || jira?.site || "", jira?.state === "connected", disconnectJira)}
    {connecting === "github" && <GitHubAuthorize onConnected={() => { setConnecting(null); refresh() }} onCancel={() => setConnecting(null)} />}
    {connecting === "jira" && <JiraAuthorize onDone={() => { setConnecting(null); refresh() }} onCancel={() => setConnecting(null)} />}
    {failure && <p className="work-note" role="alert">{failure}</p>}
  </section>
}
