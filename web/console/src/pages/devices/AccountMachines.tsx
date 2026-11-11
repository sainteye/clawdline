import { LoadingSkeleton } from "../../LoadingSkeleton.js"
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react"
import { createPortal } from "react-dom"
import { nextWord } from "../../next-strings.js"
import type { NextWord } from "../../next-strings.js"
import { accountRoster, readAccountMachines, watchAccountMachines } from "../../legacy/devices-bridge.js"
import { machineManager, watchMachineManager, type MachineManager } from "../../cloud/machine-manager.js"
import { machineLight, type CarrierReading } from "../../cloud/carrier-state.js"
import { directHistory, failingSince, watchDirectHistory, type DirectHour } from "../../cloud/direct-history.js"
import type { DirectTry } from "../../cloud/direct-carrier.js"
import type { CloudMachine } from "../../cloud/copied.js"
import { NAME_MAX } from "../../cloud/rename.js"
import { MachinePlatformIcon } from "../../cloud/MachinePlatformIcon.js"
import { sessionsFact } from "../../cloud/unpaired-rows.js"
import { machineSeenWord } from "../../cloud/machine-seen.js"
import { OwnerDiagnostics } from "../../cloud/health-owner/OwnerDiagnostics.js"
import { MachineAccess, cloudAccessSource } from "./MachineAccess.js"
import "./account-machines.css"

/**
 * The hosted console's machine list: one row per machine of the account, and a
 * sheet for the one the person opens.
 *
 * It replaces the copied cards only where an account list is installed. Those
 * cards put every action of every machine on the page at once — a destructive
 * Remove the size of New Session under every machine, an internal id, and two
 * status lines that contradicted each other while the roster loaded. Here the
 * list says what a person scans for (online, Sessions, which road reads take),
 * and the sheet holds the rest in two parts: the connection, with a direct
 * attempt and this browser's own record of them, and the name and access.
 *
 * The console the daemon serves keeps its one card: it has no account list.
 */

/** How often the open page re-reads the list (`console.machine_list_poll_seconds`). */
export const MACHINE_LIST_POLL_MS = 5_000

type Row = CloudMachine & Record<string, unknown>

const nameOf = (m: { name?: string; label?: string; id: string }) => m.name || m.label || m.id

function useManager(): MachineManager | null {
  return useSyncExternalStore(watchMachineManager, machineManager, machineManager)
}

function useHistoryTick(): number {
  const [tick, setTick] = useState(0)
  useEffect(() => watchDirectHistory(() => setTick((t) => t + 1)), [])
  return tick
}

function locale(): string | undefined {
  return typeof document === "undefined" ? undefined : document.documentElement.lang || undefined
}

