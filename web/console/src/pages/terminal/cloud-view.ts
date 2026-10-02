import type { Terminal as TerminalRow } from "@clawdline/contract"

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
  attach(terminal: string): Promise<{ result?: Record<string, unknown> }>
  request(operation: string, fields?: Record<string, unknown>): Promise<{ result?: Record<string, unknown> }>
}

/**
 * What a new session does when the page (re)connects — after a host change
 * too: it reads the terminal it shows or lists the Project's terminals. It
 * never sends `open` or `input`; those leave only from a person's own action,
 * so one sent before a reconnect is never sent again by it.
 */
export async function beginCloudTerminal(session: CloudTerminalStarter, channelProject: string, id: string, tab: string):
  Promise<{ meta: TerminalRow } | { rows: TerminalRow[] }> {
  await session.start()
  if (id) {
    const answer = await session.attach(id)
    return { meta: answer.result as unknown as TerminalRow }
  }
  return { rows: await listCloudTerminals(session, channelProject, tab) }
}

/** The Project's terminals, asked on the machine's channel by its machine-local Project id. */
export async function listCloudTerminals(session: Pick<CloudTerminalStarter, "request">, channelProject: string, tab: string): Promise<TerminalRow[]> {
  const answer = await session.request("list", { project_id: channelProject, client: tab })
  const terminals = answer.result?.terminals
  if (!Array.isArray(terminals)) throw new Error("terminal_bad_receipt")
  return terminals as TerminalRow[]
}
