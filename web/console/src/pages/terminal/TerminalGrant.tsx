import { useEffect, useState } from "react"
import type { PairedDevice } from "@clawdline/contract"
import { nextWord } from "../../next-strings.js"
import { TerminalRequestError, hostedConsole, readTerminalMachine, setTerminalGrant } from "./api.js"
import { terminalRefusalWords } from "./words.js"
import "./terminal.css"

/**
 * 終端 on one paired device's card: whether that device may open and type in
 * terminals here (`POST /v1/auth/devices/{id}/terminal`). Only the local
 * console changes it; a paired device can never grant itself.
 *
 * A device's `terminal` is absent both when it holds no grant and, for every
 * device, when the machine could not read its grants file (auth.schema.json),
 * so the list alone cannot tell "off" from "unknown". The machine says which
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

export function TerminalGrantSwitch({ device, grants, onSaid }: {
  device: PairedDevice
  grants: TerminalGrants
  onSaid: (words: string) => void
}) {
  const [granted, setGranted] = useState(device.terminal === true)
  const [busy, setBusy] = useState(false)
  useEffect(() => setGranted(device.terminal === true), [device.terminal])
  if (grants.kind === "hidden" || grants.kind === "unreadable") return null
  const waiting = grants.kind === "loading"
  const flip = async () => {
    const want = !granted
    setBusy(true)
    try {
      const answer = await setTerminalGrant(device.id, want)
      setGranted(answer.granted)
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
  return (
    <div className="terminal-grant">
      <button
        type="button"
        role="switch"
        className="terminal-grant-switch"
        aria-checked={waiting ? undefined : granted}
        aria-busy={busy || waiting ? "true" : undefined}
        disabled={busy || waiting}
        onClick={() => void flip()}
      >
        <span className="terminal-grant-track" aria-hidden="true"><span /></span>
        <span className="terminal-grant-name">{nextWord("terminalGrant")}</span>
        <span className="terminal-grant-state">
          {waiting ? nextWord("terminalGrantReading") : nextWord(granted ? "terminalGrantOn" : "terminalGrantOff")}
        </span>
      </button>
    </div>
  )
}

/** Said once above the list when the grants file could not be read. */
export function TerminalGrantsNote({ grants }: { grants: TerminalGrants }) {
  if (grants.kind !== "unreadable") return null
  return <p className="device-help terminal-grants-note" role="note">{nextWord("terminalGrantUnreadable", { why: grants.why })}</p>
}
