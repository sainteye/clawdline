import { useEffect, useLayoutEffect, useRef, useState } from "react"
import * as L from "../legacy/bridge.js"
import { NAVIGATION_MARK } from "../brand-mark.js"
import { nextWord } from "../next-strings.js"
import { focusModalContainer } from "../modal-focus.js"
import { pairingAppLink, pairingCommand, type PairState } from "./pair.js"
import { catalogWord } from "../catalog.js"
import { nativePairing, nativePairingAvailable } from "./native-pairing.js"
import { instaBoxWakeReason, wakeBeforeAssistedPairing } from "./instabox-wake.js"
import { PairScanner } from "./PairScanner.js"
import {
  agentOutcome,
  agentSaid,
  defaultPairAssistant,
  elapsedSaid,
  handPairToAgent,
  listenPairAgent,
  localPairing,
  pairAgentAvailable,
  pairAgentMatches,
  pairingOfferDigest,
  progressSaid,
  type PairAssistant,
  type PairAgentProgress,
  type PairAgentReply,
} from "./pair-agent.js"

/**
 * What the gate was asked to pair, and from where.
 *
 * `offer` is the machine list's way (a row, a Devices card, or the door of a
 * browser that holds no key yet): this browser shows a code for the machine.
 * `invitation` is the other direction: the page was opened from a link a
 * machine printed. A link that could not be read carries why, and is said
 * rather than dropped.
 */
export type PairDevice = "app" | "phone" | "desktop"
export type PairTarget = { id: string; name: string; instaBox?: boolean }

export type PairRequest =
  | { mode: "offer"; machine: PairTarget | null; startLocal?: boolean }
  | { mode: "invitation"; ok: boolean; code: string }

export function PairingProgressButton({ onShow }: { onShow: () => void }) {
  const resume = useRef<HTMLButtonElement>(null)
  useLayoutEffect(() => { resume.current?.focus({ preventScroll: true }) }, [])
  return <button ref={resume} className="chip cloud-pair-resume" type="button" onClick={onShow}>
    {nextWord("cloudPairBackgroundProgress")}
  </button>
}

export function PairingOverlay({ pairing, overConsole }: {
  pairing: Parameters<typeof PairPanel>[0]
  overConsole: boolean
}) {
  const mark = useRef<HTMLCanvasElement>(null)
  useLayoutEffect(() => { L.paintIcon(mark.current, NAVIGATION_MARK, 3) }, [])
  return <div className="door" data-step="cloud" data-cloud-screen="pair" hidden={pairing.visible === false}
    data-pair-console={overConsole ? "true" : undefined}>
    <div className="door-card" role="dialog" aria-modal="true" aria-labelledby="cloud-pair-title">
      <div className="door-head">
        <canvas ref={mark} />
        <b>clawdline</b>
      </div>
      <section data-step="cloud"><PairPanel {...pairing} /></section>
    </div>
  </div>
}

/**
 * The pairing card, drawn in the gate's own card in place of whatever it was
 * showing — one dialog at a time, as the forget and rename questions are.
 *
 * It draws a `PairingRun`'s state (`pair.ts`) and nothing else decides here:
 * which words, which fingerprint beside which line, and which one way on. The
 * two fingerprints are on the card for the whole time they can be compared,
 * because the comparison is the step that makes a code carried by hand safe.
 */
