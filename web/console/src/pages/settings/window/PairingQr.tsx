import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import type { CloudPairing } from "../cloud.js"
import { W, fill } from "./copy.js"
import { QR_QUIET, encodeQr, qrPath, type QrCode } from "./qr.js"

/**
 * The pairing QR: the link drawn as a code a phone can read off this screen,
 * how long it has left, and the order to do things in on an iPhone.
 *
 * ## Why the sizes are what they are
 *
 * A camera reads a QR by its modules, not by its pixels, so the one number that
 * decides whether it scans is how many screen pixels each module gets. The
 * pairing link is about 270 bytes; at level L that is version 10, 57 modules a
 * side and 65 with the quiet zone. Drawn inline at 6 CSS pixels a module it is
 * 390 pixels square, which is what the default window's right-hand column
 * holds — 12 device pixels a module on a Retina screen, a little over a
 * millimetre on a laptop panel. A click fills the window at whatever whole
 * number of pixels a module fits, because a person holding a phone over a
 * laptop wants the biggest target there is.
 *
 * Every size is a whole number of pixels per module and the SVG is drawn with
 * crisp edges, so no module is ever smeared across two pixels. That smearing
 * is exactly what made a scaled-down screenshot of this code unreadable.
 *
 * ## Why it renews itself
 *
 * The control plane gives an invitation three minutes, and on an iPhone the
 * honest order — install, open from the Home Screen, sign in, scan — easily
 * takes longer than that. So a code that runs out while this window is on
 * screen is replaced by a fresh one, a few times, and then the card stops and
 * offers a button: an unattended window must not keep drawing invitations
 * nobody will scan.
 */

/** Inline, a module gets at most this many CSS pixels and at least the floor below. */
const INLINE_MODULE_MAX = 6
const INLINE_MODULE_MIN = 3

/**
 * How many expired codes in a row are replaced without anybody asking. Three
 * renewals is about twelve minutes of a scannable code, which covers the whole
 * iPhone install-and-sign-in path; past that the window waits for a click.
 */
export const AUTO_RENEW_LIMIT = 3

/** Below this, the countdown says so in the accent colour. */
const HURRY_MS = 30_000

type Props = {
  pairing: CloudPairing
  busy: boolean
  /** Ask the daemon for a fresh invitation. */
  onRenew: () => void
}

