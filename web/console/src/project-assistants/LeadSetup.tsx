import { useEffect, useState } from "react"
import type { ProjectAssistantAgent, ProjectAssistantFirstRun, ProjectAssistantFirstRunAgent } from "@clawdline/contract"
import { errorCode, type SourceClient } from "./client"
import { settingsSaveFailure } from "./BotSettings"
import { agentName } from "./fallback-settings"
import type { Words } from "./words"

type Probe = { source: SourceClient; revision: number; result: ProjectAssistantFirstRun | null }

export function useLeadSetup(source: SourceClient, eligible: boolean, agent?: ProjectAssistantAgent) {
  const active = eligible && source.canFirstRun?.() === true && !!source.firstRun
  const [revision, setRevision] = useState(0)
  const [probe, setProbe] = useState<Probe>()
  useEffect(() => {
    if (!active) return
    const control = new AbortController()
    void source.firstRun!(control.signal).then(result => {
      if (!control.signal.aborted) setProbe({ source, revision, result })
    }).catch(() => {
      if (!control.signal.aborted) setProbe({ source, revision, result: null })
    })
    return () => control.abort()
  }, [active, revision, source])
  const current = active && probe?.source === source && probe.revision === revision ? probe : undefined
  const rows = current?.result?.agents ?? []
  const selected = rows.find(row => row.agent === agent)
  const checking = active && !current
  // An inconclusive local probe must not hide a working existing account.
  const blocked = checking || (!!selected && selected.state !== "ready" && selected.state !== "unknown")
  return {
    rows, checking, blocked, show: active && (blocked || selected?.state === "unknown" || current?.result === null),
    check: () => setRevision(revision => revision + 1),
  }
}

const guide = {
  node: "https://nodejs.org/en/download",
  claude: "https://code.claude.com/docs/en/setup",
  codex: "https://developers.openai.com/codex/cli/",
}

function Step({ row, w, choose, choosing }: {
  row: ProjectAssistantFirstRunAgent; w: Words; choose: () => void; choosing: boolean
}) {
  const [copied, setCopied] = useState(false)
  const name = agentName(row.agent)
  if (row.state === "ready") {
    return <button type="button" onClick={choose} disabled={choosing}>{w.leadUseAgent.replace("{agent}", name)}</button>
  }
  if (row.state === "needs_sign_in") {
    const command = row.agent === "codex" ? "codex login" : "claude auth login"
    return <div>
      <p>{w.leadSignInAgent.replace("{agent}", name)} <code>{command}</code></p>
      {navigator.clipboard && <button type="button" onClick={() => {
        void navigator.clipboard.writeText(command).then(() => setCopied(true)).catch(() => setCopied(false))
      }}>{copied ? w.leadCopiedSignIn : w.leadCopySignIn}</button>}
    </div>
  }
  if (row.state === "needs_agent") {
    return <a href={row.agent === "codex" ? guide.codex : guide.claude} target="_blank" rel="noreferrer">{w.leadInstallAgent.replace("{agent}", name)}</a>
  }
  if (row.state === "unknown") return <p>{w.leadUnknownAgent.replace("{agent}", name)}</p>
  return null
}

export function LeadSetup({ setup, w, choose }: {
  setup: ReturnType<typeof useLeadSetup>; w: Words; choose: (agent: ProjectAssistantAgent) => Promise<void>
}) {
  const [choosing, setChoosing] = useState(false)
  const [error, setError] = useState("")
  const chooseAgent = async (agent: ProjectAssistantAgent) => {
    setChoosing(true)
    setError("")
    try {
      await choose(agent)
    } catch (cause) {
      setError(errorCode(cause))
    } finally {
      setChoosing(false)
    }
  }
  const unsupported = setup.rows.length > 0 && setup.rows.every(row => row.state === "unsupported")
  const needsNode = setup.rows.some(row => row.state === "needs_node")
  return <div className="pa-held-send pa-lead-setup" aria-label={w.leadSetupTitle}>
    {setup.checking ? <p role="status">{w.leadChecking}</p> : unsupported ? <p>{w.leadUnsupported}</p> : <>
      <p>{setup.rows.length ? w.leadSetupTitle : w.leadCheckFailed}</p>
      {needsNode && <a href={guide.node} target="_blank" rel="noreferrer">{w.leadInstallNode}</a>}
      {setup.rows.map(row => <Step key={row.agent} row={row} w={w} choosing={choosing || error === "pa_outcome_unknown" || error === "pa_stale_version" || error === "pa_stale_generation"} choose={() => void chooseAgent(row.agent)}/>)}
      {error && <p role="alert">{settingsSaveFailure({code:error},w)}</p>}
      <button type="button" onClick={setup.check} disabled={choosing}>{w.leadCheckAgain}</button>
    </>}
  </div>
}