export function PairPanel(props: {
  visible?: boolean
  onHide?: () => void
  request: PairRequest
  state: PairState
  /** The name the list shows for a machine id, for a handover from another machine than asked. */
  nameOf: (machine: string) => string
  onBegin: () => void
  onStop: () => void
  onAgain: () => void
  onClose: () => void
  onPaired: () => void
  helpers?: { id: string; name: string; readsQuota?: boolean }[]
  readHelperQuota?: (helper: string) => Promise<unknown>
  wakeOrigin?: string
  targetOnline?: boolean
  onAgentStart?: (helper: string, offer: string, target: { id: string; name: string }, assistant: PairAssistant) => Promise<PairAgentReply>
  onAgentStatus?: (helper: string, task: string) => Promise<PairAgentProgress>
  /** Which device this is: the Home Screen app and a phone browser scan the machine's QR here; a computer copies a code. */
  device: PairDevice
  onScan: (fragment: string) => void
  /** The fingerprints differed: the machine that answered is not to be kept. */
  viewerID?: string
  onMismatch?: (machine: string) => void
}) {
  const { request, state, nameOf, onBegin, onStop, onAgain, onClose, onPaired, device, onScan, onMismatch, visible = true, onHide } = props
  const T = L.strings
  const [localMachine, setLocalMachine] = useState("")
  const [localName, setLocalName] = useState("")
  const [mismatched, setMismatched] = useState(false)
  const [localBusy, setLocalBusy] = useState(false)
  const [localError, setLocalError] = useState("")
  const localRequested = useRef(false)
  const [copied, setCopied] = useState<"" | "yes" | "no">("")
  const [commandCopied, setCommandCopied] = useState<"" | "yes" | "no">("")
  const [codeCopied, setCodeCopied] = useState<"" | "yes" | "no">("")
  // The hand-off to the Mac app's assistant: nothing yet, the native sheet is
  // up, or what the shell answered.
  const [agent, setAgent] = useState<null | "sending" | PairAgentReply>(null)
  // Where the AI task is, from the shell's events, and when it was handed.
  const [progress, setProgress] = useState<PairAgentProgress | null>(null)
  const [startedAt, setStartedAt] = useState<number | null>(null)
  const [now, setNow] = useState(() => Date.now())
  // Other methods stay folded during assisted pairing; an explicit manual start opens them.
  const [manualOpen, setManualOpen] = useState(false)
  const [assistantOverride, setAssistantOverride] = useState<PairAssistant | null>(null)
  const [quotaRead, setQuotaRead] = useState<{ helper: string; snapshot: unknown } | null>(null)
  const [helperFlow, setHelperFlow] = useState(false)
  const [helperID, setHelperID] = useState("")
  const [runningHelper, setRunningHelper] = useState("")
  const waking = useRef<AbortController | null>(null)
  const [wakeStage, setWakeStage] = useState<"idle" | "checking" | "waking">("idle")
  const [wakeError, setWakeError] = useState("")
  const autoHelper = useRef<{ helper: { id: string; name: string }; assistant: PairAssistant } | null>(null)
  const [agentExpected, setAgentExpected] = useState<{ machineID: string; offerDigest: string; expiresAt: number } | null>(null)
  const attempt = useRef(0)
  const completed = useRef(false)
  const localStarted = useRef(false)
  const localAttempt = useRef(0)
  const [localProof, setLocalProof] = useState<{ machineID: string; viewerFingerprint: string; machineFingerprint: string } | null>(null)
  const field = useRef<HTMLTextAreaElement>(null)
  const codeField = useRef<HTMLTextAreaElement>(null)
  const titleRef = useRef<HTMLHeadingElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  const first = useRef<HTMLButtonElement>(null)
  const scanStart = useRef<HTMLButtonElement>(null)
  const phase = state.phase
  const nativeApp = nativePairingAvailable()
  const macBrowser = device === "desktop" && /Mac/i.test(navigator.platform) && !nativeApp
  const [computer, setComputer] = useState<"this" | "other">(macBrowser ? "this" : "other")
  const sameMac = macBrowser && computer === "this"
  const offer = request.mode === "offer"
  const asked = offer ? request.machine ?? (localMachine ? { id: localMachine, name: localName || localMachine } : null) : null
  const requestKey = offer ? asked?.id ?? "" : request.code
  useLayoutEffect(() => () => {
    waking.current?.abort()
    waking.current = null
  }, [phase, requestKey])
  useLayoutEffect(() => {
    if (visible) focusModalContainer(panel.current)
  }, [requestKey, visible])
  useLayoutEffect(() => {
    setWakeStage("idle")
    setWakeError("")
    // Handover can arrive before the helper finishes reporting its observation.
    // Keep following that same task through the paired phase.
    if (phase === "paired") return
    attempt.current++
    completed.current = false
    localAttempt.current++
    setLocalProof(null)
    setAgentExpected(null)
    setMismatched(false)
    setCopied("")
    setCodeCopied("")
    setCommandCopied("")
    setAgent(null)
    setProgress(null)
    setStartedAt(null)
    setManualOpen(phase === "waiting" && !helperFlow && !sameMac)
    setRunningHelper("")
    if (phase === "idle") setHelperFlow(false)
  }, [phase, requestKey])
  const followed = agent !== null && agent !== "sending" && agent.ok && agent.mode === "agent" ? agent.taskID : ""
  const following = followed !== "" && startedAt !== null
  useEffect(() => {
    if (!followed) return
    if (runningHelper && props.onAgentStatus) {
      let active = true
      let pending = false
      const read = () => {
        if (pending || !active) return
        if (agentExpected && Date.now() >= agentExpected.expiresAt) {
          setProgress({ stage: "failed", summary: "", failedReason: nextWord("cloudPairAutoManual") })
          active = false
          return
        }
        pending = true
        void props.onAgentStatus!(runningHelper, followed).then((next) => {
          if (active) setProgress(next)
          if (next.stage === "done" || next.stage === "failed") active = false
        }).catch((error) => {
          if (active) {
            setProgress({ stage: "failed", summary: "", failedReason: error instanceof Error ? error.message : String(error) })
            active = false
          }
        }).finally(() => { pending = false })
      }
      read()
      const timer = setInterval(read, 2000)
      return () => { active = false; clearInterval(timer) }
    }
    return listenPairAgent(followed, (next) => {
      setProgress(next)
    })
  }, [followed, runningHelper, props.onAgentStatus, agentExpected?.expiresAt])
  useEffect(() => {
    if (!following || !agentExpected || progress?.stage === "done" || progress?.stage === "failed") return
    const timer = setTimeout(() => setProgress({ stage: "failed", summary: "", failedReason: nextWord("cloudPairAutoManual") }), Math.max(0, agentExpected.expiresAt - Date.now()))
    return () => clearTimeout(timer)
  }, [following, agentExpected, progress?.stage])
  const running = following && progress?.stage !== "done" && progress?.stage !== "failed"
  useEffect(() => {
    if (!running) return
    setNow(Date.now())
    const tick = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(tick)
  }, [running])

  const selectedHelper = props.onAgentStart ? props.helpers?.find((machine) => machine.id === helperID) ?? props.helpers?.[0] : undefined
  const quotaHelper = !sameMac && asked && selectedHelper?.readsQuota ? selectedHelper.id : null
  const readQuota = props.readHelperQuota
  const quotaPending = !assistantOverride && !!(quotaHelper && readQuota) && quotaRead?.helper !== quotaHelper
  const assistant = assistantOverride ?? defaultPairAssistant(
    quotaRead?.helper === quotaHelper ? quotaRead.snapshot : null, Math.floor(Date.now() / 1000),
  )
  useEffect(() => {
    if (!quotaHelper || !readQuota) return
    let active = true
    const settle = (snapshot: unknown) => { if (active) setQuotaRead({ helper: quotaHelper, snapshot }) }
    void readQuota(quotaHelper).then(settle, () => settle(null))
    return () => { active = false }
  }, [quotaHelper, readQuota])

  async function prepareHelper(target: { id: string; name: string }, launch: () => void) {
    if (waking.current) return
    setWakeError("")
    if (!props.wakeOrigin || props.targetOnline) {
      launch()
      return
    }
    const controller = new AbortController()
    waking.current = controller
    setWakeStage("checking")
    try {
      await wakeBeforeAssistedPairing(props.wakeOrigin, target.id, controller.signal, () => setWakeStage("waking"))
      if (!controller.signal.aborted) launch()
    } catch (error) {
      if (!controller.signal.aborted) setWakeError(instaBoxWakeReason(error))
    } finally {
      if (waking.current === controller) {
        waking.current = null
        setWakeStage("idle")
      }
    }
  }

  function startAgent(helper: { id: string; name: string } | null, target: { id: string; name: string }, choice: PairAssistant) {
    const generation = ++attempt.current
    const fragment = state.phase === "waiting" ? state.fragment : ""
    const expiresAt = state.phase === "waiting" ? state.expiresAt : Date.now()
    setAgentExpected(null)
    void pairingOfferDigest(fragment).then(offerDigest => {
      if (attempt.current === generation) setAgentExpected({ machineID: target.id, offerDigest, expiresAt })
    }).catch(() => {})
    setManualOpen(false)
    setAgent("sending")
    setProgress(null)
    setRunningHelper(helper?.id ?? "")
    const started = helper && props.onAgentStart
      ? props.onAgentStart(helper.id, state.phase === "waiting" ? state.fragment : "", target, choice)
      : handPairToAgent({ offer: state.phase === "waiting" ? state.fragment : "", machineID: target.id, machineName: target.name })
    void started.then((reply) => {
      if (attempt.current !== generation) return
      setAgent(reply)
      if (reply.ok && reply.mode === "agent") setStartedAt(Date.now())
    }).catch((error) => {
      if (attempt.current === generation) setAgent({ ok: false, error: error instanceof Error ? error.message : String(error) })
    })
  }
  useEffect(() => {
    const queued = autoHelper.current
    if (state.phase !== "waiting" || !asked || !queued) return
    autoHelper.current = null
    startAgent(queued.helper, asked, queued.assistant)
  }, [state.phase, state.phase === "waiting" ? state.fragment : "", asked?.id])
  useEffect(() => {
    let active = true
    void localPairing({}).then((reply) => { if (active && reply.ok) setLocalMachine(reply.machineID) })
    if (nativePairingAvailable()) void nativePairing({ kind: "status" }).then(reply => {
      if (active && reply.machine_id) { setLocalMachine(reply.machine_id); setLocalName(reply.machine_name || "") }
    }, () => {})
    return () => { active = false }
  }, [])
  useEffect(() => {
    if (completed.current || !pairAgentMatches(state, progress, agentExpected)) return
    completed.current = true
    onPaired()
  }, [state, progress, agentExpected, onPaired])
  useEffect(() => () => { attempt.current++; localAttempt.current++ }, [])

  const localAvailable = !!asked && asked.id === localMachine
  useEffect(() => {
    if (phase !== "idle" || !localAvailable || !offer || !request.startLocal || localStarted.current) return
    localStarted.current = true
    localRequested.current = true
    onBegin()
  }, [phase, localAvailable, request, onBegin])
  useEffect(() => {
    if (completed.current || state.phase !== "paired" || !localProof ||
        localProof.machineID !== state.machineID || localProof.viewerFingerprint !== state.fingerprint ||
        localProof.machineFingerprint !== state.machineFingerprint) return
    completed.current = true
    onPaired()
  }, [state, localProof, onPaired])
  async function finishLocally(fragment: string) {
    if (!asked || !localAvailable) return
    const generation = ++localAttempt.current
    setLocalBusy(true)
    setLocalError("")
    setLocalProof(null)
    const reply = await localPairing({ machineID: asked.id, offer: fragment })
    if (generation !== localAttempt.current) return
    setLocalBusy(false)
    if (!reply.ok) setLocalError(reply.error)
    else if (reply.comparison) setLocalProof(reply.comparison)
  }
  useEffect(() => {
    if (state.phase === "waiting" && localRequested.current) {
      localRequested.current = false
      void finishLocally(state.fragment)
    }
  }, [state.phase])
  const canHide = !!onHide && (
    wakeStage !== "idle" ||
    phase === "asking" && helperFlow ||
    phase === "waiting" && (helperFlow || agent === "sending" || following) ||
    phase === "paired" && offer
  )
  const title = nativeApp && localAvailable ? catalogWord("browserPairing", "appAccessTitle") : offer
    ? asked
      ? nextWord(device === "app" ? "cloudPairTitleApp" : "cloudPairTitle", { machine: asked.name })
      : nextWord(device === "app" ? "cloudPairTitleAnyApp" : "cloudPairTitleAny")
    : nextWord("cloudPairLinkLede")

  return (
    <div
      className="cloud-pair-ask"
      ref={panel}
      data-pair-phase={phase}
      onKeyDown={(event) => {
        if (event.key !== "Escape") return
        event.preventDefault()
        if (canHide) onHide?.()
        else if (phase === "asking" || phase === "waiting") onStop()
        else if (phase !== "paired") onClose()
      }}
    >
      <h2 className="lede" id="cloud-pair-title" ref={titleRef} tabIndex={-1}>{title}</h2>
      {wakeStage === "idle" ? body() : <>
        <p className="cloud-pair-wait" role="status">
          <span className="cloud-pair-spinner" aria-hidden="true" />
          {nextWord(wakeStage === "waking" ? "msInstaWaking" : "cloudPairHelperPreparing")}
        </p>
        {canHide && <div className="buttons">{hideButton()}</div>}
      </>}
    </div>
  )

  function body() {
    if (request.mode === "invitation" && !request.ok) {
      return (
        <>
          <p className="say" id="cloud-pair-said">
            {request.code === "invitation_expired"
              ? nextWord("cloudPairLinkExpired")
              : nextWord("cloudPairLinkBad", { code: request.code }) // refusal-ok: a link that ran out sends the person to the machine list, and any other unreadable link says its own code in the sentence
            }
          </p>
          {scannerRecovery()}
          <div className="buttons">
            <button className="chip" type="button" id="cloud-pair-close" ref={first} onClick={onClose}>
              {T.webClose}
            </button>
          </div>
        </>
      )
    }
    if (mismatched) return <>
      <p role="alert">{catalogWord("browserPairing", "mismatch")}</p>
      {props.viewerID && <pre>clawdline cloud revoke {props.viewerID}</pre>}
      <button className="chip" type="button" onClick={onClose}>{T.webClose}</button>
    </>
    switch (state.phase) {
      case "idle":
        if (offer) {
          // A phone scans the machine's QR inside this page, so the key lands
          // in this browser's own store; a computer has no camera to point,
          // so it copies a code to paste on the machine.
          const steps = localAvailable
            ? ["cloudPairLocalHint", "cloudPairStepCompare"] as const
            : device === "desktop"
            ? ["cloudPairStepCopy", "cloudPairStepPaste", "cloudPairStepCompare"] as const
            : ["cloudPairStepOpen", "cloudPairStepScanApp", "cloudPairStepCompare"] as const
          const begin = () => {
            autoHelper.current = null
            setHelperFlow(false)
            onBegin()
          }
          return (
            <>
              <p className="cloud-pair-intro">{sameMac ? catalogWord("browserPairing", "sameMacIntro") : nextWord(device === "app" ? "cloudPairIntroApp" : "cloudPairIntro")}</p>
              {macBrowser && <fieldset className="cloud-pair-location">
                <legend>{catalogWord("browserPairing", "where")}</legend>
                <label><input type="radio" name="pair-computer" checked={computer === "this"} onChange={() => setComputer("this")} />{catalogWord("browserPairing", "thisMac")}</label>
                <label><input type="radio" name="pair-computer" checked={computer === "other"} onChange={() => setComputer("other")} />{catalogWord("browserPairing", "otherComputer")}</label>
              </fieldset>}
              {sameMac && <button className="go cloud-pair-primary" type="button" id="cloud-pair-go" onClick={begin}>{catalogWord("browserPairing", "startBrowser")}</button>}
              {!sameMac && selectedHelper && asked && (
                <div className="cloud-pair-helper-way">
                  {(props.helpers?.length ?? 0) > 1 && <label className="cloud-pair-helper">
                    {nextWord("cloudPairHelperChoose")}
                    <select value={selectedHelper.id} onChange={(event) => setHelperID(event.target.value)}>
                      {props.helpers!.map((machine) => <option key={machine.id} value={machine.id}>{machine.name}</option>)}
                    </select>
                  </label>}
                  <p className="fine">{nextWord("cloudPairCloudHandWhen", { helper: selectedHelper.name, machine: asked.name })}</p>
                  {assistantPicker()}
                  <button
                    className="go cloud-pair-primary"
                    type="button"
                    id="cloud-pair-helper-go"
                    disabled={quotaPending}
                    onClick={() => void prepareHelper(asked, () => {
                      setHelperFlow(true)
                      autoHelper.current = { helper: selectedHelper, assistant }
                      onBegin()
                    })}
                  >
                    {nextWord(quotaPending ? "quotaLoading" : "cloudPairHelperStart")}
                  </button>
                  {wakeError && <p className="fine" role="alert">{wakeError}</p>}
                </div>
              )}
              <details className="cloud-pair-manual" open={sameMac || (selectedHelper && asked) ? undefined : true}>
                <summary>{nextWord("cloudPairOtherWays")}</summary>
                <ol className="cloud-pair-steps">
                  {steps.map((step) => <li key={step}>{nextWord(step)}</li>)}
                </ol>
                <p className="cloud-pair-outcome">{nextWord("cloudPairOutcome")}</p>
                {localAvailable ? (
                  <>
                    <button className="go cloud-pair-primary" type="button" id="cloud-pair-local-start"
                      onClick={() => { localRequested.current = true; onBegin() }}>{nextWord("cloudPairLocal")}</button>
                    <p className="fine">{nextWord("cloudPairLocalHint")}</p>
                  </>
                ) : sameMac ? (
                  <button className="chip" type="button" onClick={() => setComputer("other")}>{catalogWord("browserPairing", "otherComputer")}</button>
                ) : device !== "desktop" ? (
                  <PairScanner onScanned={onScan} onUseCode={begin} startRef={scanStart} />
                ) : (
                  <button className="go cloud-pair-primary" type="button" id="cloud-pair-go" onClick={begin}>
                    {nextWord("cloudPairGenerate")}
                  </button>
                )}
              </details>
              <details className="cloud-pair-how">
                <summary>{nextWord("cloudPairHowItWorks")}</summary>
                <p className="fine">{nextWord("cloudPairWhy")}</p>
                {device === "app" && <p className="fine">{nextWord("cloudPairContainerPwa")}</p>}
              </details>
              <div className="buttons">
                <button className="chip" type="button" id="cloud-pair-close" onClick={onClose}>
                  {T.webClose}
                </button>
              </div>
            </>
          )
        }
        return (
          <>
            <p className="cloud-pair-intro">{nextWord("cloudPairLinkFine")}</p>
            <button className="go cloud-pair-primary" type="button" id="cloud-pair-go" ref={first} onClick={onBegin}>
              {nextWord("cloudPairLinkGo")}
            </button>
            <div className="buttons">
              <button className="chip" type="button" id="cloud-pair-close" onClick={onClose}>
                {T.webCancel}
              </button>
            </div>
          </>
        )
      case "asking":
        return (
          <>
            <p className="cloud-pair-wait" role="status"><span className="cloud-pair-spinner" aria-hidden="true" />{nextWord(helperFlow ? "cloudPairHelperPreparing" : "cloudPairAsking")}</p>
            <div className="buttons">
              <button className="chip" type="button" id="cloud-pair-stop" ref={first} onClick={onStop}>
                {T.webCancel}
              </button>
              {canHide && hideButton()}
            </div>
          </>
        )
      case "waiting": {
        const line = pairingCommand(state.fragment, catalogWord("browserPairing", "cliMissing"))
        const target = asked ? `${JSON.stringify(asked.name)} (machine ID ${asked.id})` : nextWord("cloudPairTargetAny")
        const agentPrompt = nextWord("cloudPairAgentPrompt", { machine: target, command: line })
        // Only inside the Mac app's Cloud tab, and only for a named machine:
        // the shell needs to say which machine it is about to reach.
        const helper = props.helpers?.find((machine) => machine.id === helperID) ?? props.helpers?.[0]
        const cloudHandOff = offer && asked && helper && props.onAgentStart ? helper : null
        const handOff = offer && asked && (cloudHandOff || pairAgentAvailable()) ? asked : null
        const send = () => {
          if (!handOff) return
          if (!cloudHandOff) startAgent(null, handOff, assistant)
          else if (!quotaPending) void prepareHelper(handOff, () => startAgent(cloudHandOff, handOff, assistant))
        }
        // The complete offer is the direct path; an agent hand-off is optional.
        const manual = (
          <>
            <p className="cloud-pair-step">
              {asked ? nextWord("cloudPairRun", { machine: asked.name }) : nextWord("cloudPairRunAny")}
            </p>
            <textarea
              className="cloud-pair-command"
              id="cloud-pair-command"
              ref={field}
              readOnly
              rows={10}
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
              value={agentPrompt}
              onFocus={(event) => event.currentTarget.select()}
            />
            <div className="cloud-pair-copy">
              <button className="chip" type="button" id="cloud-pair-copy" onClick={() => copy(agentPrompt, "agent")}>
                {nextWord("cloudPairCopy")}
              </button>
              {copied && (
                <span className="cloud-pair-copied" role="status">
                  {copied === "yes" ? nextWord("cloudPairCopied") : nextWord("cloudPairCopyFailed")}
                </span>
              )}
            </div>
          </>
        )
        return (
          <>
            {offer && (
              <>
                {!handOff && !sameMac && !localAvailable && (
                  <div className="cloud-pair-wait" role="status">
                    <span className="cloud-pair-spinner" aria-hidden="true" />
                    <div>
                      <b id="cloud-pair-said">{nextWord("cloudPairWaiting")}</b>
                      <p className="fine cloud-pair-expires">{nextWord("cloudPairWaitKeepOpen", { time: clock(state.expiresAt) })}</p>
                    </div>
                  </div>
                )}
                {localAvailable && <div className="cloud-pair-native-way">
                  <p role="status">{catalogWord("browserPairing", localBusy ? "allowInApp" : "appAccessHint")}</p>
                  {!localBusy && <button className="go cloud-pair-primary" type="button" id="cloud-pair-local" onClick={() => void finishLocally(state.fragment)}>{nextWord("cloudPairLocal")}</button>}
                  {localError && <p role="alert">{nextWord("cloudPairAgentRefused", { reason: localError })}</p>}
                </div>}
                {sameMac && <div className="cloud-pair-native-way">
                  <p className="cloud-pair-intro">{catalogWord("browserPairing", "openAppHint")}</p>
                  <a className="go cloud-pair-primary" id="cloud-pair-open-app" href={pairingAppLink(state.fragment, asked?.id)}>{catalogWord("browserPairing", "openApp")}</a>
                  <p className="fine" role="status">{catalogWord("browserPairing", "keepBrowser")}</p>
                </div>}
                {handOff && !sameMac && !localAvailable && (
                  <div className="cloud-pair-agent">
                    {cloudHandOff && (props.helpers?.length ?? 0) > 1 && !following && agent !== "sending" && (
                      <label className="cloud-pair-helper">
                        {nextWord("cloudPairHelperChoose")}
                        <select value={cloudHandOff.id} onChange={(event) => setHelperID(event.target.value)}>
                          {props.helpers!.map((machine) => <option key={machine.id} value={machine.id}>{machine.name}</option>)}
                        </select>
                      </label>
                    )}
                    {!following && agent !== "sending" && cloudHandOff && assistantPicker()}
                    {agent === "sending" ? (
                      <div className="cloud-pair-wait" role="status">
                        <span className="cloud-pair-spinner" aria-hidden="true" />
                        <div><b>{nextWord("cloudPairHelperPreparing")}</b><p className="fine">{nextWord("cloudPairAgentSending")}</p></div>
                      </div>
                    ) : following ? (
                      <>
                        {agentProgress(handOff.name, send)}
                        {canHide && progress?.stage !== "failed" && <p className="fine">{nextWord("cloudPairHelperBackground")}</p>}
                      </>
                    ) : (
                      <>
                        <button
                          className="go cloud-pair-primary"
                          type="button"
                          id="cloud-pair-agent"
                          disabled={agent !== null && agent.ok || !!cloudHandOff && quotaPending}
                          onClick={send}
                        >
                          {nextWord(cloudHandOff ? quotaPending ? "quotaLoading" : "cloudPairHelperStart" : "cloudPairAgentHand")}
                        </button>
                        <p className="fine">{cloudHandOff
                          ? nextWord("cloudPairCloudHandWhen", { helper: cloudHandOff.name, machine: handOff.name })
                          : nextWord("cloudPairAgentHandWhen", { machine: handOff.name })}</p>
                        {agent && (
                          <p className="cloud-pair-agent-said" id="cloud-pair-agent-said" role="status" data-agent={agentOutcome(agent)}>
                            {agentSaid(agent, handOff.name, nextWord)}
                          </p>
                        )}
                      </>
                    )}
                    {wakeError && <p className="fine" role="alert">{wakeError}</p>}
                  </div>
                )}
                <details className="cloud-pair-manual" open={manualOpen || (!handOff && !sameMac && !localAvailable)}
                  onToggle={(event) => setManualOpen(event.currentTarget.open)}>
                  <summary>{nextWord("cloudPairOtherWays")}</summary>
                  <ol className="cloud-pair-steps cloud-pair-code-path">
                    <li data-done={codeCopied === "yes" ? "true" : undefined}>
                    <span id="cloud-pair-code-step">{nextWord("cloudPairCopyThis")}</span>
                    <label className="sr-only" htmlFor="cloud-pair-code">{nextWord("cloudPairCodeLabel")}</label>
                    <textarea
                      className="cloud-pair-command cloud-pair-code"
                      id="cloud-pair-code"
                      ref={codeField}
                      readOnly
                      rows={3}
                      spellCheck={false}
                      autoCapitalize="off"
                      autoCorrect="off"
                      value={state.fragment}
                      onFocus={(event) => event.currentTarget.select()}
                    />
                    <div className="cloud-pair-copy">
                      <button className="chip" type="button" id="cloud-pair-code-copy" ref={first} onClick={() => copy(state.fragment, "code")}>
                        {nextWord(codeCopied === "yes" ? "cloudPairCopyDone" : "cloudPairCodeCopy")}
                      </button>
                      {codeCopied === "no" && (
                        <span className="cloud-pair-copied" role="status">{nextWord("cloudPairCopyFailed")}</span>
                      )}
                    </div>
                    </li>
                    <li>{nextWord("cloudPairStepPaste")}
                      <label className="sr-only" htmlFor="cloud-pair-cli">{nextWord("cloudPairCliCopy")}</label>
                      <textarea className="cloud-pair-command" id="cloud-pair-cli" readOnly rows={3}
                        spellCheck={false} autoCapitalize="off" autoCorrect="off" value={line}
                        onFocus={(event) => event.currentTarget.select()} />
                      <button className="chip" type="button" id="cloud-pair-cli-copy" onClick={() => copy(line, "command")}>
                        {nextWord(commandCopied === "yes" ? "cloudPairCopyDone" : "cloudPairCliCopy")}
                      </button>
                      {commandCopied === "no" && <span role="status">{nextWord("cloudPairCopyFailed")}</span>}
                    </li>
                  </ol>
                  <details
                    className="cloud-pair-manual"
                    id="cloud-pair-agent-advanced"
                  >
                    <summary>{nextWord("cloudPairAgentAdvanced")}</summary>
                    {manual}
                  </details>
                  {yourKey(state.fingerprint)}
                </details>
              </>
            )}
            {!offer && (
              <p className="cloud-pair-wait" role="status">
                <span className="cloud-pair-spinner" aria-hidden="true" />
                <b id="cloud-pair-said">{nextWord("cloudPairLinkWaiting")}</b>
              </p>
            )}
            {!offer && yourKey(state.fingerprint)}
            <div className="buttons">
              {canHide ? hideButton() : (
                <button className="chip" type="button" id="cloud-pair-stop" ref={offer ? undefined : first} onClick={onStop}>
                  {T.webCancel}
                </button>
              )}
            </div>
          </>
        )
      }
      case "paired": {
        const name = asked && asked.id === state.machineID ? asked.name : nameOf(state.machineID)
        const assisted = agent !== null && agent !== "sending" && agent.ok && agent.mode === "agent"
        const starting = agent === "sending"
        const verifying = starting || assisted && progress?.stage !== "done" && progress?.stage !== "failed"
        return (
          <>
            {verifying && <p role="status" aria-live="polite">{nextWord("cloudPairAutoChecking")}</p>}
            {assisted && !verifying && !pairAgentMatches(state, progress, agentExpected) && <p role="status">{nextWord("cloudPairAutoManual")}</p>}
            <p className="cloud-pair-question" id="cloud-pair-said" data-paired={state.machineID}>
              {nextWord("cloudPairCompareTitle")}
            </p>
            <p className="cloud-pair-intro">{nextWord("cloudPairCompareFine", { machine: name })}</p>
            {asked && asked.id !== state.machineID && (
              <p className="say">{nextWord("cloudPairOther", { other: name, machine: asked.name })}</p>
            )}
            <div className="cloud-pair-fingerprint">
              <span>{nextWord("cloudPairMachineFingerprint", { machine: name })}</span>
              <b id="cloud-pair-machine-key">{state.machineFingerprint}</b>
            </div>
            {yourKey(state.fingerprint)}
            <div className="cloud-pair-answer">
              <button className="go cloud-pair-primary" type="button" id="cloud-pair-close" ref={first} onClick={onPaired}>
                {nextWord("cloudPairSame")}
              </button>
              {onMismatch && (
                <button className="chip cloud-pair-mismatch" type="button" id="cloud-pair-mismatch" onClick={() => { setMismatched(true); onMismatch(state.machineID) }}>
                  {nextWord("cloudPairDifferent")}
                </button>
              )}
            </div>
            {canHide && <div className="buttons">{hideButton()}</div>}
          </>
        )
      }
      case "stopped":
      case "expired":
      case "refused":
      case "failed": {
        const said =
          state.phase === "stopped"
            ? state.expiresAt
              ? nextWord("cloudPairStopped", { time: clock(state.expiresAt) })
              : nextWord("cloudPairStoppedEarly")
            : state.phase === "expired"
              ? offer
                ? nextWord("cloudPairExpired")
                : nextWord("cloudPairLinkExpired")
              : state.phase === "refused"
                ? nextWord("cloudPairRefused", { code: state.code })
                : nextWord("cloudPairFailed", { code: state.code })
        // A fresh browser offer can recover an expired or lost offer.
        // A machine invitation must still be renewed on that machine.
        const again = offer && (state.phase !== "refused" || state.code === "pairing_gone")
        return (
          <>
            <div className="cloud-pair-result" data-tone={state.phase === "stopped" ? "calm" : "warn"}>
              <b>{nextWord(resultTitle[state.phase])}</b>
              <p id="cloud-pair-said">{said}</p>
            </div>
            {!offer && scannerRecovery()}
            {again && (
              <button className="go cloud-pair-primary" type="button" id="cloud-pair-again" ref={first} onClick={onAgain}>
                {nextWord("cloudPairAgain")}
              </button>
            )}
            <div className="buttons">
              <button className="chip" type="button" id="cloud-pair-close" ref={again ? undefined : first} onClick={onClose}>
                {T.webClose}
              </button>
            </div>
          </>
        )
      }
    }
  }

  function hideButton() {
    return <button className="chip" type="button" id="cloud-pair-hide" onClick={onHide}>{T.webClose}</button>
  }

  function assistantPicker() {
    return <label className="cloud-pair-helper">
      {nextWord("cloudPairHelperAssistant")}
      <select value={assistant} onChange={(event) => setAssistantOverride(event.target.value as PairAssistant)}>
        <option value="claude">Claude Code</option>
        <option value="codex">Codex</option>
      </select>
    </label>
  }

  function scannerRecovery() {
    return device !== "desktop" ? <PairScanner onScanned={onScan} onUseCode={onClose} startRef={scanStart} /> : null
  }

  /**
   * After the native sheet's Start: a spinner, the machine, how long it has
   * been, and where the AI task is in words, from the shell's events. On a
   * failure, the reason, a way to send it again, and the copy path opened.
   */
  function agentProgress(machine: string, send: () => void) {
    const stage = progress?.stage ?? "starting"
    const ended = stage === "done" || stage === "failed"
    const trust = agent !== null && agent !== "sending" && agent.ok ? agent.trust : ""
    return (
      <div className="cloud-pair-progress" id="cloud-pair-progress" data-stage={stage}>
        <p className="cloud-pair-progress-head">
          {!ended && <span className="cloud-pair-spinner" aria-hidden="true" />}
          <b>{stage === "failed" ? nextWord("cloudPairFailedTitle") : nextWord("cloudPairAgentProgressTitle", { machine })}</b>
        </p>
        <p className="cloud-pair-progress-said" id="cloud-pair-agent-said" role="status" aria-live="polite">
          {progressSaid(progress, machine, nextWord)}
        </p>
        {stage === "done" && progress?.summary && <p className="cloud-pair-progress-summary">{progress.summary}</p>}
        {!ended && startedAt !== null && (
          <p className="fine cloud-pair-elapsed">{elapsedSaid((now - startedAt) / 1000, nextWord)}</p>
        )}
        {stage === "starting" && trust === "not_recorded" && <p className="fine">{nextWord("cloudPairAgentTrustAsk")}</p>}
        {stage === "failed" && (
          <button className="go" type="button" id="cloud-pair-agent-retry" onClick={send}>
            {nextWord("cloudPairAgentRetry")}
          </button>
        )}
      </div>
    )
  }

  function yourKey(fingerprint: string) {
    if (!fingerprint) return null
    return (
      <p className="fine cloud-pair-yourkey">
        {nextWord("cloudPairYourKey", { fingerprint: "" })}
        <b id="cloud-pair-browser-key">{fingerprint}</b>
        <span>{nextWord("cloudPairYourKeyCheck")}</span>
      </p>
    )
  }

  function copy(line: string, destination: "code" | "agent" | "command") {
    const done = (ok: boolean) => {
      if (destination === "code") setCodeCopied(ok ? "yes" : "no")
      else if (destination === "command") setCommandCopied(ok ? "yes" : "no")
      else setCopied(ok ? "yes" : "no")
      if (!ok) {
        const target = destination === "command" ? document.getElementById("cloud-pair-cli") as HTMLTextAreaElement | null : destination === "code" ? codeField.current : field.current
        target?.focus({ preventScroll: true })
        target?.select()
      }
    }
    try {
      const clipboard = navigator.clipboard
      if (!clipboard || typeof clipboard.writeText !== "function") {
        done(false)
        return
      }
      clipboard.writeText(line).then(
        () => done(true),
        () => done(false),
      )
    } catch {
      done(false)
    }
  }
}

const resultTitle = {
  stopped: "cloudPairStoppedTitle",
  expired: "cloudPairExpiredTitle",
  refused: "cloudPairRefusedTitle",
  failed: "cloudPairFailedTitle",
} as const

/** A time of day as this page's language writes one: when the code stops working. */
function clock(ms: number): string {
  try {
    return new Date(ms).toLocaleTimeString(document.documentElement.lang || undefined, {
      hour: "2-digit",
      minute: "2-digit",
    })
  } catch {
    return new Date(ms).toISOString()
  }
}