function clock(at: number): string {
  const when = new Date(at)
  const today = new Date().toDateString() === when.toDateString()
  try {
    return new Intl.DateTimeFormat(locale(), today
      ? { hour: "2-digit", minute: "2-digit" }
      : { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(when)
  } catch {
    return when.toLocaleString()
  }
}

/** What the row's chip and the sheet's heading say about the road, from what is known. */
type Road = "direct" | "relay" | "failing" | "machine_off" | "offline" | "unknown"

function roadOf(machine: Row, reading: CarrierReading | null, history: readonly DirectHour[]): Road {
  const light = machineLight(machine.freshness, reading)
  if (!light || light.show === "unknown") return "unknown"
  if (light.show === "offline") return "offline"
  if (light.show === "direct") return "direct"
  if (light.word === "cloudCarrierRelayMachineOff") return "machine_off"
  return failingSince(history) !== null ? "failing" : "relay"
}

const CHIP: Partial<Record<Road, NextWord>> = {
  direct: "msChipDirect",
  relay: "msChipRelay",
  machine_off: "msChipRelay",
  failing: "msChipFailing",
  offline: "msChipOffline",
}

export function AccountMachines({ shown, onStart }: { shown: boolean; onStart: (machine: string) => void }) {
  const manager = useManager()
  const tick = useHistoryTick()
  const roster = useSyncExternalStore(watchAccountMachines, () => accountRoster().status, () => accountRoster().status)
  const [rows, setRows] = useState<Row[] | null>(null)
  const [openID, setOpenID] = useState<string | null>(null)
  const [told, setTold] = useState<string | null>(null)
  const [, redraw] = useState(0)

  const load = useCallback(async () => {
    const reading = readAccountMachines()
    if (!reading) return
    try {
      const answer = await reading
      setRows(answer.machines as Row[])
    } catch {
      /* the next pass reads again; the rows on screen stay what they were */
    }
  }, [])

  useEffect(() => {
    if (!shown) return
    void load()
    const stop = watchAccountMachines(() => { void load() })
    // The road a machine's reads take changes without any list event, so the
    // rows are re-read on a pace while the page is open.
    const timer = setInterval(() => { void load(); redraw((n) => n + 1) }, MACHINE_LIST_POLL_MS)
    return () => { stop(); clearInterval(timer) }
  }, [shown, load])

  const open = rows?.find((m) => m.id === openID) ?? null
  const retry = accountRoster().retry
  // The access section names one machine: the one picked, else the first paired one.
  const [accessID, setAccessID] = useState<string | null>(null)
  const paired = rows?.filter((m) => m.pairing !== "not_paired") ?? []
  // Picked here, else the machine the header names, else the first paired one.
  const headerID = manager?.headerMachine?.() ?? null
  const accessMachine = paired.find((m) => m.id === accessID) ?? paired.find((m) => m.id === headerID) ?? paired[0] ?? null
  const accessSource = useMemo(() => manager && accessMachine ? cloudAccessSource(manager, accessMachine.id, nameOf(accessMachine)) : null,
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [manager, accessMachine?.id, accessMachine ? nameOf(accessMachine) : ""])

  return (
    <div className="account-machines" data-tick={tick}>
      {accessSource && (
        <MachineAccess source={accessSource} picker={paired.length > 1 ? (
          <div className="access-chips" role="radiogroup" aria-label={nextWord("accessMachineLabel")}>
            {paired.map((m) => (
              <button key={m.id} type="button" role="radio" aria-checked={m.id === accessMachine!.id}
                className={m.id === accessMachine!.id ? "on" : ""} onClick={() => setAccessID(m.id)}>
                <span className="am-icon"><MachinePlatformIcon platform={m.kind ?? ""} /></span>
                <span>{nameOf(m)}</span>
              </button>
            ))}
          </div>
        ) : undefined} />
      )}
      {told && <p className="am-told" role="status">{told}</p>}
      {roster === "failed" && (
        <div className="am-roster-failed">
          <p>{nextWord("devicesRosterFailed")}</p>
          {retry && <button type="button" className="am-ghost" onClick={retry}>{nextWord("devicesRosterRetry")}</button>}
        </div>
      )}
      {rows === null ? (
        <LoadingSkeleton id="SK-04" kind="cards" label={nextWord("msLoading")} />
      ) : rows.length === 0 ? (
        <p className="am-quiet">{nextWord("msEmpty")}</p>
      ) : (
        <ul className="am-list">
          {rows.map((m) => {
            const history = directHistory(m.id)
            const road = roadOf(m, manager?.carrier(m.id) ?? null, history)
            const chip = CHIP[road]
            const sessions = sessionsFact(m)
            const online = m.freshness === "current"
            return (
              <li key={m.id}>
                <button type="button" className="am-row" aria-label={nextWord("msOpen", { machine: nameOf(m) })}
                  aria-haspopup="dialog" onClick={() => { setTold(null); setOpenID(m.id) }}>
                  <span className="am-icon"><MachinePlatformIcon platform={m.kind ?? ""} /></span>
                  <span className="am-row-text">
                    <span className="am-name">{nameOf(m)}</span>
                    <span className="am-meta">
                      <span className={"am-dot" + (online ? " on" : "")} aria-hidden="true" />
                      {m.freshness === "current" ? nextWord("msOnline") : m.freshness === "stale" ? nextWord("msOffline") : nextWord("msNotReported")}
                      {" · "}
                      {m.pairing === "not_paired" ? nextWord("msNotPaired")
                        : typeof sessions === "object" ? (sessions.count === 0 ? nextWord("msNoSessions") : nextWord("msSessions", { count: sessions.count }))
                        : nextWord("msSessionsUnread")}
                    </span>
                  </span>
                  {chip && <span className="am-chip" data-road={road}>{nextWord(chip)}</span>}
                  <svg className="am-chev" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><path d="M9 6l6 6-6 6" /></svg>
                </button>
              </li>
            )
          })}
        </ul>
      )}
      {open && manager && createPortal(
        <MachineSheet
          key={open.id}
          machine={open}
          manager={manager}
          onClose={() => setOpenID(null)}
          onStart={() => { setOpenID(null); onStart(open.id) }}
          onForgotten={(word) => { setOpenID(null); setTold(word); void load() }}
        />,
        document.body,
      )}
    </div>
  )
}

type Tab = "connection" | "access" | "diagnostics"

export function MachineSheet({ machine, manager, onClose, onStart, onForgotten }: {
  machine: Row
  manager: MachineManager
  onClose: () => void
  onStart: () => void
  onForgotten: (word: string) => void
}) {
  const [tab, setTab] = useState<Tab>("connection")
  const [forgetting, setForgetting] = useState(false)
  const [closing, setClosing] = useState(false)
  const afterClose = useRef<(() => void) | null>(null)
  const close = (after = onClose) => {
    if (afterClose.current) return
    afterClose.current = after
    setClosing(true)
  }
  const box = useRef<HTMLDivElement>(null)
  const name = nameOf(machine)

  useEffect(() => {
    const was = document.activeElement as HTMLElement | null
    // The dialog itself takes focus, so a screen reader starts at its name and
    // a tap does not draw a focus ring around the close button.
    box.current?.focus({ preventScroll: true })
    return () => was?.focus?.({ preventScroll: true })
  }, [])

  const onKey = (event: React.KeyboardEvent) => {
    if (event.key === "Escape") { event.preventDefault(); if (forgetting) setForgetting(false); else close() }
  }

  return (
    <div className="am-layer" data-closing={closing} onKeyDown={onKey}>
      <div className="am-scrim" onClick={() => close()} aria-hidden="true" />
      <div className="am-sheet" role="dialog" aria-modal="true" aria-labelledby="am-sheet-title" ref={box} tabIndex={-1}
        inert={closing} onAnimationEnd={(event) => {
          if (event.target === event.currentTarget && event.animationName === "am-sheet-out") afterClose.current?.()
        }}>
        <div className="am-grab" aria-hidden="true" />
        <div className="am-sheet-head">
          <span className="am-icon"><MachinePlatformIcon platform={machine.kind ?? ""} /></span>
          <div className="am-sheet-name">
            <h2 id="am-sheet-title">{name}</h2>
            <p className="am-meta">
              <span className={"am-dot" + (machine.freshness === "current" ? " on" : "")} aria-hidden="true" />
              {machine.freshness === "current" ? nextWord("msOnline") : machine.freshness === "stale" ? nextWord("msOffline") : nextWord("msNotReported")}
              {" · "}{machineSeenWord(machine.observedAt)}
            </p>
          </div>
          <button type="button" className="am-close" aria-label={nextWord("msClose")} onClick={() => close()}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18" /></svg>
          </button>
        </div>
        {forgetting ? (
          <ForgetPanel machine={machine} manager={manager} onCancel={() => setForgetting(false)} onForgotten={(word) => close(() => onForgotten(word))} />
        ) : (
          <>
            <div className="am-tabs" role="tablist">
              <button type="button" role="tab" id="am-tab-connection" aria-selected={tab === "connection"} aria-controls="am-panel"
                className={tab === "connection" ? "on" : ""} onClick={() => setTab("connection")}>{nextWord("msTabConnection")}</button>
              <button type="button" role="tab" id="am-tab-access" aria-selected={tab === "access"} aria-controls="am-panel"
                className={tab === "access" ? "on" : ""} onClick={() => setTab("access")}>{nextWord("msTabNameAccess")}</button>
              <button type="button" role="tab" id="am-tab-diagnostics" aria-selected={tab === "diagnostics"} aria-controls="am-panel"
                className={tab === "diagnostics" ? "on" : ""} onClick={() => setTab("diagnostics")}>{nextWord("ownerDiagTab")}</button>
            </div>
            <div className="am-panel" id="am-panel" role="tabpanel" aria-labelledby={"am-tab-" + tab}>
              {tab === "connection"
                ? <ConnectionTab machine={machine} manager={manager} onStart={() => close(onStart)} />
                : tab === "diagnostics"
                  ? <OwnerDiagnostics machineID={machine.id} />
                  : <AccessTab machine={machine} manager={manager} onForget={() => setForgetting(true)} />}
            </div>
          </>
        )}
      </div>
    </div>
  )
}

const PhoneIcon = () => <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true"><rect x="7" y="3" width="10" height="18" rx="2" /></svg>
const CloudIcon = () => <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true"><path d="M7 18h10a4 4 0 0 0 .5-7.97A6 6 0 0 0 6.1 9.5 4.25 4.25 0 0 0 7 18z" /></svg>
const Check = () => <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5" /></svg>
const Cross = () => <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3.2" strokeLinecap="round" aria-hidden="true"><path d="M7 7l10 10M17 7L7 17" /></svg>

function ConnectionTab({ machine, manager, onStart }: { machine: Row; manager: MachineManager; onStart: () => void }) {
  useHistoryTick()
  const [attempt, setAttempt] = useState<"idle" | "trying" | DirectTry>("idle")
  const [localNetwork, setLocalNetwork] = useState<"idle" | "saving" | "enabled" | "failed">("idle")
  const live = useRef(true)
  useEffect(() => { live.current = true; return () => { live.current = false } }, [])
  const name = nameOf(machine)
  const history = directHistory(machine.id)
  const since = failingSince(history)
  const reading = manager.carrier(machine.id)
  const road = roadOf(machine, reading, history)
  const words = roadWords(road, name, since)

  const tryNow = async () => {
    setAttempt("trying")
    const outcome = await manager.tryDirect(machine.id)
    if (live.current) setAttempt(outcome)
  }
  const enableSameNetwork = async () => {
    if (localNetwork === "saving" || !manager.enableSameNetwork) return
    setLocalNetwork("saving")
    try {
      await manager.enableSameNetwork(machine.id)
    } catch {
      if (live.current) setLocalNetwork("failed")
      return
    }
    if (!live.current) return
    setLocalNetwork("enabled")
    await tryNow()
  }
  const suggestLocal = road === "failing" && machine.pairing !== "not_paired" &&
    manager.sameNetworkAvailable?.(machine.id) === true
  const canTry = road !== "offline" && road !== "machine_off" && road !== "direct" && machine.pairing !== "not_paired"

  return (
    <div className="am-stack">
      <div>
        <h3 className={"am-road-title" + (road === "direct" ? " good" : "")}>{words.title}</h3>
        <p className="am-say">{words.body}</p>
      </div>
      {road !== "offline" && (
        <div className="am-lanes" role="img" aria-label={nextWord("msLanes", { machine: name })}>
          <div className={"am-lane" + (road === "direct" ? " on" : road === "failing" ? " broken" : attempt === "trying" ? " trying" : "")}>
            <span className="am-lane-k">{nextWord("msLaneDirect")}</span>
            <span className="am-node"><PhoneIcon /></span>
            <span className="am-ln" />
            {road === "failing" && <span className="am-break"><Cross /></span>}
            {road === "failing" && <span className="am-ln" />}
            <span className="am-node"><MachinePlatformIcon platform={machine.kind ?? ""} /></span>
          </div>
          <div className={"am-lane" + (road === "direct" ? " off" : " on")}>
            <span className="am-lane-k">{nextWord("msLaneRelay")}</span>
            <span className="am-node"><PhoneIcon /></span>
            <span className="am-ln" />
            <span className="am-node"><CloudIcon /></span>
            <span className="am-ln" />
            <span className="am-node"><MachinePlatformIcon platform={machine.kind ?? ""} /></span>
          </div>
        </div>
      )}
      <HistoryChart history={history} since={since} />
      {attempt !== "idle" && <AttemptResult attempt={attempt} machine={name} />}
      {suggestLocal && localNetwork !== "enabled" && (
        <div className="am-stack">
          <div>
            <h3 className="am-road-title">{nextWord("msSameNetworkTitle")}</h3>
            <p className="am-say">{nextWord("msSameNetworkHint", { machine: name })}</p>
          </div>
          <button type="button" className="am-ghost am-wide" onClick={enableSameNetwork}
            disabled={localNetwork === "saving" || attempt === "trying"} aria-busy={localNetwork === "saving"}>
            {nextWord(localNetwork === "saving" ? "msSameNetworkSaving" : "msSameNetworkEnable")}
          </button>
        </div>
      )}
      {localNetwork === "failed" && <p className="am-say" role="alert">{nextWord("msSameNetworkFailed")}</p>}
      {localNetwork === "enabled" && <p className="am-say" role="status">{nextWord("msSameNetworkEnabled", { machine: name })}</p>}
      {canTry && (
        <button type="button" className="am-ghost am-wide" onClick={tryNow} disabled={attempt === "trying" || localNetwork === "saving"} aria-busy={attempt === "trying"}>
          {attempt === "trying" ? nextWord("msTrying") : nextWord("msTryNow")}
        </button>
      )}
      {machine.pairing === "not_paired" ? (
        <button type="button" className="am-primary am-wide" onClick={() => manager.pair(machine.id)}>{nextWord("msPair")}</button>
      ) : machine.freshness === "current" && (
        <button type="button" className="am-primary am-wide" onClick={onStart}>{nextWord("msNewSession")}</button>
      )}
    </div>
  )
}

function roadWords(road: Road, machine: string, since: number | null): { title: string; body: string } {
  switch (road) {
    case "direct": return { title: nextWord("msDirectTitle"), body: nextWord("msDirectBody", { machine }) }
    case "failing": return { title: nextWord("msFailingTitle"), body: nextWord("msFailingSince", { time: clock(since ?? Date.now()) }) }
    case "machine_off": return { title: nextWord("msRelayTitle"), body: nextWord("cloudCarrierRelayMachineOff", { machine }) }
    case "offline": return { title: nextWord("msOffline"), body: nextWord("cloudCarrierOffline", { machine }) }
    case "unknown": return { title: nextWord("msNotReported"), body: nextWord("cloudCarrierUnknown", { machine }) }
    default: return { title: nextWord("msRelayTitle"), body: nextWord("msRelayBody") }
  }
}

function HistoryChart({ history, since }: { history: readonly DirectHour[]; since: number | null }) {
  const peak = Math.max(0, ...history.map((h) => h.ok + h.failed))
  if (peak === 0) {
    return <div className="am-hist"><p className="am-hist-title">{nextWord("msHistoryTitle")}</p><p className="am-say">{nextWord("msHistoryEmpty")}</p></div>
  }
  const summary = since !== null ? nextWord("msFailingSince", { time: clock(since) }) : ""
  return (
    <div className="am-hist">
      <div className="am-hist-head">
        <p className="am-hist-title">{nextWord("msHistoryTitle")}</p>
      </div>
      <div className="am-bars" role="img" aria-label={summary || nextWord("msHistoryTitle")}>
        {history.map((h) => {
          const total = h.ok + h.failed
          const label = nextWord("msHistoryHour", { time: clock(h.hour * 3_600_000), ok: h.ok, failed: h.failed })
          if (total === 0) return <span key={h.hour} className="am-bar idle" title={label} />
          const height = Math.max(18, Math.round((total / peak) * 100))
          return (
            <span key={h.hour} className="am-bar" style={{ height: height + "%" }} title={label}>
              {h.failed > 0 && <i className="no" style={{ flexGrow: h.failed }} />}
              {h.ok > 0 && <i className="ok" style={{ flexGrow: h.ok }} />}
            </span>
          )
        })}
      </div>
      <div className="am-axis"><span>{nextWord("msHistoryStart")}</span><span>{nextWord("msHistoryNow")}</span></div>
      <div className="am-legend">
        <span><i className="sw ok" />{nextWord("msHistoryOk")}</span>
        <span><i className="sw no" />{nextWord("msHistoryFailed")}</span>
        <span><i className="sw idle" />{nextWord("msHistoryIdle")}</span>
        <span className="am-legend-note">{nextWord("msHistoryHeight")}</span>
      </div>
    </div>
  )
}

function AttemptResult({ attempt, machine }: { attempt: "trying" | DirectTry; machine: string }) {
  if (attempt === "trying") {
    return <p className="am-say" role="status">{nextWord("msTryingBody")}</p>
  }
  switch (attempt.kind) {
    case "open":
      return <p className="am-say am-good" role="status">{nextWord("msTryOpen", { seconds: (attempt.ms / 1000).toFixed(1) })}</p>
    case "busy":
      return <p className="am-say" role="status">{nextWord("msTryBusy", { machine })}</p>
    case "offline":
      return <p className="am-say" role="status">{nextWord("msTryOffline")}</p>
    case "unsupported":
      return <p className="am-say" role="status">{nextWord("cloudCarrierRelayMachineOff", { machine })}</p>
    default: {
      const answered = attempt.kind === "not_connected"
      return (
        <div className="am-stack-tight" role="status">
          <ol className="am-steps">
            {answered ? <>
              <li><span className="am-mark ok"><Check /></span>{nextWord("msStepAnswered", { machine })}</li>
              <li><span className="am-mark ok"><Check /></span>{nextWord("msStepExchanged")}</li>
              <li><span className="am-mark no"><Cross /></span>{nextWord("msStepConnect")}</li>
            </> : (
              <li><span className="am-mark no"><Cross /></span>{nextWord("msStepNoAnswer", { machine })}</li>
            )}
          </ol>
          <p className="am-tips">{nextWord("msTips")}</p>
        </div>
      )
    }
  }
}

function AccessTab({ machine, manager, onForget }: { machine: Row; manager: MachineManager; onForget: () => void }) {
  const name = nameOf(machine)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(name)
  const [saving, setSaving] = useState(false)
  const [said, setSaid] = useState<string | null>(null)
  const field = useRef<HTMLInputElement>(null)
  useEffect(() => { if (editing) field.current?.select() }, [editing])

  const save = async () => {
    setSaving(true)
    const outcome = await manager.rename(machine, draft)
    setSaving(false)
    switch (outcome.kind) {
      case "renamed": setEditing(false); setSaid(nextWord("cloudRenamed", { machine: name, name: outcome.name })); break
      case "blank": setSaid(null); break
      case "too_long": setSaid(nextWord("cloudRenameTooLong", { max: outcome.max })); break
      case "refused": setSaid(nextWord("cloudRenameRefused", { machine: name, code: outcome.code })); break
      case "absent": setSaid(nextWord("cloudRenameAbsent", { machine: name, code: outcome.code })); break
      default: setSaid(nextWord("cloudRenameUnreadable", { machine: name, code: outcome.code }))
    }
  }

  return (
    <div className="am-stack">
      <section aria-labelledby="am-name-label">
        <p className="am-label" id="am-name-label">{nextWord("msNameLabel")}</p>
        {editing ? (
          <form className="am-rename" onSubmit={(e) => { e.preventDefault(); if (!saving) void save() }}>
            <label className="sr-only" htmlFor="am-rename-field">{nextWord("cloudRenameField")}</label>
            <input id="am-rename-field" ref={field} value={draft} maxLength={NAME_MAX} onChange={(e) => setDraft(e.target.value)} disabled={saving} />
            <p className="am-say">{nextWord("msRenameHint")}</p>
            <div className="am-pair-buttons">
              <button type="button" className="am-ghost" onClick={() => { setEditing(false); setDraft(name); setSaid(null) }} disabled={saving}>{nextWord("msCancel")}</button>
              <button type="submit" className="am-accent" disabled={saving || !draft.trim() || draft.trim() === name}>{nextWord("msSave")}</button>
            </div>
          </form>
        ) : (
          <div className="am-card">
            <div className="am-card-row">
              <span className="am-card-text"><span>{name}</span><span className="am-sub">{nextWord("msNameHint")}</span></span>
              <button type="button" className="am-ghost am-small" onClick={() => { setDraft(name); setEditing(true); setSaid(null) }}>{nextWord("msRename")}</button>
            </div>
          </div>
        )}
        {said && <p className="am-say" role="status">{said}</p>}
      </section>
      {manager.invited?.() !== true && (
        <button type="button" className="am-danger am-wide" onClick={onForget}>{nextWord("msForget")}</button>
      )}
    </div>
  )
}




function ForgetPanel({ machine, manager, onCancel, onForgotten }: {
  machine: Row
  manager: MachineManager
  onCancel: () => void
  onForgotten: (word: string) => void
}) {
  const name = nameOf(machine)
  const [typed, setTyped] = useState("")
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState<string | null>(null)
  const matches = typed.trim() === name.trim()

  const go = async () => {
    setBusy(true)
    const outcome = await manager.forget(machine)
    setBusy(false)
    if (outcome.kind === "forgotten") { onForgotten(nextWord("cloudForgotten", { machine: name })); return }
    setSaid(outcome.kind === "refused" ? nextWord("cloudForgetRefused", { machine: name, code: outcome.code })
      : outcome.kind === "absent" ? nextWord("cloudForgetAbsent", { machine: name, code: outcome.code })
      : nextWord("cloudForgetUnreadable", { machine: name, code: outcome.code }))
  }

  return (
    <form className="am-stack" onSubmit={(e) => { e.preventDefault(); if (matches && !busy) void go() }}>
      <h3 className="am-road-title">{nextWord("cloudForgetTitle", { machine: name })}</h3>
      <ul className="am-forget-items">
        {machine.freshness === "current" && <li className="loss">{nextWord("msForgetLive")}</li>}
        <li className="loss">{nextWord("msForgetLose")}</li>
        <li className="loss">{nextWord("msForgetRepair")}</li>
        <li>{nextWord("msForgetKeeps")}</li>
        <li>{nextWord("cloudForgetHonest")}</li>
      </ul>
      <div className="am-field">
        <label htmlFor="am-forget-field">{nextWord("msForgetConfirm")}</label>
        <input id="am-forget-field" value={typed} onChange={(e) => setTyped(e.target.value)} placeholder={name} autoComplete="off" disabled={busy} />
      </div>
      {said && <p className="am-say" role="status">{said}</p>}
      <div className="am-pair-buttons">
        <button type="button" className="am-ghost" onClick={onCancel} disabled={busy}>{nextWord("msCancel")}</button>
        <button type="submit" className="am-danger-fill" disabled={!matches || busy}>
          {busy ? nextWord("cloudForgetting", { machine: name }) : nextWord("msForgetGo")}
        </button>
      </div>
    </form>
  )
}
