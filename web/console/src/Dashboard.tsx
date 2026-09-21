import { useCallback, useMemo, useState } from "react"
import type {
  CapacityEntry,
  CapacityPanel,
  CapacityState,
  CloseReason,
  Diagnostics,
  Obligation,
  SchedulerPulse,
  ScheduleRow,
  SessionRow,
  TaskRow,
} from "@clawdline/contract"
import { RefusalError, needsYou, sortSessions } from "@clawdline/core"
import { client } from "./client.js"
import { nextWord } from "./next-strings.js"
import { scheduleErrorCopy } from "./schedule-errors.js"
import { useFleet, usePoll } from "./useFleet.js"
import "./dashboard.css"
import "./schedule-errors.css"

/**
 * The dashboard.
 *
 * This was the whole console for a while, and it is a different idea from the
 * screen this app is replicating: the fleet is the subject and the conversation
 * is an accessory, where the original makes the conversation the subject. Both
 * are useful and they are not the same thing, so it stays — as one page among
 * the others rather than as the app.
 */
export default function Dashboard({ fleet }: { fleet: ReturnType<typeof useFleet> }) {
  const rows = fleet.snapshot ? sortSessions(fleet.snapshot.sessions) : []
  const refresh = useCallback(() => fleet.refresh(), [fleet])
  return (
    <section className="page dashboard-page">
      <div className="columns">
        <div>
          <Sessions rows={rows} fleet={fleet} onDid={refresh} />
        </div>
        <div>
          <Capacity />
          <Dispatch onDid={refresh} />
          <Usage />
          <Obligations />
          <Tasks />
          <Schedules />
          <Coordinator />
        </div>
      </div>
      {needsYou(rows) > 0 && null}
    </section>
  )
}

/**
 * Whether the daemon answers, from the open `/v1/health`, and the clock and
 * port from `/v1/diagnostics`, which only this Mac's own token may read. A
 * browser that is a paired device of its own is refused those, and the pills
 * that need them are left out rather than shown as a failure.
 */
function Health() {
  const read = useMemo(() => () => client.health(), [])
  const { data, error } = usePoll(read, 15000)
  const readDetail = useMemo(() => () => readDiagnostics(), [])
  const { data: detail } = usePoll(readDetail, 15000)
  if (error) return <span className="pill"><i className="dot off" />daemon 連不上</span>
  if (!data) return <span className="pill"><i className="dot" />…</span>
  return (
    <>
      {detail && <Clock pulse={detail.scheduler} />}
      <span className="pill">
        <i className="dot on" />
        {data.served_by}
        {detail && ` :${detail.port}`}
      </span>
    </>
  )
}

/** `/v1/capacity`, or null when this browser may not read it. */
async function readCapacity(): Promise<CapacityPanel | null> {
  const res = await fetch(client.url("/v1/capacity"), { credentials: "same-origin" })
  if (res.status === 401 || res.status === 403) return null
  if (!res.ok) throw new Error(`/v1/capacity answered ${res.status}`)
  return (await res.json()) as CapacityPanel
}

/** `/v1/diagnostics`, or null when this browser may not read it. */
async function readDiagnostics(): Promise<Diagnostics | null> {
  const res = await fetch(client.url("/v1/diagnostics"), { credentials: "same-origin" })
  if (res.status === 401 || res.status === 403) return null
  if (!res.ok) throw new Error(`/v1/diagnostics answered ${res.status}`)
  return (await res.json()) as Diagnostics
}

/**
 * How full every bounded thing this daemon keeps is (docs/limits.md §4.5,
 * design-decisions C4): which row, how full, who lets go of what when it is
 * full, and when it last told anybody.
 *
 * It is on this page and not on the replicated ones: a capacity banner on the
 * 1:1 screens would need words the original never had, and that is a decision
 * left to the person (U11). A row that is not ok is always listed; the rest
 * are one click away. A row that could not be measured says so and carries no
 * bar — unknown is not empty.
 *
 * It reads `/v1/capacity`, which any paired device may, and not
 * `/v1/diagnostics`, which is this Mac's own token's: a browser opened on this
 * Mac is a paired device too, and a panel it could never fill is not a panel.
 *
 * The completion notices that went unanswered through their whole ladder are
 * here too: a dead letter is pushed once when it happens, and this is where
 * it stays visible afterwards.
 */