export function PairingQr({ pairing, busy, onRenew }: Props) {
  const now = useClock(pairing.phase === "waiting" || pairing.phase === "failed")
  const [enlarged, setEnlarged] = useState(false)
  // Renewals in a row that nobody asked for, the invitation the last one
  // replaced, whether one is still on its way, and the last code on screen.
  const renewals = useRef(0)
  const renewedFor = useRef("")
  const autoPending = useRef(false)
  const shown = useRef("")

  const link = pairing.phase === "waiting" ? pairing.link ?? "" : ""
  const code = useMemo(() => (link ? encodeQr(link, "L") : null), [link])
  const deadline = pairing.expires_at ? pairing.expires_at * 1000 : 0
  const remaining = deadline ? deadline - now : Infinity
  const live = code !== null && remaining > 0
  if (live && pairing.invitation_id && pairing.invitation_id !== shown.current) {
    // A new code on screen. If this card did not ask for it, a person did —
    // the card's own button or the one under it — and the count starts over.
    if (!autoPending.current) renewals.current = 0
    autoPending.current = false
    shown.current = pairing.invitation_id
  }
  const lapsed = lapsedInvitation(pairing, now)

  // Renew a code this card was showing when it ran out — never one it only
  // found expired, such as the last one after this window was reopened an hour
  // later — once per invitation, only while somebody can see this, and only up
  // to the limit. The id is remembered so a re-render, or React's development
  // double effect, does not draw two.
  useEffect(() => {
    if (!lapsed || lapsed !== shown.current || busy || renewedFor.current === lapsed) return
    if (renewals.current >= AUTO_RENEW_LIMIT) return
    if (typeof document !== "undefined" && document.visibilityState !== "visible") return
    renewedFor.current = lapsed
    renewals.current += 1
    autoPending.current = true
    onRenew()
  }, [lapsed, busy, onRenew])

  // A pairing that finished resets the count and closes the enlarged view:
  // the next code is a new errand. (A renewal keeps the view open on purpose —
  // the person is still holding the phone up to it.)
  useEffect(() => {
    if (pairing.phase !== "paired" && pairing.phase !== "idle") return
    renewals.current = 0
    setEnlarged(false)
  }, [pairing.phase])

  const renewByHand = () => {
    renewals.current = 0
    autoPending.current = false
    // The code this replaces is dealt with; it must not be renewed again
    // when its expiry is read once more on the way to the new one.
    renewedFor.current = lapsed
    onRenew()
  }

  if (live) {
    return (
      <div className="sw-qr">
        <span className="sw-block-label">{W.pairingScanTitle}</span>
        <FittedQr code={code} onOpen={() => setEnlarged(true)} />
        {deadline ? <Countdown remaining={remaining} deadline={deadline} renewed={renewals.current} /> : null}
        <ScanOrder />
        {enlarged ? <FullQr code={code} remaining={remaining} onClose={() => setEnlarged(false)} /> : null}
      </div>
    )
  }

  if (!lapsed) return null
  return (
    <div className="sw-qr">
      <span className="sw-block-label">{W.pairingScanTitle}</span>
      <div className="sw-qr-lapsed">
        <p>
          {busy
            ? W.webCloudPairRenewing
            : renewals.current >= AUTO_RENEW_LIMIT
              ? fill(W.webCloudPairGaveUp, { count: String(AUTO_RENEW_LIMIT) })
              : W.webCloudPairExpired}
        </p>
        <button type="button" className="sw-qr-renew" disabled={busy} onClick={renewByHand}>
          {W.webCloudPairRenew}
        </button>
      </div>
    </div>
  )
}

/**
 * Whether a failure the daemon reports is simply the invitation running out.
 *
 * The daemon marks an invitation failed when its window closes, stamped at or
 * after the deadline. A failure stamped before it is some other failure — a
 * refused offer, a mismatch — which a fresh code does not fix and whose reason
 * the person needs to read, so it is neither renewed nor hidden.
 */
export function expiredFailure(pairing: CloudPairing): boolean {
  if (pairing.phase !== "failed" || !pairing.invitation_id || !pairing.expires_at) return false
  return !pairing.error_at || pairing.error_at >= pairing.expires_at
}

/**
 * The invitation id that has run out, or "". Two readings of one event: the
 * daemon still says `waiting` because its poll has not noticed yet, or it
 * already says `failed` because it has.
 */
function lapsedInvitation(pairing: CloudPairing, now: number): string {
  if (!pairing.invitation_id || !pairing.expires_at) return ""
  if (pairing.phase === "waiting" && now >= pairing.expires_at * 1000) return pairing.invitation_id
  return expiredFailure(pairing) ? pairing.invitation_id : ""
}

/** Now, ticking twice a second while `running`, so a countdown never skips a second. */
function useClock(running: boolean): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!running) return
    setNow(Date.now())
    const timer = setInterval(() => setNow(Date.now()), 500)
    return () => clearInterval(timer)
  }, [running])
  return now
}

