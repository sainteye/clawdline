import { useEffect, useRef, useState } from "react"
import type { PairedDevice } from "@clawdline/contract"
import { nextWord } from "../../next-strings.js"
import { TerminalRequestError, hostedConsole, readTerminalMachine, setTerminalGrant } from "./api.js"
import { Switch } from "../settings/window/controls.js"
import { terminalRefusalWords } from "./words.js"
import "./terminal.css"

/**
 * 終端 on one paired device's card: whether that device may open and type in
 * terminals here (`POST /v1/auth/devices/{id}/terminal`). Only the local
 * console changes it; a paired device can never grant itself.
 *
 * A device's `terminal` is absent both when it holds no grant and, for every
 * device, when the machine could not read its grants file (auth.schema.json),
 * so the list alone cannot tell "off" from "unknown".
 *
 * A terminal is a shell as this machine's user, the largest thing a device
 * can be given here, so turning it on asks first and names the device; turning
 * it off does not ask. The switch is the settings window's own (`Switch`). The machine says which
 * (`/v1/diagnostics` `terminals.grants_ok`), read once for the list: until it
 * has, the switches wait; when the file cannot be read, no switch is drawn as
 * off and the reason is said once.
 */
export type TerminalGrants =
  | { kind: "loading" }
  | { kind: "readable" }
  | { kind: "unreadable"; why: string }
  | { kind: "hidden" }

function whyWords(e: unknown): string {
  if (e instanceof TerminalRequestError) return terminalRefusalWords(e.code)
  return e instanceof Error ? e.message : String(e)
}

/** Whether this machine's terminal grants can be read, asked again each time `ask` changes to true. */
export function useTerminalGrants(ask: boolean, again: unknown): TerminalGrants {
  const [grants, setGrants] = useState<TerminalGrants>({ kind: "loading" })
  useEffect(() => {
    if (hostedConsole()) {
      setGrants({ kind: "hidden" })
      return
    }
    if (!ask) return
    let live = true
    readTerminalMachine().then(
      (m) => {
        if (!live) return
        if (!m.terminals) setGrants({ kind: "hidden" }) // a daemon without terminals
        else if (m.terminals.grants_ok) setGrants({ kind: "readable" })
        else setGrants({ kind: "unreadable", why: m.terminals.grants_error || nextWord("terminalRefusalUnreachable") })
      },
      (e) => live && setGrants({ kind: "unreadable", why: whyWords(e) }),
    )
    return () => {
      live = false
    }
  }, [ask, again])
  return grants
}

export function TerminalGrantSwitch({ device, grants, onSaid, onChanged }: {
  device: PairedDevice
  grants: TerminalGrants
  onSaid: (words: string) => void
  /** The machine now holds `granted` for this device. */
  onChanged: (granted: boolean) => void
}) {
  const [granted, setGranted] = useState(device.terminal === true)
  const [busy, setBusy] = useState(false)
  const [asking, setAsking] = useState(false)
  const cancel = useRef<HTMLButtonElement>(null)
  useEffect(() => setGranted(device.terminal === true), [device.terminal])
  useEffect(() => {
    if (asking) cancel.current?.focus()
  }, [asking])
  if (grants.kind === "hidden" || grants.kind === "unreadable") return null
  const waiting = grants.kind === "loading"
  const save = async (want: boolean) => {
    setBusy(true)
    try {
      const answer = await setTerminalGrant(device.id, want)
      setGranted(answer.granted)
      setAsking(false)
      onChanged(answer.granted)
      onSaid(nextWord("terminalGrantSaved", {
        name: device.name,
        state: nextWord(answer.granted ? "terminalGrantOn" : "terminalGrantOff"),
      }))
    } catch (e) {
      onSaid(nextWord("terminalGrantFailed", { name: device.name, why: whyWords(e) }))
    } finally {
      setBusy(false)
    }
  }
  // Giving a shell away asks first, by name, as Revoke does; taking it back is at once.
  const flip = (want: boolean) => (want ? setAsking(true) : void save(false))
  return (
    <div className="terminal-grant" aria-busy={busy || waiting ? "true" : undefined}>
      <div className="terminal-grant-row">
        <Switch on={granted} disabled={busy || waiting || asking} label={nextWord("terminalGrant")} onChange={flip} />
        <span className="terminal-grant-name" aria-hidden="true">{nextWord("terminalGrant")}</span>
        <span className="terminal-grant-state">
          {waiting ? nextWord("terminalGrantReading") : nextWord(granted ? "terminalGrantOn" : "terminalGrantOff")}
        </span>
      </div>
      <p className="device-help terminal-grant-help">{nextWord("terminalGrantHelp")}</p>
      {asking && (
        <div className="signed-in-ask terminal-grant-ask" role="group" aria-label={nextWord("terminalGrant")}>
          <p className="device-help">{nextWord("terminalGrantAsk", { name: device.name })}</p>
          <button className="device-start signed-in-danger" type="button" disabled={busy}
            aria-busy={busy ? "true" : undefined} onClick={() => void save(true)}>
            {nextWord("terminalGrantConfirm")}
          </button>
          <button className="device-start" type="button" ref={cancel} disabled={busy} onClick={() => setAsking(false)}>
            {nextWord("signedInCancel")}
          </button>
        </div>
      )}
    </div>
  )
}

/** Said once above the list when the grants file could not be read. */
export function TerminalGrantsNote({ grants }: { grants: TerminalGrants }) {
  if (grants.kind !== "unreadable") return null
  return <p className="device-help terminal-grants-note" role="note">{nextWord("terminalGrantUnreadable", { why: grants.why })}</p>
}