function Capacity() {
  const read = useMemo(() => () => readCapacity(), [])
  const { data, error, pending } = usePoll(read, 15000)
  const [all, setAll] = useState(false)
  const rows = data ? [...data.capacity.entries].sort(bySeverity) : []
  const loud = rows.filter((r) => r.state !== "ok")
  const shown = all ? rows : loud
  const beat = data?.capacity.beat
  const dead = data?.completions?.dead_letter ?? 0
  return (
    <section className="panel capacity">
      <header>
        容量
        <span className="count">
          {data ? (loud.length > 0 ? `${loud.length} 列要注意` : `${rows.length} 列都正常`) : "—"}
        </span>
      </header>
      {error && <p className="refusal">{error}</p>}
      {pending && !data && !error && <p className="empty">讀取中…</p>}
      {!pending && !data && !error && (
        <p className="empty unread">這個瀏覽器沒有配對，讀不到容量。</p>
      )}
      {data?.completions_error && (
        <p className="refusal">完成通知讀不到，所以不知道有沒有 dead letter：{data.completions_error}</p>
      )}
      {beat && !beat.running && (
        <p className="refusal">容量巡邏沒有在這個 daemon 跑，所以下面每一列都是未知。</p>
      )}
      {beat?.stalled && <p className="refusal">容量巡邏停了：超過三輪沒有完成，下面的數字是舊的。</p>}
      {dead > 0 && (
        <div className="row cap-dead">
          <span className="k">dead letter</span>
          <span className="grow" title="重送：POST /v1/orchestrator/completions/reconcile">
            {dead} 則完成通知送到最後都沒被收下；當時已推播過一次
          </span>
        </div>
      )}
      {data && loud.length === 0 && !all && (
        <p className="empty">每一列都在告警門檻以下。</p>
      )}
      {shown.map((r) => (
        <CapacityRow key={r.name} row={r} />
      ))}
      {data && rows.length > loud.length && (
        <p className="empty footnote">
          <button className="as-link" onClick={() => setAll((v) => !v)}>
            {all ? "只看要注意的" : `展開全部 ${rows.length} 列`}
          </button>
        </p>
      )}
    </section>
  )
}

const stateWord: Record<CapacityState, string> = {
  ok: "正常",
  warn: "注意",
  critical: "快滿了",
  full: "滿了",
  unknown: "量不到",
}

const stateRank: Record<CapacityState, number> = { full: 0, critical: 1, unknown: 2, warn: 3, ok: 4 }

function bySeverity(a: CapacityEntry, b: CapacityEntry): number {
  return stateRank[a.state] - stateRank[b.state] || (b.ratio ?? 0) - (a.ratio ?? 0) || a.name.localeCompare(b.name)
}

function CapacityRow({ row }: { row: CapacityEntry }) {
  const pct = row.ratio == null ? null : Math.floor(row.ratio * 100)
  const counted = counters(row)
  const why = row.error ?? row.note
  return (
    <div className="cap" data-state={row.state}>
      <div className="line">
        <span className="name" title={row.class}>
          {row.name}
        </span>
        <span className="state">{stateWord[row.state]}</span>
        <span className="amount">
          {row.used == null
            ? `上限 ${amount(row.unit, row.limit)}`
            : `${amount(row.unit, row.used)} / ${amount(row.unit, row.limit)}`}
          {pct != null && ` · ${pct}%`}
        </span>
      </div>
      <div className="meter">{pct != null && <i style={{ width: `${Math.min(100, pct)}%` }} />}</div>
      <div className="meta">
        <span>{evicts(row)}</span>
        <span>{lastAlert(row)}</span>
        {row.projected_full_at ? <span>照目前速度 {when(row.projected_full_at)} 會滿</span> : null}
        {row.overridden && <span>上限被調小了</span>}
        {counted && <span>{counted}</span>}
      </div>
      {why && <p className="said">{why}</p>}
    </div>
  )
}