/** The code at a whole number of pixels per module, as large as the column allows up to the cap. */
function FittedQr({ code, onOpen }: { code: QrCode; onOpen: () => void }) {
  const box = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  useLayoutEffect(() => {
    const element = box.current
    if (!element) return
    const measure = () => setWidth(element.clientWidth)
    measure()
    // The card sits under the Cloud status in a window 660 points tall, so a
    // code that has just appeared is often half below the fold. Bring the
    // whole of it into view — "nearest" moves nothing when it already is.
    element.scrollIntoView?.({ block: "nearest" })
    if (typeof ResizeObserver === "undefined") return
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  const span = code.size + QR_QUIET * 2
  const fits = width ? Math.floor(width / span) : INLINE_MODULE_MAX
  const module = Math.max(INLINE_MODULE_MIN, Math.min(INLINE_MODULE_MAX, fits))
  return (
    <div className="sw-qr-fit" ref={box}>
      <button
        type="button"
        className="sw-qr-code"
        onClick={onOpen}
        title={W.webCloudPairEnlarge}
        aria-label={W.webCloudPairEnlarge}
        data-qr-modules={code.size}
        data-qr-module-px={module}
      >
        <QrSvg code={code} pixels={span * module} />
      </button>
    </div>
  )
}

function QrSvg({ code, pixels }: { code: QrCode; pixels: number }) {
  const span = code.size + QR_QUIET * 2
  const path = useMemo(() => qrPath(code), [code])
  return (
    <svg
      className="sw-qr-svg"
      width={pixels}
      height={pixels}
      viewBox={`0 0 ${span} ${span}`}
      shapeRendering="crispEdges"
      aria-hidden="true"
    >
      <rect width={span} height={span} fill="#fff" />
      <path d={path} fill="#000" />
    </svg>
  )
}

function Countdown({ remaining, deadline, renewed }: { remaining: number; deadline: number; renewed: number }) {
  const hurry = remaining <= HURRY_MS
  return (
    <p className={"sw-qr-countdown" + (hurry ? " hurry" : "")} role="timer" aria-live="off">
      {fill(W.webCloudPairRemaining, {
        time: clock(remaining),
        at: new Date(deadline).toLocaleTimeString(),
      })}
      {renewed > 0 ? fill(W.webCloudPairRenewed, { count: String(renewed) }) : ""}
      <span className="sw-qr-hint">{W.webCloudPairEnlarge}</span>
    </p>
  )
}

/** m:ss, rounded up, so the last second reads 0:01 rather than 0:00. */
function clock(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000))
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`
}

/**
 * The order, said where the code is, because on an iPhone the wrong order is
 * work thrown away: a Home Screen app keeps its storage apart from Safari, and
 * the keys are non-extractable, so a browser paired in Safari cannot hand them
 * to the app installed afterwards. The button name is the hosted console's own
 * words, which are English there.
 */
function ScanOrder() {
  return (
    <div className="sw-qr-order">
      <span className="sw-qr-order-head">{W.webCloudPairOrderHead}</span>
      <ol>
        <li>{W.webCloudPairOrderInstall}</li>
        <li>{W.webCloudPairOrderOpen}</li>
        <li>{W.webCloudPairOrderSignIn}</li>
        <li>{W.webCloudPairOrderScan}</li>
      </ol>
      <p className="sw-qr-order-warn">{W.webCloudPairOrderWhy}</p>
      <p className="sw-qr-order-other">{W.webCloudPairOrderOther}</p>
    </div>
  )
}

/**
 * The code filling the window, on white, for a phone held over a laptop.
 * Any click or Escape closes it; it keeps counting down, and it follows a
 * renewal, because the code it draws is whatever the card is drawing.
 */
function FullQr({ code, remaining, onClose }: { code: QrCode; remaining: number; onClose: () => void }) {
  const [view, setView] = useState(() => ({ w: window.innerWidth, h: window.innerHeight }))
  useEffect(() => {
    const resize = () => setView({ w: window.innerWidth, h: window.innerHeight })
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose()
    }
    window.addEventListener("resize", resize)
    window.addEventListener("keydown", key)
    return () => {
      window.removeEventListener("resize", resize)
      window.removeEventListener("keydown", key)
    }
  }, [onClose])
  const span = code.size + QR_QUIET * 2
  // Room for the line of text underneath; everything else is the code.
  const module = Math.max(1, Math.floor(Math.min(view.w - 24, view.h - 72) / span))
  return (
    <div
      className="sw-qr-full"
      role="dialog"
      aria-modal="true"
      aria-label={W.pairingScanTitle}
      onClick={onClose}
      data-qr-module-px={module}
    >
      <QrSvg code={code} pixels={span * module} />
      <p className={"sw-qr-full-line" + (remaining <= HURRY_MS ? " hurry" : "")}>
        {fill(W.webCloudPairRemainingShort, { time: clock(remaining) })} · {W.webCloudPairClose}
      </p>
    </div>
  )
}
