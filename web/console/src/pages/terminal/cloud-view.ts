import type { Terminal as TerminalRow, TerminalControl } from "@clawdline/contract"

/** What the hosted terminal page shows. With no terminal host there is no live xterm and no key is taken. */
export type CloudTerminalBody = "offline" | "no_project" | "list" | "terminal"

export function cloudTerminalBody(host: unknown, channelProject: string, id: string): CloudTerminalBody {
  if (!host) return "offline"
  if (!channelProject) return "no_project"
  return id ? "terminal" : "list"
}

/** The part of a Cloud terminal session a fresh start uses. */
export interface CloudTerminalStarter {
  start(): Promise<void>
  reconnect(terminal: string): Promise<void>
  attach(terminal: string, reset?: boolean): Promise<{ result?: Record<string, unknown> }>
  request(operation: string, fields?: Record<string, unknown>): Promise<{ result?: Record<string, unknown> }>
}

/**
 * What a new session does when the page (re)connects — after a host change
 * too: it reads the terminal it shows or lists the Project's terminals. It
 * never sends `open` or `input`; those leave only from a person's own action,
 * so one sent before a reconnect is never sent again by it.
 */
export async function beginCloudTerminal(session: CloudTerminalStarter, channelProject: string, id: string, tab: string, started = false):
  Promise<{ meta: TerminalRow } | { rows: TerminalRow[] }> {
  if (!started) await session.start()
  if (id) {
    const answer = await session.attach(id)
    return { meta: answer.result as unknown as TerminalRow }
  }
  return { rows: await listCloudTerminals(session, channelProject, tab) }
}

/** The way back to typing that a refused key's notice offers beside the terminal. */
export type KeyRefusalAction = "acquire" | "takeover" | "reacquire" | "reconnect" | null
/** What a refused key's notice says, and which way back it offers. */
export interface KeyRefusalNotice {
  /** `no_control`, `other_holder` and `unknown` have their own sentences; `other` says `code` in words. */
  kind: "no_control" | "other_holder" | "unknown" | "other"
  code: string
  action: KeyRefusalAction
}

/**
 * Every key the session refused, as the notice the page shows next to the terminal itself. None is
 * left to the status line alone: that line can be off screen while the person types, and a key that
 * vanished with nothing said beside the screen is the failure this replaces.
 */
export function keyRefusalNotice(code: string, control: TerminalControl | null, state: string): KeyRefusalNotice {
  if (code === "terminal_input_state_unknown" || code === "input_state_unknown") return { kind: "unknown", code, action: "reacquire" }
  if (code === "not_controller" || code === "lease_superseded" || code === "lease_expired") {
    if (control?.held && !control.holder?.same_client) return { kind: "other_holder", code, action: "takeover" }
    return { kind: "no_control", code, action: control?.held ? "reacquire" : "acquire" }
  }
  const reconnect = state === "stale" || state === "offline" || code === "terminal_stale" || code === "machine_offline" || code === "machine_stale"
  return { kind: "other", code, action: reconnect ? "reconnect" : null }
}

/** Who holds the terminal, as far as a refused key's notice depends on it. */
export function keyRefusalControl(control: TerminalControl | null): string {
  return control ? `${control.held ? "held" : "free"}/${control.epoch}/${control.holder?.same_client ? "this-tab" : "other"}` : ""
}

/**
 * The refused key's notice while what it says still holds: once control changes (this tab took
 * over, another took it, the lease was forgotten) it is gone. Kept across that change it read
 * "not sent: you (this tab) control this terminal" after 接手, and offered 接手 a second time.
 */
export function shownKeyRefusal<T extends { control: string }>(refusal: T | null, control: TerminalControl | null): T | null {
  return refusal && refusal.control === keyRefusalControl(control) ? refusal : null
}

/** Reconnect the displayed terminal without repeating a create or input request. */
export async function reconnectCloudTerminal(session: CloudTerminalStarter, id: string): Promise<void> {
  await session.reconnect(id)
}

/** Take control on entry when it is free or already belongs to this tab. */
export async function acquireVisibleTerminal(session: Pick<CloudTerminalStarter, "request"> & {
  acquire(action: "acquire"): Promise<void>
}, id: string, tab: string, control: TerminalControl | null, state: string): Promise<"acquired" | "needs_review" | "other_holder" | "unavailable"> {
  if (!id || !control || state === "unknown" || state === "revoked") return "unavailable"
  if (control.held && !control.holder?.same_client) return "other_holder"
  if (control.held) {
    const checked = await session.request("control", { terminal_id: id, client: tab })
    if (checked.result?.input_state_unknown === true) return "needs_review"
  }
  await session.acquire("acquire")
  return "acquired"
}

/** The Project's terminals, asked on the machine's channel by its machine-local Project id. */
export async function listCloudTerminals(session: Pick<CloudTerminalStarter, "request">, channelProject: string, tab: string): Promise<TerminalRow[]> {
  const answer = await session.request("list", { project_id: channelProject, client: tab })
  const terminals = answer.result?.terminals
  if (!Array.isArray(terminals)) throw new Error("terminal_bad_receipt")
  return terminals as TerminalRow[]
}