/** Who lets go of what when the row is full: a person, or the daemon by its rule. */
function evicts(row: CapacityEntry): string {
  if (row.evicted_by === "person") {
    switch (row.at_limit) {
      case "rotate":
        return "滿了輪替，舊的分段只有你能刪"
      case "refuse":
        return "滿了拒絕新的，只有你能騰出空間"
      default:
        // store.db today: its class refuses at the limit, and nothing does
        // yet (the row's deviation says so).
        return "滿了只回報、還不會拒絕，只有你能騰出空間"
    }
  }
  switch (row.at_limit) {
    case "refuse":
      return "滿了拒絕新的，daemon 不丟已有的"
    case "evict_oldest":
      return "滿了由 daemon 淘汰最舊的"
    case "expire":
      return "過了視窗由 daemon 讓它過期"
    case "rotate":
      return "滿了由 daemon 輪替、刪最舊的分段"
    case "coalesce":
      return "滿了只留最新的值"
    case "disconnect":
      return "滿了斷開跟不上的讀者"
    case "summarize":
      return "滿了先摘要再移走"
    default:
      return "滿了只回報，不拒絕也不淘汰"
  }
}

const pushWord: Record<NonNullable<CapacityEntry["last_push"]>["push"], string> = {
  pending: "推播待送",
  sending: "推播送出中",
  pushed: "已推播",
  not_subscribed: "沒有裝置訂閱推播",
  failed: "推播送不出去",
  unknown: "不確定推播有沒有送出",
}

/** When the row last told anybody, and whether that reached a push service. */
function lastAlert(row: CapacityEntry): string {
  const tells = row.told.includes("notice")
  if (!row.last_notice_at && !row.last_push) return tells ? "沒有告警過" : "不推播，只記在 diagnostics"
  const at = row.last_push && row.last_push.at >= (row.last_notice_at ?? 0) ? row.last_push.at : row.last_notice_at!
  const push = row.last_push ? `，${pushWord[row.last_push.push]}` : tells ? "" : "，不推播"
  const held = row.notices_suppressed > 0 ? `（另有 ${row.notices_suppressed} 則被一天一則擋下）` : ""
  return `上次告警 ${ago(at)}${push}${held}`
}

function counters(row: CapacityEntry): string {
  const parts: string[] = []
  if (row.refused) parts.push(`拒絕 ${row.refused}`)
  if (row.evicted) parts.push(`淘汰 ${row.evicted}`)
  if (row.expired) parts.push(`過期 ${row.expired}`)
  if (row.rotated) parts.push(`輪替 ${row.rotated}`)
  if (row.dropped) parts.push(`丟掉 ${row.dropped}`)
  if (row.coalesced) parts.push(`合併 ${row.coalesced}`)
  if (row.disconnected) parts.push(`斷線 ${row.disconnected}`)
  if (row.write_errors) parts.push(`寫入失敗 ${row.write_errors}`)
  return parts.join(" · ")
}

/** A reading in its row's unit: bytes in binary units, rows as a count. */
function amount(unit: CapacityEntry["unit"], n: number): string {
  if (unit !== "bytes") return `${n} 筆`
  const k = 1024
  if (n >= k * k * k) return `${(n / (k * k * k)).toFixed(1)} GiB`
  if (n >= k * k) return `${(n / (k * k)).toFixed(1)} MiB`
  if (n >= k) return `${(n / k).toFixed(1)} KiB`
  return `${n} B`
}

function when(unix: number): string {
  return new Date(unix * 1000).toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })
}

/**
 * Whether the clock is running.
 *
 * A pass that fired nothing and a scheduler that stopped are both silence, so
 * this reports the pass rather than its effects. `due` is shown next to `fired`
 * because a due schedule whose dispatch was refused is a thing to look at, and
 * `fired` alone cannot show it.
 */
