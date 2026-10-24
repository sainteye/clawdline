import { useEffect, useLayoutEffect, useRef, useState } from "react"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { focusModalContainer } from "../modal-focus.js"
import { pairingAppLink, pairingCommand, type PairState } from "./pair.js"
import { catalogWord } from "../catalog.js"
import { nativePairing, nativePairingAvailable } from "./native-pairing.js"
import { PairScanner } from "./PairScanner.js"
import {
  agentOutcome,
  agentSaid,
  elapsedSaid,
  handPairToAgent,
  listenPairAgent,
  localPairing,
  pairAgentAvailable,
  progressSaid,
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

export type PairRequest =
  | { mode: "offer"; machine: { id: string; name: string } | null }
  | { mode: "invitation"; ok: boolean; code: string }

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
  request: PairRequest
  state: PairState
  /** The name the list shows for a machine id, for a handover from another machine than asked. */
  nameOf: (machine: string) => string
  onBegin: () => void
  onStop: () => void
  onAgain: () => void
  onClose: () => void
  onPaired: () => void
  helpers?: { id: string; name: string }[]
  onAgentStart?: (helper: string, offer: string, target: { id: string; name: string }) => Promise<PairAgentReply>
  onAgentStatus?: (helper: string, task: string) => Promise<PairAgentProgress>
  /** Which device this is: the Home Screen app and a phone browser scan the machine's QR here; a computer copies a code. */
  device: PairDevice
  onScan: (fragment: string) => void
  /** The fingerprints differed: the machine that answered is not to be kept. */
  viewerID?: string
  onMismatch?: (machine: string) => void
}) {
  const { request, state, nameOf, onBegin, onStop, onAgain, onClose, onPaired, device, onScan, onMismatch } = props
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
  // The manual path: folded until asked for, or until the AI could not finish.
  const [manualOpen, setManualOpen] = useState(false)
  const [helperID, setHelperID] = useState("")
  const [runningHelper, setRunningHelper] = useState("")
  const autoHelper = useRef("")
  const field = useRef<HTMLTextAreaElement>(null)
  const codeField = useRef<HTMLTextAreaElement>(null)
  const titleRef = useRef<HTMLHeadingElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  const first = useRef<HTMLButtonElement>(null)
  const scanStart = useRef<HTMLButtonElement>(null)
  const phase = state.phase
  const offer = request.mode === "offer"
  const asked = offer ? request.machine ?? (localMachine ? { id: localMachine, name: localName || localMachine } : null) : null
  const requestKey = offer ? asked?.id ?? "" : request.code
  useLayoutEffect(() => {
    focusModalContainer(panel.current)
  }, [requestKey])
  useLayoutEffect(() => {
    setMismatched(false)
    setCopied("")
    setCodeCopied("")
    setCommandCopied("")
    setAgent(null)
    setProgress(null)
    setStartedAt(null)
    setManualOpen(false)
    setRunningHelper("")
  }, [phase])
  const followed = agent !== null && agent !== "sending" && agent.ok && agent.mode === "agent" ? agent.taskID : ""
  const following = followed !== "" && startedAt !== null
  useEffect(() => {
    if (!followed) return
    if (runningHelper && props.onAgentStatus) {
      let active = true
      let pending = false
      const read = () => {
        if (pending || !active) return
        pending = true
        void props.onAgentStatus!(runningHelper, followed).then((next) => {
          if (active) setProgress(next)
          if (next.stage === "failed" && active) setManualOpen(true)
          if (next.stage === "done" || next.stage === "failed") active = false
        }).catch((error) => {
          if (active) {
            setProgress({ stage: "failed", summary: "", failedReason: error instanceof Error ? error.message : String(error) })
            setManualOpen(true)
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
      if (next.stage === "failed") setManualOpen(true)
    })
  }, [followed, runningHelper, props.onAgentStatus])
  const running = following && progress?.stage !== "done" && progress?.stage !== "failed"
  useEffect(() => {
    if (!running) return
    setNow(Date.now())
    const tick = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(tick)
  }, [running])

  const selectedHelper = props.helpers?.find((machine) => machine.id === helperID) ?? props.helpers?.[0]
  function startAgent(helper: { id: string; name: string } | null, target: { id: string; name: string }) {
    setAgent("sending")
    setProgress(null)
    setRunningHelper(helper?.id ?? "")
    const started = helper && props.onAgentStart
      ? props.onAgentStart(helper.id, state.phase === "waiting" ? state.fragment : "", target)
      : handPairToAgent({ offer: state.phase === "waiting" ? state.fragment : "", machineID: target.id, machineName: target.name })
    void started.then((reply) => {
      setAgent(reply)
      if (reply.ok && reply.mode === "agent") setStartedAt(Date.now())
    }).catch((error) => setAgent({ ok: false, error: error instanceof Error ? error.message : String(error) }))
  }
  useEffect(() => {
    if (state.phase !== "waiting" || !asked || !autoHelper.current) return
    const helper = props.helpers?.find((machine) => machine.id === autoHelper.current)
    autoHelper.current = ""
    if (helper) startAgent(helper, asked)
  }, [state.phase, state.phase === "waiting" ? state.fragment : "", asked?.id])
  useEffect(() => {
    let active = true
    void localPairing({}).then((reply) => { if (active && reply.ok) setLocalMachine(reply.machineID) })
    if (nativePairingAvailable()) void nativePairing({ kind: "status" }).then(reply => {
      if (active && reply.machine_id) { setLocalMachine(reply.machine_id); setLocalName(reply.machine_name || "") }
    }, () => {})
    return () => { active = false }
  }, [])
  const localAvailable = !!asked && asked.id === localMachine
  async function finishLocally(fragment: string) {
    if (!asked || !localAvailable) return
    setLocalBusy(true)
    setLocalError("")
    const reply = await localPairing({ machineID: asked.id, offer: fragment })
    setLocalBusy(false)
    if (!reply.ok) setLocalError(reply.error)
  }
  useEffect(() => {
    if (state.phase === "waiting" && localRequested.current) {
      localRequested.current = false
      void finishLocally(state.fragment)
    }
  }, [state.phase])
  const title = offer
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
        if (phase === "asking" || phase === "waiting") onStop()
        else if (phase !== "paired") onClose()
      }}
    >
      <h2 className="lede" id="cloud-pair-title" ref={titleRef} tabIndex={-1}>{title}</h2>
      {body()}
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
            autoHelper.current = ""
            onBegin()
          }
          return (
            <>
              <p className="cloud-pair-intro">{nextWord(device === "app" ? "cloudPairIntroApp" : "cloudPairIntro")}</p>
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
              ) : device !== "desktop" ? (
                <PairScanner onScanned={onScan} onUseCode={begin} startRef={scanStart} />
              ) : (
                <button className="go cloud-pair-primary" type="button" id="cloud-pair-go" onClick={begin}>
                  {nextWord("cloudPairGenerate")}
                </button>
              )}
              {selectedHelper && asked && (
                <div className="cloud-pair-helper-way">
                  {(props.helpers?.length ?? 0) > 1 && <label className="cloud-pair-helper">
                    {nextWord("cloudPairHelperChoose")}
                    <select value={selectedHelper.id} onChange={(event) => setHelperID(event.target.value)}>
                      {props.helpers!.map((machine) => <option key={machine.id} value={machine.id}>{machine.name}</option>)}
                    </select>
                  </label>}
                  <button
                    className="cloud-pair-link"
                    type="button"
                    id="cloud-pair-helper-go"
                    onClick={() => {
                      autoHelper.current = selectedHelper.id
                      onBegin()
                    }}
                  >
                    {nextWord("cloudPairCloudHand")}
                  </button>
                  <p className="fine">{nextWord("cloudPairCloudHandWhen", { helper: selectedHelper.name, machine: asked.name })}</p>
                </div>
              )}
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
            <p className="cloud-pair-wait" role="status"><span className="cloud-pair-spinner" aria-hidden="true" />{nextWord("cloudPairAsking")}</p>
            <div className="buttons">
              <button className="chip" type="button" id="cloud-pair-stop" ref={first} onClick={onStop}>
                {T.webCancel}
              </button>
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
        const send = () => { if (handOff) startAgent(cloudHandOff || null, handOff) }
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
                {localAvailable && <div>
                  <button className="go" type="button" id="cloud-pair-local" disabled={localBusy}
                    onClick={() => { void finishLocally(state.fragment) }}>{nextWord("cloudPairLocal")}</button>
                  <p className="fine">{nextWord("cloudPairLocalHint")}</p>
                  {localError && <p role="status">{nextWord("cloudPairAgentRefused", { reason: localError })}</p>}
                </div>}
                {device === "desktop" && /Mac/i.test(navigator.platform) && !localAvailable && <div className="cloud-pair-native-way">
                  <a className="go cloud-pair-primary" href={pairingAppLink(state.fragment, asked?.id)}>{catalogWord("browserPairing", "openApp")}</a>
                  <p className="fine">{catalogWord("browserPairing", "openAppHint")}</p>
                </div>}
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
                <div className="cloud-pair-wait" role="status">
                  <span className="cloud-pair-spinner" aria-hidden="true" />
                  <div>
                    <b id="cloud-pair-said">{nextWord("cloudPairWaiting")}</b>
                    <p className="fine cloud-pair-expires">{nextWord("cloudPairWaitKeepOpen", { time: clock(state.expiresAt) })}</p>
                  </div>
                </div>
                <details
                  className="cloud-pair-manual"
                  id="cloud-pair-agent-advanced"
                  open={manualOpen}
                  onToggle={(event) => setManualOpen(event.currentTarget.open)}
                >
                  <summary>{nextWord("cloudPairAgentAdvanced")}</summary>
                  {handOff && (
                    <div className="cloud-pair-agent">
                      {cloudHandOff && (props.helpers?.length ?? 0) > 1 && !following && (
                        <label className="cloud-pair-helper">
                          {nextWord("cloudPairHelperChoose")}
                          <select value={cloudHandOff.id} disabled={agent === "sending"} onChange={(event) => setHelperID(event.target.value)}>
                            {props.helpers!.map((machine) => <option key={machine.id} value={machine.id}>{machine.name}</option>)}
                          </select>
                        </label>
                      )}
                      {following ? (
                        <>
                          {agentProgress(handOff.name, send)}
                          <p className="fine">{nextWord("cloudPairAgentWindow")}</p>
                        </>
                      ) : (
                        <>
                          <button
                            className="go"
                            type="button"
                            id="cloud-pair-agent"
                            disabled={agent === "sending" || (agent !== null && agent.ok)}
                            onClick={send}
                          >
                            {cloudHandOff ? nextWord("cloudPairCloudHand") : nextWord("cloudPairAgentHand")}
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
                    </div>
                  )}
                  {manual}
                </details>
              </>
            )}
            {!offer && (
              <p className="cloud-pair-wait" role="status">
                <span className="cloud-pair-spinner" aria-hidden="true" />
                <b id="cloud-pair-said">{nextWord("cloudPairLinkWaiting")}</b>
              </p>
            )}
            {yourKey(state.fingerprint)}
            <div className="buttons">
              <button className="chip" type="button" id="cloud-pair-stop" ref={offer ? undefined : first} onClick={onStop}>
                {T.webCancel}
              </button>
            </div>
          </>
        )
      }
      case "paired": {
        const name = asked && asked.id === state.machineID ? asked.name : nameOf(state.machineID)
        return (
          <>
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
          <b>{nextWord("cloudPairAgentProgressTitle", { machine })}</b>
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
