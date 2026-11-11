import { useCallback, useEffect, useRef, useState } from "react"
import { nextWord } from "../../next-strings.js"
import type { MachineManager } from "../../cloud/machine-manager.js"

/**
 * A machine's browser invitations, in its sheet's Name and access tab
 * (docs/browser-invite.md): make a code for a browser with no account, compare
 * the digits that browser shows, allow or deny, and revoke an invited browser
 * later. Every step is one Cloud word to the machine; the code and the digits
 * never leave this page and the machine except inside encrypted envelopes.
 */

type Expiry = "8h" | "7d" | "never"

interface InviteView {
  invite_id: string
  phase: string
  expiry: string
  code?: string
  code_expires_at?: number
  comparison_code?: string
  browser_name?: string
  admission_ends_at?: number
  error?: string
}

interface InvitedRow { device_id: string; name?: string; expires_at: number; active: boolean; paired_at: number }

const POLL_MS = 2_000
const LIVE = new Set(["waiting", "exchanging", "confirm_code", "admitting"])

function codeOf(error: unknown): string {
  const code = (error as { code?: unknown })?.code
  return typeof code === "string" && code ? code : error instanceof Error ? error.message : "unknown"
}

function when(seconds: number): string {
  try {
    return new Intl.DateTimeFormat(document.documentElement.lang || undefined,
      { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(seconds * 1000))
  } catch { return new Date(seconds * 1000).toLocaleString() }
}

function readView(answer: Record<string, unknown>): InviteView | null {
  return typeof answer.invite_id === "string" && typeof answer.phase === "string" ? answer as unknown as InviteView : null
}

export function BrowserInvites({ machineID, machineName, manager }: { machineID: string; machineName: string; manager: MachineManager }) {
  const [expiry, setExpiry] = useState<Expiry>("8h")
  const [invite, setInvite] = useState<InviteView | null>(null)
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState<string | null>(null)
  const [browsers, setBrowsers] = useState<InvitedRow[] | null>(null)
  const live = useRef(true)
  useEffect(() => { live.current = true; return () => { live.current = false } }, [])

  const ask = manager.ask
  const serves = manager.serves?.(machineID, "browser-invite-start") === true && Boolean(ask)
  const invited = manager.invited?.() === true

  const refreshList = useCallback(async () => {
    if (!ask) return
    try {
      const answer = await ask(machineID, "browser-invite-list", {}, "read")
      if (live.current && Array.isArray(answer.browsers)) setBrowsers(answer.browsers as InvitedRow[])
    } catch { /* the list is a convenience; the invitation itself reports its own errors */ }
  }, [ask, machineID])

  useEffect(() => { if (serves && !invited) void refreshList() }, [serves, invited, refreshList])

  // A live invitation is read every two seconds while this page shows it.
  useEffect(() => {
    if (!invite || !LIVE.has(invite.phase) || !ask) return
    const timer = setTimeout(async () => {
      try {
        const answer = readView(await ask(machineID, "browser-invite-status", { invite_id: invite.invite_id }, "read"))
        if (live.current && answer) {
          setInvite(answer)
          if (answer.phase === "admitted") void refreshList()
        }
      } catch (error) {
        if (live.current) setSaid(nextWord("invError", { code: codeOf(error) }))
      }
    }, POLL_MS)
    return () => clearTimeout(timer)
  }, [invite, ask, machineID, refreshList])

  if (invited) return <section><p className="am-label">{nextWord("invTitle")}</p><p className="am-say">{nextWord("invInvitedHere")}</p></section>
  if (!serves) return <section><p className="am-label">{nextWord("invTitle")}</p><p className="am-say">{nextWord("invUnavailable")}</p></section>

  const act = async (word: string, body: Record<string, unknown>) => {
    if (!ask) return
    setBusy(true)
    setSaid(null)
    try {
      const answer = readView(await ask(machineID, word, body, "action"))
      if (live.current && answer) setInvite(answer)
    } catch (error) {
      if (live.current) setSaid(nextWord("invError", { code: codeOf(error) }))
    } finally {
      if (live.current) setBusy(false)
    }
  }

  const revoke = async (device: string) => {
    if (!ask) return
    try {
      await ask(machineID, "browser-invite-revoke", { device_id: device }, "action")
      await refreshList()
    } catch (error) {
      if (live.current) setSaid(nextWord("invRevokeFailed", { code: codeOf(error) }))
    }
  }

  const ended = invite && !LIVE.has(invite.phase)
  return (
    <section aria-labelledby="inv-title" className="inv">
      <p className="am-label" id="inv-title">{nextWord("invTitle")}</p>
      {!invite || ended ? (
        <div className="am-card inv-card">
          {ended && <p className="am-say inv-result" role="status">{resultWord(invite!, machineName)}</p>}
          <p className="am-say">{nextWord("invIntro")}</p>
          <fieldset className="inv-expiry">
            <legend className="am-sub">{nextWord("invExpiryLabel")}</legend>
            {(["8h", "7d", "never"] as const).map((choice) => (
              <label key={choice} className={expiry === choice ? "on" : ""}>
                <input type="radio" name={"inv-expiry-" + machineID} value={choice} checked={expiry === choice}
                  onChange={() => setExpiry(choice)} />
                {nextWord(choice === "8h" ? "invExpiry8h" : choice === "7d" ? "invExpiry7d" : "invExpiryNever")}
              </label>
            ))}
          </fieldset>
          {expiry === "never" && <p className="am-say inv-warn">{nextWord("invNeverWarning", { machine: machineName })}</p>}
          <button type="button" className="am-accent am-wide" disabled={busy}
            onClick={() => void act("browser-invite-start", { expiry })}>{ended ? nextWord("invAnother") : nextWord("invStart")}</button>
        </div>
      ) : invite.phase === "confirm_code" || invite.phase === "admitting" ? (
        <div className="am-card inv-card" role="alertdialog" aria-labelledby="inv-compare">
          <p className="inv-head" id="inv-compare">{nextWord("invCompareTitle", { machine: machineName })}</p>
          <p className="am-sub">{invite.browser_name}</p>
          <p className="inv-digits">{(invite.comparison_code ?? "").slice(0, 3)} {(invite.comparison_code ?? "").slice(3)}</p>
          <p className="am-say">{nextWord("invCompareHint")}</p>
          <p className="am-say inv-warn">{nextWord("invOperateWarning")}</p>
          <div className="am-pair-buttons">
            <button type="button" className="am-ghost" disabled={busy || invite.phase === "admitting"}
              onClick={() => void act("browser-invite-decide", { invite_id: invite.invite_id, approve: false })}>{nextWord("invDeny")}</button>
            <button type="button" className="am-accent" disabled={busy || invite.phase === "admitting"}
              onClick={() => void act("browser-invite-decide", { invite_id: invite.invite_id, approve: true })}>
              {invite.phase === "admitting" ? nextWord("invAdmitting") : nextWord("invAllow")}</button>
          </div>
        </div>
      ) : (
        <div className="am-card inv-card" aria-live="polite">
          {invite.code && <p className="inv-code">{invite.code}</p>}
          {invite.code && invite.code_expires_at ? <p className="am-say">{nextWord("invCodeHow", { time: when(invite.code_expires_at) })}</p> : null}
          <p className="am-say inv-warn">{nextWord("invCodeSecret")}</p>
          <p className="am-say">{invite.phase === "exchanging" ? nextWord("invExchanging") : nextWord("invWaiting")}</p>
          <button type="button" className="am-ghost am-wide" disabled={busy}
            onClick={() => void act("browser-invite-cancel", { invite_id: invite.invite_id })}>{nextWord("invCancel")}</button>
        </div>
      )}
      {said && <p className="am-say" role="status">{said}</p>}
      <p className="am-label inv-list-label">{nextWord("invListTitle")}</p>
      {browsers === null ? null : browsers.length === 0 ? (
        <p className="am-say">{nextWord("invListEmpty")}</p>
      ) : (
        <ul className="am-card">
          {browsers.map((row) => (
            <li key={row.device_id} className="am-card-row">
              <span className="am-card-text">
                <span>{row.name || row.device_id}</span>
                <span className="am-sub">{!row.active ? nextWord("invEnded")
                  : row.expires_at ? nextWord("invUntil", { time: when(row.expires_at) }) : nextWord("invUntilRevoked")}</span>
              </span>
              {row.active && <button type="button" className="am-ghost am-small" onClick={() => void revoke(row.device_id)}>{nextWord("invRevoke")}</button>}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function resultWord(invite: InviteView, machine: string): string {
  switch (invite.phase) {
    case "admitted":
      return invite.admission_ends_at
        ? nextWord("invAdmitted", { browser: invite.browser_name || "Browser", machine, time: when(invite.admission_ends_at) })
        : nextWord("invAdmittedNever", { browser: invite.browser_name || "Browser", machine })
    case "denied": return nextWord("invDenied")
    case "cancelled": return nextWord("invCancelled")
  }
  if (invite.error === "expired") return nextWord("invFailedExpired")
  if (invite.error === "code_mismatch") return nextWord("invFailedMismatch")
  return nextWord("invFailedOther", { code: invite.error || invite.phase })
}