function Clock({ pulse }: { pulse: SchedulerPulse }) {
  if (!pulse.at) {
    return (
      <span className="pill" title="排程器還沒巡過第一輪">
        <i className="dot" />
        clock 尚未巡邏
      </span>
    )
  }
  const stale = Date.now() / 1000 - pulse.at > pulse.tickSeconds * 3
  return (
    <span
      className="pill"
      title={`每 ${pulse.tickSeconds}s 巡一次，上一輪看了 ${pulse.considered} 個排程`}
    >
      <i className={`dot ${stale ? "unknown" : "on"}`} />
      clock {ago(pulse.at)}
      {pulse.due > 0 && ` · ${pulse.due} 到期`}
      {pulse.fired > 0 && ` · ${pulse.fired} 已派`}
      {pulse.note && ` · ${pulse.note}`}
    </span>
  )
}

function Sessions({
  rows,
  fleet,
  onDid,
}: {
  rows: SessionRow[]
  fleet: ReturnType<typeof useFleet>
  onDid: () => void
}) {
  const scan = fleet.snapshot?.scan
  return (
    <section className="panel">
      <header>
        Sessions<span className="count">{fleet.loaded ? rows.length : "—"}</span>
      </header>

      {fleet.error && <p className="refusal">{fleet.error}</p>}

      {/* An empty list only means "nothing is running" when the reading was
          complete. Otherwise it means the reading could not see. */}
      {fleet.loaded && rows.length === 0 && (
        <p className={`empty ${scan?.emptyAuthoritative ? "" : "unread"}`}>
          {scan?.emptyAuthoritative
            ? "沒有 assistant session 在跑。"
            : "這次掃描不完整，所以空白不代表沒有東西在跑。"}
        </p>
      )}

      {!fleet.loaded && <p className="empty">讀取中…</p>}

      {rows.map((r) => (
        <Session key={r.id} row={r} onDid={onDid} />
      ))}
    </section>
  )
}

function Session({ row, onDid }: { row: SessionRow; onDid: () => void }) {
  const blocked = row.closeability.state === "blocked"
  const [open, setOpen] = useState(false)
  return (
    <article className="session" data-work={row.work_state} data-open={open || undefined}>
      <i className="bar" />
      <div className="body">
        <div className="title">
          <button className="name as-button" onClick={() => setOpen((v) => !v)}>
            {row.label || row.cwd || row.id}
          </button>
          <span className="who">{row.assistant ?? "?"}</span>
        </div>
        <div className="meta">
          {[home(row.cwd), row.tty, row.id, row.backend].filter(Boolean).join("  ·  ")}
        </div>
        {open && (
          <>
            <Controls row={row} onDid={onDid} />
            <Transcript id={row.id} />
          </>
        )}
      </div>
      <div className="right">
        <span className="work" data-work={row.work_state}>
          {row.work_state}
        </span>
        <span className="evidence" data-e={row.evidence}>
          {row.state} / {row.evidence}
        </span>
        {blocked && (
          <span className="blocked">
            {row.closeability.reasons.length} 項未了
          </span>
        )}
        {row.closeability.state === "unknown" && (
          <span className="blocked" style={{ color: "var(--unknown)" }}>
            closeability 未知
          </span>
        )}
      </div>
    </article>
  )
}

function Obligations() {
  const read = useMemo(() => () => client.obligations(), [])
  const { data, error, pending } = usePoll(read)
  return (
    <section className="panel">
      <header>
        Obligations
        <span className="count">{data ? `${data.obligations.length} · ${data.stuck} stuck` : "—"}</span>
      </header>
      {error && <p className="refusal">{error}</p>}
      {pending && !data && <p className="empty">讀取中…</p>}
      {data?.obligations.length === 0 && <p className="empty">沒有未了的事。</p>}
      {data?.obligations.map((o) => <ObligationRow key={o.id} ob={o} />)}
    </section>
  )
}

function ObligationRow({ ob }: { ob: Obligation }) {
  return (
    <div className="ob" data-esc={ob.escalation}>
      <span className="kind">{ob.kind}</span>
      <span className="note">{ob.note || ob.subject}</span>
      <span className="mover">{ob.mover.kind === "person" ? "你" : ob.mover.id || ob.mover.kind}</span>
      <span className="age">{age(ob.age_seconds)}</span>
    </div>
  )
}

