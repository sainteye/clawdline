import { useEffect, useRef, useState } from "react"
import { nextWord } from "../../next-strings.js"

/**
 * The top card of the Devices page (`MachineAccess.tsx`), the same on the
 * console a machine serves and on Clawdline Cloud: make an invitation link for
 * a browser with no account, and confirm the pairing when that browser shows
 * its six digits (docs/browser-invite.md). Every step is one invitation word;
 * `ask` carries it, as a Cloud word or as this machine's own route.
 */

type Expiry = "8h" | "7d" | "never"

export type InviteAsk = (word: string, body: Record<string, unknown>, kind: "action" | "read") => Promise<Record<string, unknown>>

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

const POLL_MS = 2_000
const LIVE = new Set(["waiting", "exchanging", "confirm_code", "admitting"])
const JOIN_ORIGIN = "https://app.clawdline.com"

export function codeOf(error: unknown): string {
  const code = (error as { code?: unknown })?.code
  return typeof code === "string" && code ? code : error instanceof Error ? error.message : "unknown"
}

export function when(seconds: number): string {
  try {
    return new Intl.DateTimeFormat(document.documentElement.lang || undefined,
      { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(seconds * 1000))
  } catch { return new Date(seconds * 1000).toLocaleString() }
}

function readView(answer: Record<string, unknown>): InviteView | null {
  return typeof answer.invite_id === "string" && typeof answer.phase === "string" ? answer as unknown as InviteView : null
}

/** The link the invited browser opens; the code rides in it so nothing is typed. */
export function joinLink(code: string): string {
  return `${JOIN_ORIGIN}/join?code=${encodeURIComponent(code)}`
}

export function PairCard({ machineKey, machineName, ask, available, unavailableWord, onChanged }: {
  machineKey: string
  machineName: string
  ask: InviteAsk | null
  /** False when this machine cannot invite here; `unavailableWord` says why. */
  available: boolean
  unavailableWord: "invUnavailable" | "invInvitedHere" | "accessCloudOff" | "accessNotOwner"
  /** Called when an invitation admitted a browser, so the list below reads again. */
  onChanged?: () => void
}) {
  const [expiry, setExpiry] = useState<Expiry>("8h")
  const [invite, setInvite] = useState<InviteView | null>(null)
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const live = useRef(true)
  useEffect(() => { live.current = true; return () => { live.current = false } }, [])
  useEffect(() => { setInvite(null); setSaid(null) }, [machineKey])

  // A live invitation is read every two seconds while this page shows it.
  useEffect(() => {
    if (!invite || !LIVE.has(invite.phase) || !ask) return
    const timer = setTimeout(async () => {
      try {
        const answer = readView(await ask("browser-invite-status", { invite_id: invite.invite_id }, "read"))
        if (live.current && answer) {
          setInvite(answer)
          if (answer.phase === "admitted") onChanged?.()
        }
      } catch (error) {
        if (live.current) setSaid(nextWord("invError", { code: codeOf(error) }))
      }
    }, POLL_MS)
    return () => clearTimeout(timer)
  }, [invite, ask, onChanged])

  const head = <p className="am-label" id="inv-title">{nextWord("accessPairTitle")}</p>
  if (!available || !ask) {
    return <section aria-labelledby="inv-title" className="inv">{head}<div className="am-card inv-card"><p className="am-say">{nextWord(unavailableWord)}</p></div></section>
  }

  const act = async (word: string, body: Record<string, unknown>) => {
    setBusy(true)
    setSaid(null)
    try {
      const answer = readView(await ask(word, body, "action"))
      if (live.current && answer) { setInvite(answer); setCopied(false) }
    } catch (error) {
      if (live.current) setSaid(nextWord("invError", { code: codeOf(error) }))
    } finally {
      if (live.current) setBusy(false)
    }
  }

  const copy = async (text: string) => {
    try { await navigator.clipboard.writeText(text); if (live.current) setCopied(true) } catch { /* the link stays selectable on screen */ }
  }

  const ended = invite && !LIVE.has(invite.phase)
  return (
    <section aria-labelledby="inv-title" className="inv">
      {head}
      {!invite || ended ? (
        <div className="am-card inv-card">
          {ended && <p className="am-say inv-result" role="status">{resultWord(invite!, machineName)}</p>}
          <p className="am-say">{nextWord("invIntro")}</p>
          <fieldset className="inv-expiry">
            <legend className="am-sub">{nextWord("invExpiryLabel")}</legend>
            {(["8h", "7d", "never"] as const).map((choice) => (
              <label key={choice} className={expiry === choice ? "on" : ""}>
                <input type="radio" name={"inv-expiry-" + machineKey} value={choice} checked={expiry === choice}
                  onChange={() => setExpiry(choice)} />
                {nextWord(choice === "8h" ? "invExpiry8h" : choice === "7d" ? "invExpiry7d" : "invExpiryNever")}
              </label>
            ))}
          </fieldset>
          {expiry === "never" && <p className="am-say inv-warn">{nextWord("invNeverWarning", { machine: machineName })}</p>}
          <button type="button" className="am-accent am-wide" disabled={busy}
            onClick={() => void act("browser-invite-start", { expiry })}>{nextWord("accessMakeLink")}</button>
        </div>
      ) : invite.phase === "confirm_code" || invite.phase === "admitting" ? (
        <div className="am-card inv-card" role="alertdialog" aria-labelledby="inv-compare">
          <p className="inv-head" id="inv-compare">{nextWord("accessConfirmTitle", { browser: invite.browser_name || "Browser" })}</p>
          <p className="am-sub">{nextWord("invCompareTitle", { machine: machineName })}</p>
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
          {invite.code && (
            <>
              <p className="inv-link" translate="no">{joinLink(invite.code)}</p>
              <button type="button" className="am-accent am-wide" onClick={() => void copy(joinLink(invite.code!))}>
                {copied ? nextWord("accessCopied") : nextWord("accessCopyLink")}</button>
              <p className="am-say">{nextWord("accessCodeLine", { code: invite.code })}</p>
              {invite.code_expires_at ? <p className="am-say">{nextWord("accessLinkHow", { time: when(invite.code_expires_at) })}</p> : null}
            </>
          )}
          <p className="am-say inv-warn">{nextWord("invCodeSecret")}</p>
          <p className="am-say">{invite.phase === "exchanging" ? nextWord("invExchanging") : nextWord("invWaiting")}</p>
          <button type="button" className="am-ghost am-wide" disabled={busy}
            onClick={() => void act("browser-invite-cancel", { invite_id: invite.invite_id })}>{nextWord("invCancel")}</button>
        </div>
      )}
      {said && <p className="am-say" role="status">{said}</p>}
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
  if (invite.error === "device_limit") return nextWord("invFailedDeviceLimit")
  return nextWord("invFailedOther", { code: invite.error || invite.phase })
}
