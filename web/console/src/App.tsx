import { useCallback, useMemo, useState } from "react"
import { ClawdlineClient, RefusalError, needsYou, sortSessions } from "@clawdline/core"
import type {
  CloseReason,
  Obligation,
  SchedulerPulse,
  ScheduleRow,
  SessionRow,
  TaskRow,
} from "@clawdline/contract"
import { useFleet, usePoll } from "./useFleet.js"

const client = new ClawdlineClient()

export default function App() {
  const fleet = useFleet(client)
  const refresh = useCallback(() => {
    void fleet.refresh()
  }, [fleet])
  const rows = fleet.snapshot ? sortSessions(fleet.snapshot.sessions) : []
  const scan = fleet.snapshot?.scan

  return (
    <div className="shell">
      <header className="topbar">
        <span className="brand">
          Clawdline<span className="go">go</span>
        </span>

        <span className="pill">
          <i className={`dot ${fleet.live ? "on" : "off"}`} />
          {fleet.live ? "live" : "polling"}
        </span>

        {scan && (
          <span className="pill" title="Whether the reading behind this list saw everything">
            <i className={`dot ${scan.complete ? "on" : "unknown"}`} />
            {scan.complete ? "complete" : "partial"} · {scan.provenance}
          </span>
        )}

        {needsYou(rows) > 0 && (
          <span className="pill" style={{ color: "var(--waiting-you)" }}>
            {needsYou(rows)} 需要你
          </span>
        )}

        <span className="spacer" />
        <Health />
      </header>

      <div className="columns">
        <div>
          <Sessions rows={rows} fleet={fleet} onDid={refresh} />
        </div>
        <div>
          <Obligations />
          <Tasks />
          <Schedules />
          <Coordinator />
        </div>
      </div>
    </div>
  )
}

function Health() {
  const read = useMemo(() => () => client.health(), [])
  const { data, error } = usePoll(read, 15000)
  if (error) return <span className="pill"><i className="dot off" />daemon 連不上</span>
  if (!data) return <span className="pill"><i className="dot" />…</span>
  return (
    <>
      <Clock pulse={data.scheduler} />
      <span className="pill">
        <i className="dot on" />
        {data.served_by} :{data.port}
      </span>
    </>
  )
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
        {open && <Controls row={row} onDid={onDid} />}
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
      {data?.schedules.map((s) => <ScheduleRowView key={s.id} row={s} />)}
    </section>
  )
}

function ScheduleRowView({ row }: { row: ScheduleRow }) {
  // An unreadable schedule is switched off by the daemon, not by a person, and
  // the two must not look alike: one is a decision, the other is a thing to fix.
  if (row.unreadable) {
    return (
      <div className="row unreadable">
        <span className="k">讀不懂</span>
        <span className="grow">
          {row.name} — 存的是 <code>{row.when}</code>
        </span>
        <span className="v">已停用</span>
      </div>
    )
  }
  return (
    <div className="row" style={{ opacity: row.enabled ? 1 : 0.5 }}>
      <span className="k">{row.when}</span>
      <span className="grow">{row.name}</span>
      <span className="v">{row.lastRun === 0 ? "還沒跑過" : ago(row.lastRun)}</span>
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
          <span className="grow">{data.record.label || data.record.sessionId}</span>
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
          {r.kind} · {r.mover.kind === "person" ? "你" : r.mover.id || r.mover.kind} · {r.note}
        </p>
      ))}
    </div>
  )
}