function Tasks() {
  const read = useMemo(() => () => client.tasks(), [])
  const { data, error, pending } = usePoll(read)
  return (
    <section className="panel">
      <header>
        Tasks<span className="count">{data ? data.tasks.length : "—"}</span>
      </header>
      {error && <p className="refusal">{error}</p>}
      {pending && !data && <p className="empty">讀取中…</p>}
      {data?.tasks.length === 0 && <p className="empty">沒有進行中的派工。</p>}
      {data?.tasks.map((t) => <TaskRowView key={t.task_id} task={t} />)}
    </section>
  )
}

function TaskRowView({ task }: { task: TaskRow }) {
  return (
    <div className="row">
      <span className="k">{task.assistant}</span>
      <span className="grow">{task.project_dir}</span>
      <span className="v">
        {task.state}
        {task.claims.length > 0 && ` · ${task.claims.length} claims`}
      </span>
    </div>
  )
}

function Schedules() {
  const read = useMemo(() => () => client.schedules(), [])
  const { data, error, pending } = usePoll(read, 15000)
  return (
    <section className="panel">
      <header>
        Schedules<span className="count">{data ? data.schedules.length : "—"}</span>
      </header>
      {error && <p className="refusal">{error}</p>}
      {pending && !data && <p className="empty">讀取中…</p>}
      {data?.schedules.length === 0 && <p className="empty">沒有排程。</p>}
      {data?.schedules.map((s) => <ScheduleRowView key={s.id ?? s.file} row={s} />)}
    </section>
  )
}

function ScheduleRowView({ row }: { row: ScheduleRow }) {
  // A row the daemon could not parse is listed as the file it is, not as a
  // schedule somebody switched off: one is a decision, the other a thing to fix.
  if (row.state === "invalid") {
    const problem = scheduleErrorCopy(row, nextWord)
    return (
      <div className="row unreadable">
        <span className="k">讀不懂</span>
        <div className="grow">
          {row.file} — {problem.sentence}
          {problem.detail && (
            <details className="schedule-error-details">
              <summary>{problem.detailsLabel}</summary>
              <code>{problem.detail}</code>
            </details>
          )}
        </div>
        <span className="v">已停用</span>
      </div>
    )
  }
  const next = row.next_fire ? new Date(row.next_fire * 1000).toLocaleString() : "—"
  return (
    <div className="row" style={{ opacity: row.enabled ? 1 : 0.5 }}>
      <span className="k">{next}</span>
      <span className="grow">{row.title}</span>
      <span className="v">{row.last_run ? ago(row.last_run.at) : "還沒跑過"}</span>
    </div>
  )
}

function Coordinator() {
  const read = useMemo(() => () => client.coordinator(), [])
  const { data, error, pending } = usePoll(read, 10000)
  return (
    <section className="panel">
      <header>
        Coordinator
        <span className="count">{data ? data.liveness : "—"}</span>
      </header>
      {error && <p className="refusal">{error}</p>}
      {pending && !data && <p className="empty">讀取中…</p>}
      {data && !data.registered && (
        <p className="empty">
          這台機器沒有登記協調者。
          {data.liveness === "unknown" && " 而且這次讀取不完整，所以這不算是確定的。"}
        </p>
      )}
      {data?.record && (
        <div className="row">
          <span className="k">{data.record.assistant}</span>
          <span className="grow">{data.record.label || data.record.terminalId}</span>
          <span className="v">gen {data.record.generation}</span>
        </div>
      )}
      {data && data.candidates.length > 0 && (
        <div className="row">
          <span className="k">候選</span>
          <span className="grow">{data.candidates.join(" ")}</span>
        </div>
      )}
    </section>
  )
}

/** Shortens the home directory, which is on every row and says nothing. */
function home(path: string | undefined): string | undefined {
  if (!path) return path
  return path.replace(/^\/Users\/[^/]+/, "~").replace(/^\/home\/[^/]+/, "~")
}

