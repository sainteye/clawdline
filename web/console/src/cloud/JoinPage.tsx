import { useEffect, useMemo, useRef, useState, type FormEvent, type MouseEvent } from "react"
import { nextWord, type NextWord } from "../next-strings.js"
import { NAVIGATION_MARK } from "../brand-mark.js"
import * as L from "../legacy/bridge.js"
import { chooseTransport, cloudViewerDeviceMetadata, newCloudSession, readCloudConfig } from "./copied.js"
import { InviteError, joinBrowserInvite, type InviteFailure, type JoinStage } from "./browser-invite.js"
import { bootAdminCatalog } from "./admin/words.js"
import "./join.css"

/**
 * `app.clawdline.com/join`: a browser with no Clawdline account types the code
 * an owner's paired device shows, and becomes an invited browser of that one
 * machine (docs/browser-invite.md). Nothing here signs in to GitHub. When the
 * owner allows it, the keys are kept like an ordinary pairing's and the page
 * opens the console, which boots this browser as invited.
 */

const FAILURE: Record<InviteFailure, NextWord> = {
  bad_code: "joinBadCode", unknown_invite: "joinUnknown", invite_used: "joinUsed", invite_expired: "joinExpired",
  rate_limited: "joinRateLimited", denied: "joinDenied", code_mismatch: "joinDenied", device_limit: "joinDeviceLimit",
  network: "joinNetwork", failed: "joinFailed",
}

type View = { at: "type" } | { at: "busy"; stage: JoinStage } | { at: "failed"; word: NextWord; retry: boolean }

/** Refusals after which the very same code can still work: a full account, a busy network. */
const RETRYABLE = new Set<InviteFailure>(["device_limit", "network", "rate_limited"])

export function JoinPage({ declared }: { declared: string }) {
  const config = useMemo(() => {
    try {
      const c = readCloudConfig({ __clawdlineCloud: JSON.parse(declared) })
      return c && chooseTransport({ mock: false, origin: location.origin, config: c }) === "cloud" ? c : null
    } catch { return null }
  }, [declared])
  const [code, setCode] = useState(() => new URLSearchParams(location.search).get("code") ?? "")
  const [view, setView] = useState<View>({ at: "type" })
  const mark = useRef<HTMLCanvasElement>(null)
  const stop = useRef<AbortController | null>(null)
  // The document starts behind `html.booting` (main.tsx) until its words arrive; this page owns
  // lifting it, as every other hosted page does. Without it the page stays blank.
  const [wordsReady, setWordsReady] = useState(false)
  useEffect(() => {
    void bootAdminCatalog(config, document.baseURI).catch(() => {}).finally(() => {
      setWordsReady(true)
      document.documentElement.classList.remove("booting")
    })
  }, [config])
  useEffect(() => { if (wordsReady && mark.current) L.paintIcon(mark.current, NAVIGATION_MARK, 3) }, [wordsReady])
  useEffect(() => () => stop.current?.abort(), [])

  const join = async (event: FormEvent | MouseEvent) => {
    event.preventDefault()
    if (!config || view.at === "busy") return
    const controller = new AbortController()
    stop.current = controller
    setView({ at: "busy", stage: { stage: "joining" } })
    const device = cloudViewerDeviceMetadata(window)
    try {
      const result = await joinBrowserInvite(config.apiOrigin, code, device.name,
        (stage) => setView({ at: "busy", stage }), { signal: controller.signal })
      const session = newCloudSession({ config, deviceKind: device.kind, deviceName: device.name })
      if (!session.adoptInvitedBrowser) throw new InviteError("failed")
      await session.adoptInvitedBrowser(result)
      location.replace("/")
    } catch (error) {
      if (controller.signal.aborted) return
      const code = error instanceof InviteError ? error.code : "failed"
      setView({ at: "failed", word: FAILURE[code], retry: RETRYABLE.has(code) })
    }
  }

  const stage = view.at === "busy" ? view.stage : null
  if (!wordsReady) return null
  return (
    <div className="door join" data-step="cloud">
      <div className="door-card" role="main" aria-labelledby="join-title">
        <div className="door-head">
          <canvas ref={mark} />
          <b>clawdline</b>
        </div>
        <section data-step="cloud">
          <p className="lede" id="join-title">{nextWord("joinTitle")}</p>
          {!config ? (
            <p className="fine" role="alert">{nextWord("joinNetwork")}</p>
          ) : view.at === "type" ? (
            <form onSubmit={join}>
              <p className="fine">{nextWord("joinIntro")}</p>
              <label htmlFor="join-code">{nextWord("joinCodeLabel")}</label>
              <input id="join-code" className="join-code" value={code} onChange={(e) => setCode(e.target.value)}
                autoFocus autoComplete="off" autoCapitalize="characters" spellCheck={false} inputMode="text"
                placeholder="XXXXX-XXXXXX" maxLength={20} />
              <button className="go" type="submit" disabled={code.trim().length < 11}>{nextWord("joinSubmit")}</button>
            </form>
          ) : view.at === "failed" ? (
            <>
              <p className="fine" role="alert">{nextWord(view.word)}</p>
              {view.retry ? (
                <>
                  <button className="go" type="button" onClick={(e) => void join(e)}>{nextWord("joinRetry")}</button>
                  <button className="alt" type="button" onClick={() => { setCode(""); setView({ at: "type" }) }}>{nextWord("joinAgain")}</button>
                </>
              ) : (
                <button className="go" type="button" onClick={() => { setCode(""); setView({ at: "type" }) }}>{nextWord("joinAgain")}</button>
              )}
            </>
          ) : stage?.stage === "compare" ? (
            <div aria-live="polite">
              <p className="fine">{nextWord("joinCompareTitle")}</p>
              <p className="join-digits" aria-label={stage.code.split("").join(" ")}>{stage.code.slice(0, 3)} {stage.code.slice(3)}</p>
              <p className="fine">{nextWord("joinCompareHint")}</p>
              <p className="fine join-wait"><span className="join-spin" aria-hidden="true" />{nextWord("joinWaitingAllow")}</p>
            </div>
          ) : (
            <p className="fine join-wait" aria-live="polite"><span className="join-spin" aria-hidden="true" />
              {stage?.stage === "exchanging" ? nextWord("joinExchanging", { machine: stage.machineName || "…" })
                : stage?.stage === "claiming" ? nextWord("joinClaiming", { machine: stage.machineName || "…" })
                  : nextWord("joinJoining")}
            </p>
          )}
        </section>
      </div>
    </div>
  )
}
