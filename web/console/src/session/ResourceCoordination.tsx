import { useEffect, useState } from "react"
import { catalogWord } from "../catalog.js"
import { fetchWithDeadline } from "../pages/work/fetch-deadline.js"

type Source = { observed_at: number; provenance: string; freshness: string }
type Session = { id: string; sessionId?: string; label?: string; state: string; work_state?: string; source?: Source }
type Lease = { resource: string; key: string; holder?: { holder: string; session_id?: string; reason?: string; liveness: string }; queue: { holder: string; session_id?: string; position: number; reason?: string; proving: boolean }[] }
type Wait = { id: string; owner_session_id: string; release_condition: string; waiters: { session_id: string; reason: string }[] }
type Pause = { id: string; target_session_id: string; state: string; reason: string; wake_condition: string; accepted_at: number; delivered_at?: number; observed_at?: number; safe_at?: number; wake_requested_at?: number; wake_delivered_at?: number; delivery_error?: string; wake_error?: string }
type Task = { id: string; title: string; kind: string; state: string }
type Readings = {
  sessions?: { at: number; scan: { complete: boolean; provenance: string; notes?: string[] }; sessions: Session[] }
  leases?: { at: number; leases: Lease[] }
  waits?: { at: number; waits: Wait[] }
  pauses?: { at: number; pauses: Pause[] }
  tasks?: { at: number; source: Source; tasks: Task[] }
}

const paths = {
  sessions: "/v1/sessions",
  leases: "/v1/orchestrator/leases",
  waits: "/v1/orchestrator/waits",
  pauses: "/v1/orchestrator/pauses",
  tasks: "/v1/orchestrator/tasks?limit=50",
} as const

async function read(path: string): Promise<unknown> {
  const response = await fetchWithDeadline(path, { credentials: "same-origin" }, 15_000)
  if (!response.ok) throw new Error(String(response.status))
  return response.json()
}

const word = (key: string) => catalogWord("resourceCoordination", key)
const at = (seconds: number | undefined) => seconds ? new Date(seconds * 1000).toLocaleString() : word("unknown")
const state = (value: string) => word(value === "observed" ? "observed_state" : value)

export function ResourceCoordination({ onBack, onAsk }: { onBack: () => void; onAsk: () => void }) {
  const [data, setData] = useState<Readings>({})
  const [failed, setFailed] = useState<string[]>([])
  const [busy, setBusy] = useState(true)
  const load = async () => {
    setBusy(true)
    const entries = Object.entries(paths) as [keyof Readings, string][]
    const results = await Promise.allSettled(entries.map(([, path]) => read(path)))
    const next: Readings = {}
    const errors: string[] = []
    results.forEach((result, i) => {
      const name = entries[i][0]
      if (result.status === "fulfilled") (next as Record<string, unknown>)[name] = result.value
      else errors.push(name)
    })
    setData(next)
    setFailed(errors)
    setBusy(false)
  }
  useEffect(() => { void load() }, [])
  const sessions = data.sessions?.sessions ?? []
  const leases = data.leases?.leases ?? []
  const waits = data.waits?.waits ?? []
  const pauses = data.pauses?.pauses ?? []
  const tasks = data.tasks?.tasks?.filter((task) => !["success", "failure", "timeout", "cancelled", "spawn_failed"].includes(task.state)) ?? []
  const name = (id: string) => sessions.find((s) => s.sessionId === id || s.id === id)?.label || id

  return <div className="resource-coordination">
    <div className="resource-coordination-actions">
      <button type="button" onClick={onBack}>{word("back")}</button>
      <button type="button" onClick={() => void load()} disabled={busy}>{word("refresh")}</button>
      <button type="button" onClick={onAsk}>{word("ask")}</button>
    </div>
    {busy && <p>{word("loading")}</p>}
    {failed.length > 0 && <p role="status">{word("unavailable")}: {failed.join(", ")}</p>}
    <section><h3>{word("sessions")}</h3><small>{word("observed")}: {at(data.sessions?.at)} · {word("source")}: {data.sessions?.scan.provenance || word("unknown")}</small>
      {data.sessions && !data.sessions.scan.complete && <p role="status">{word("incompleteScan")}: {data.sessions.scan.notes?.join(" · ") || word("unknown")}</p>}
      {data.sessions && (sessions.length ? <ul>{sessions.map((s) => <li key={s.id}>
        <strong>{s.label || s.id}</strong> · {s.state} · {s.work_state || word("unknown")}
        <small>{word("source")}: {s.source?.provenance || word("unknown")} · {s.source?.freshness || word("unknown")} · {at(s.source?.observed_at)}</small>
      </li>)}</ul> : <p>{word("empty")}</p>)}
    </section>
    <section><h3>{word("resources")}</h3><small>{word("observed")}: {at(data.leases?.at)} · {word("source")}: broker</small>
      {data.leases && (leases.length ? <ul>{leases.map((l) => <li key={l.resource + l.key}>
        <strong>{l.resource}</strong> · {l.key}<br />
        {word("holder")}: {l.holder ? `${name(l.holder.session_id || "")} · ${l.holder.liveness} · ${l.holder.reason || ""}` : word("none")}
        {l.queue.length > 0 && <ol>{l.queue.map((q) => <li key={q.session_id || q.holder}>
          #{q.position || "?"} {name(q.session_id || "")} · {q.reason || word("unknown")} · {q.proving ? word("queued") : word("stale")} · {word("nextWake")}: {word("leaseGrant")}
        </li>)}</ol>}
      </li>)}</ul> : <p>{word("empty")}</p>)}
    </section>
    <section><h3>{word("pauses")}</h3><small>{word("observed")}: {at(data.pauses?.at)} · {word("source")}: broker</small>
      {data.pauses && (pauses.length ? <ul>{pauses.map((p) => <li key={p.id}>
        <strong>{name(p.target_session_id)}</strong> · {state(p.state)} · {p.reason}<small>{word("requestID")}: <code>{p.id}</code></small>
        {word("nextWake")}: {p.wake_condition}<small>{word("accepted")}: {at(p.accepted_at)} · {word("delivered")}: {at(p.delivered_at)} · {word("observed")}: {at(p.observed_at)} · {word("safe_point")}: {at(p.safe_at)}</small>
        {(p.delivery_error || p.wake_error) && <small>{word("error")}: {p.delivery_error || p.wake_error} · {word("retryHint")}: <code>clawdline coordination retry {p.id}</code></small>}
      </li>)}</ul> : <p>{word("empty")}</p>)}
    </section>
    <section><h3>{word("waits")}</h3><small>{word("observed")}: {at(data.waits?.at)} · {word("source")}: broker</small>
      {data.waits && (waits.length ? <ul>{waits.map((w) => <li key={w.id}>
        <strong>{name(w.owner_session_id)}</strong> · {word("nextWake")}: {w.release_condition}
        <small>{w.waiters.map((q) => `${name(q.session_id)}: ${q.reason}`).join(" · ")}</small>
      </li>)}</ul> : <p>{word("empty")}</p>)}
    </section>
    <section><h3>{word("tasks")}</h3><small>{word("observed")}: {at(data.tasks?.at)} · {word("source")}: {data.tasks?.source?.provenance || word("unknown")} · {data.tasks?.source?.freshness || word("unknown")}</small>
      {data.tasks && (tasks.length ? <ul>{tasks.map((t) => <li key={t.id}>{t.title} · {t.kind} · {t.state}</li>)}</ul> : <p>{word("empty")}</p>)}
    </section>
  </div>
}