function age(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`
  return `${Math.floor(seconds / 86400)}d`
}

function ago(unix: number): string {
  return `${age(Math.max(0, Math.floor(Date.now() / 1000) - unix))} 前`
}

/**
 * What a person can do to one session from here.
 *
 * The three verbs are the ones the daemon owns, and each reports what actually
 * happened rather than what was intended: a send says the bytes were typed, not
 * that the assistant read them. A blocked close shows the reasons it came back
 * with, because sending somebody to the terminal to find out why is the round
 * trip this whole screen exists to remove.
 */
function Controls({ row, onDid }: { row: SessionRow; onDid: () => void }) {
  const [text, setText] = useState("")
  const [busy, setBusy] = useState<string | null>(null)
  const [said, setSaid] = useState<string | null>(null)
  const [blocked, setBlocked] = useState<readonly CloseReason[]>([])
  const [confirmClose, setConfirmClose] = useState(false)

  const run = async (name: string, fn: () => Promise<unknown>) => {
    setBusy(name)
    setSaid(null)
    setBlocked([])
    try {
      await fn()
      setSaid(name === "send" ? "打進去了（不代表它讀了）" : name === "interrupt" ? "已送出中斷" : "已關閉")
      if (name === "send") setText("")
      onDid()
    } catch (err) {
      if (err instanceof RefusalError) {
        setSaid(err.detail)
        setBlocked(err.reasons)
      } else {
        setSaid(err instanceof Error ? err.message : String(err))
      }
    } finally {
      setBusy(null)
      setConfirmClose(false)
    }
  }

  return (
    <div className="controls" onClick={(e) => e.stopPropagation()}>
      <form
        className="compose"
        onSubmit={(e) => {
          e.preventDefault()
          if (text.trim()) void run("send", () => client.send(row.id, text))
        }}
      >
        <input
          value={text}
          placeholder="打一行字進去…"
          onChange={(e) => setText(e.target.value)}
          disabled={busy !== null}
        />
        <button type="submit" disabled={busy !== null || !text.trim()}>
          送出
        </button>
      </form>

      <div className="verbs">
        <button
          onClick={() => void run("interrupt", () => client.interrupt(row.id))}
          disabled={busy !== null}
        >
          中斷
        </button>

        {/* Closing cannot be undone, so it takes two clicks. The second one
            says what it will do, rather than repeating the first one's word. */}
        {!confirmClose ? (
          <button className="danger" onClick={() => setConfirmClose(true)} disabled={busy !== null}>
            關閉…
          </button>
        ) : (
          <>
            <button
              className="danger"
              onClick={() => void run("close", () => client.close(row.id))}
              disabled={busy !== null}
            >
              確定關掉 {row.id}
            </button>
            <button onClick={() => setConfirmClose(false)}>取消</button>
          </>
        )}

        {blocked.length > 0 && (
          <button
            className="danger"
            onClick={() => void run("close", () => client.close(row.id, true))}
            disabled={busy !== null}
          >
            仍然關閉（{blocked.length} 項未了）
          </button>
        )}
      </div>

      {said && <p className="said">{said}</p>}
      {blocked.map((r, i) => (
        <p key={i} className="said blocked-reason">
          {r.code} · {r.mover.person_needed ? "你" : r.mover.self ? "這個 session" : r.mover.kind} · {r.subject_kind} {r.subject_id}
        </p>
      ))}
    </div>
  )
}

/**
 * Dispatching work from the screen.
 *
 * Claims get a field of their own rather than being inferred from the project
 * directory, because the daemon refuses a dispatch that declares nothing and
 * that refusal is the point: an undeclared task cannot be arbitrated against
 * another root. An empty box means "none", which is a declaration; there is no
 * way from here to mean "I did not say".
 */
function Dispatch({ onDid }: { onDid: () => void }) {
  const [open, setOpen] = useState(false)
  const [assistant, setAssistant] = useState<"claude" | "codex">("claude")
  const [dir, setDir] = useState("")
  const [claims, setClaims] = useState("")
  const [brief, setBrief] = useState("")
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState<string | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setSaid(null)
    try {
      const res = await client.dispatch({
        assistant,
        project_dir: dir,
        instructions: brief,
        claims: claims
          .split("\n")
          .map((s) => s.trim())
          .filter(Boolean),
      })
      // `replayed` is not a failure and not a second start; it means this
      // dispatch had already been made and the original outcome is the answer.
      setSaid(res.replayed ? `已經派過了：${res.task_id}` : `派出去了：${res.task_id}`)
      setBrief("")
      onDid()
    } catch (err) {
      setSaid(err instanceof RefusalError ? `${err.code} — ${err.detail}` : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="panel">
      <header>
        派工
        <button className="count as-link" onClick={() => setOpen((v) => !v)}>
          {open ? "收起" : "展開"}
        </button>
      </header>
      {!open && <p className="empty">開一個新的 assistant session 來做一件事。</p>}
      {open && (
        <form className="dispatch" onSubmit={submit}>
          <div className="verbs">
            {(["claude", "codex"] as const).map((a) => (
              <button
                key={a}
                type="button"
                className={assistant === a ? "picked" : ""}
                onClick={() => setAssistant(a)}
              >
                {a}
              </button>
            ))}
          </div>
          <input
            value={dir}
            placeholder="專案目錄，例如 /Users/you/code/thing"
            onChange={(e) => setDir(e.target.value)}
          />
          <textarea
            value={claims}
            placeholder="claims：這件工作會寫到的路徑，一行一個（留空＝宣告不寫任何東西）"
            rows={2}
            onChange={(e) => setClaims(e.target.value)}
          />
          <textarea
            value={brief}
            placeholder="要它做什麼"
            rows={4}
            onChange={(e) => setBrief(e.target.value)}
          />
          <button type="submit" disabled={busy || !dir.trim() || !brief.trim()}>
            {busy ? "派工中…" : "派出去"}
          </button>
          {said && <p className="said">{said}</p>}
        </form>
      )}
    </section>
  )
}

/** Compact token counts. Exact numbers in the billions are unreadable. */
function tokens(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(2)}B`
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`
  if (n >= 1e3) return `${Math.round(n / 1e3)}k`
  return String(n)
}

/**
 * What each running session has spent, as its own transcript records it.
 *
 * A row that could not be read shows why and carries no number, rather than a
 * zero: zero is a measurement, and "we could not look" is not.
 */
function Usage() {
  const read = useMemo(() => () => client.usage(), [])
  const { data, error, pending } = usePoll(read, 20000)
  return (
    <section className="panel">
      <header>
        用量<span className="count">{data ? `${tokens(data.totalTokens)} tokens` : "—"}</span>
      </header>
      {error && <p className="refusal">{error}</p>}
      {pending && !data && <p className="empty">讀取中…</p>}
      {data?.sessions.map((u) => (
        <div className="row" key={u.id}>
          <span className="k">{u.assistant ?? "?"}</span>
          <span className="grow">{u.label || u.id}</span>
          {u.evidence === "none" ? (
            <span className="v unread" title={u.note}>
              讀不到
            </span>
          ) : (
            <span className="v">{tokens(u.totalTokens)}</span>
          )}
        </div>
      ))}
      {data && (
        <p className="empty footnote">
          讀的是各自 transcript 裡自己記的數字。它算得到這段對話自己的回合，算不到別的 model
          替它做的事——所以這是下限，不是總額。
        </p>
      )}
    </section>
  )
}

/**
 * The last few turns of one session.
 *
 * Tool calls are shown as their name. Reproducing every payload would put the
 * log file on the screen, which is the thing a person opened this to avoid.
 */
function Transcript({ id }: { id: string }) {
  const read = useMemo(() => () => client.transcript(id, 12), [id])
  const { data, error, pending } = usePoll(read, 4000)
  if (pending && !data) return <p className="said">讀取中…</p>
  if (error) return <p className="said">{error}</p>
  if (!data) return null
  if (data.evidence === "none") return <p className="said">{data.note ?? "讀不到這個 session 的紀錄"}</p>
  return (
    <div className="transcript">
      {data.entries.length === 0 && <p className="said">這份紀錄裡還沒有可讀的回合。</p>}
      {data.entries.map((t, i) => (
        <div className="turn" key={i} data-role={t.role}>
          <span className="who">{t.tool ? t.tool : t.role}</span>
          <span className="what">{t.text || <em>（工具呼叫）</em>}</span>
        </div>
      ))}
    </div>
  )
}
