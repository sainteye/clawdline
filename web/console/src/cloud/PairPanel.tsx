import { useLayoutEffect, useRef, useState } from "react"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { pairingCommand, type PairState } from "./pair.js"

/**
 * What the gate was asked to pair, and from where.
 *
 * `offer` is the machine list's way (a row, a Devices card, or the door of a
 * browser that holds no key yet): this browser shows a code for the machine.
 * `invitation` is the other direction: the page was opened from a link a
 * machine printed. A link that could not be read carries why, and is said
 * rather than dropped.
 */
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
  onReload: () => void
}) {
  const { request, state, nameOf, onBegin, onStop, onAgain, onClose, onReload } = props
  const T = L.strings
  const [copied, setCopied] = useState<"" | "yes" | "no">("")
  const field = useRef<HTMLTextAreaElement>(null)
  const first = useRef<HTMLButtonElement>(null)
  const phase = state.phase
  // The one button a person is meant to press next holds the focus the card
  // opens with, as `Cancel` holds it in the forget question.
  useLayoutEffect(() => {
    first.current?.focus({ preventScroll: true })
  }, [phase])
  useLayoutEffect(() => {
    setCopied("")
  }, [phase])

  const offer = request.mode === "offer"
  const asked = offer ? request.machine : null
  const title = offer
    ? asked
      ? nextWord("cloudPairTitle", { machine: asked.name })
      : nextWord("cloudPairTitleAny")
    : nextWord("cloudPairLinkLede")

  return (
    <div
      className="cloud-pair-ask"
      data-pair-phase={phase}
      onKeyDown={(event) => {
        if (event.key !== "Escape") return
        event.preventDefault()
        if (phase === "asking" || phase === "waiting") onStop()
        else if (phase !== "paired") onClose()
      }}
    >
      <p className="lede">{title}</p>
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
          <div className="buttons">
            <button className="chip" type="button" id="cloud-pair-close" ref={first} onClick={onClose}>
              {T.webClose}
            </button>
          </div>
        </>
      )
    }
    switch (state.phase) {
      case "idle":
        // Only a machine's link stops here: a row's press has already begun.
        return (
          <>
            <p className="fine">{nextWord("cloudPairLinkFine")}</p>
            <div className="buttons">
              <button className="chip" type="button" id="cloud-pair-close" onClick={onClose}>
                {T.webCancel}
              </button>
              <button className="chip" type="button" id="cloud-pair-go" ref={first} onClick={onBegin}>
                {nextWord("cloudPairLinkGo")}
              </button>
            </div>
          </>
        )
      case "asking":
        return (
          <>
            {offer && <p className="fine">{nextWord("cloudPairWhy")}</p>}
            <p className="say calm">{nextWord("cloudPairAsking")}</p>
            <div className="buttons">
              <button className="chip" type="button" id="cloud-pair-stop" ref={first} onClick={onStop}>
                {T.webCancel}
              </button>
            </div>
          </>
        )
      case "waiting": {
        const line = pairingCommand(state.fragment)
        return (
          <>
            {offer && (
              <>
                <p className="fine">{nextWord("cloudPairWhy")}</p>
                <p className="cloud-pair-step">
                  {asked ? nextWord("cloudPairRun", { machine: asked.name }) : nextWord("cloudPairRunAny")}
                </p>
                <textarea
                  className="cloud-pair-command"
                  id="cloud-pair-command"
                  ref={field}
                  readOnly
                  rows={4}
                  spellCheck={false}
                  autoCapitalize="off"
                  autoCorrect="off"
                  value={line}
                  onFocus={(event) => event.currentTarget.select()}
                />
                <div className="cloud-pair-copy">
                  <button className="chip" type="button" id="cloud-pair-copy" ref={first} onClick={() => copy(line)}>
                    {nextWord("cloudPairCopy")}
                  </button>
                  {copied && (
                    <span className="cloud-pair-copied" role="status">
                      {copied === "yes" ? nextWord("cloudPairCopied") : nextWord("cloudPairCopyFailed")}
                    </span>
                  )}
                </div>
                <p className="fine cloud-pair-expires">{nextWord("cloudPairExpires", { time: clock(state.expiresAt) })}</p>
              </>
            )}
            {yourKey(state.fingerprint)}
            {offer && <p className="fine">{nextWord("cloudPairSettings")}</p>}
            <p className="say calm" id="cloud-pair-said">
              {offer ? nextWord("cloudPairWaiting") : nextWord("cloudPairLinkWaiting")}
            </p>
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
            <p className="say calm" id="cloud-pair-said" data-paired={state.machineID}>
              {nextWord("cloudPairDone", { machine: name })}
            </p>
            {asked && asked.id !== state.machineID && (
              <p className="say">{nextWord("cloudPairOther", { other: name, machine: asked.name })}</p>
            )}
            <div className="cloud-pair-key">
              <p className="fine">
                {nextWord("cloudPairMachineKey", { machine: name, fingerprint: "" })}
                <b id="cloud-pair-machine-key">{state.machineFingerprint}</b>
              </p>
              <p className="fine">{nextWord("cloudPairMachineKeyCheck")}</p>
            </div>
            {yourKey(state.fingerprint)}
            <div className="buttons">
              <button className="chip" type="button" id="cloud-pair-reload" ref={first} onClick={onReload}>
                {nextWord("cloudPairReload")}
              </button>
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
        // A new code fixes a code that ran out or was abandoned; it does not
        // fix a pairing that was never this browser's, and a machine's link
        // is the machine's to print again.
        const again = offer && state.phase !== "refused"
        return (
          <>
            <p className={state.phase === "stopped" ? "say calm" : "say"} id="cloud-pair-said">
              {said}
            </p>
            <div className="buttons">
              <button className="chip" type="button" id="cloud-pair-close" ref={again ? undefined : first} onClick={onClose}>
                {T.webClose}
              </button>
              {again && (
                <button className="chip" type="button" id="cloud-pair-again" ref={first} onClick={onAgain}>
                  {nextWord("cloudPairAgain")}
                </button>
              )}
            </div>
          </>
        )
      }
    }
  }

  function yourKey(fingerprint: string) {
    if (!fingerprint) return null
    return (
      <div className="cloud-pair-key">
        <p className="fine">
          {nextWord("cloudPairYourKey", { fingerprint: "" })}
          <b id="cloud-pair-browser-key">{fingerprint}</b>
        </p>
        <p className="fine">{nextWord("cloudPairYourKeyCheck")}</p>
      </div>
    )
  }

  function copy(line: string) {
    const done = (ok: boolean) => {
      setCopied(ok ? "yes" : "no")
      if (!ok) {
        field.current?.focus({ preventScroll: true })
        field.current?.select()
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
