import { useCallback, useMemo, useState } from "react"
import type {
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
import { useFleet, usePoll } from "./useFleet.js"
import "./dashboard.css"

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

/** `/v1/diagnostics`, or null when this browser may not read it. */
async function readDiagnostics(): Promise<Diagnostics | null> {
  const res = await fetch(client.url("/v1/diagnostics"), { credentials: "same-origin" })
  if (res.status === 401 || res.status === 403) return null
  if (!res.ok) throw new Error(`/v1/diagnostics answered ${res.status}`)
  return (await res.json()) as Diagnostics
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
