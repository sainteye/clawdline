import type { SessionRow, SessionShell, ShellOutputReply } from "@clawdline/contract"

/**
 * What the strip above the composer says about a session's background work:
 * the commands it left running and the subagents its assistant sent away.
 *
 * Claude Code puts the same two things at the very bottom of its screen, and
 * that is where a person looks for them. They used to be drawn inside the
 * folded "Session to-dos" at the top, where nobody found them, and the
 * commands could not be opened at all.
 *
 * Nothing here is imported at run time, so `node --test` reads it as it is.
 */
export interface BackgroundCounts {
  /** Commands still running (the daemon lists only those). */
  shells: number
  /**
   * Subagents still running; null while the provider's reading is not
   * complete, which the strip says as `?` rather than as a zero.
   */
  running: number | null
  /** Subagents listed on the row, running or not. */
  listed: number
  /** Subagents the reading left off the row. */
  truncated: number
}

/** The counts the strip draws, from the row alone. */
export function backgroundCounts(row: SessionRow): BackgroundCounts {
  const reading = row.agents_reading
  const agents = row.agents ?? []
  return {
    shells: row.shells?.length ?? 0,
    running: reading?.state === "complete" ? agents.filter((agent) => agent.state === "running").length : null,
    listed: agents.length,
    truncated: reading?.truncated ?? 0,
  }
}

/**
 * Whether the strip is drawn: only when there is something in it to open.
 *
 * A reading that is not complete is not by itself a reason — every session's
 * first row has one, and a strip that says "subagent ?" under every
 * conversation would be noise where a silent line is the truth. Once there is
 * a command or a listed agent, an unknown reading is said beside it.
 */
export function showsBackground(counts: BackgroundCounts): boolean {
  return counts.shells > 0 || counts.listed > 0 || counts.truncated > 0
}

/** The words the strip's segments are made of, in the reader's language. */
export interface BackgroundWords {
  shellOne: string
  shellMany: string
  agentRunningOne: string
  agentRunningMany: string
  agentListedOne: string
  agentListedMany: string
  agentUnknown: string
}

/** The strip's one line: `2 background shells · 1 subagent running`. */
export function backgroundLine(counts: BackgroundCounts, words: BackgroundWords): string {
  const parts: string[] = []
  const fill = (s: string, n: number) => s.replace("{n}", String(n))
  if (counts.shells > 0) parts.push(fill(counts.shells === 1 ? words.shellOne : words.shellMany, counts.shells))
  const agents = counts.listed + counts.truncated
  if (agents > 0) {
    if (counts.running === null) parts.push(words.agentUnknown)
    else if (counts.running > 0) parts.push(fill(counts.running === 1 ? words.agentRunningOne : words.agentRunningMany, counts.running))
    else parts.push(fill(agents === 1 ? words.agentListedOne : words.agentListedMany, agents))
  }
  return parts.join(" · ")
}

/** Whether anything in the strip is still going, which lights its dot. */
export function backgroundLive(counts: BackgroundCounts): boolean {
  return counts.shells > 0 || (counts.running ?? 0) > 0
}

/** How often an open Shell panel reads the command again. */
export const SHELL_BEAT_MS = 1500

/** How much of the output the panel asks for: the daemon's default window. */
export const SHELL_WINDOW_BYTES = 65536

/**
 * One open Shell panel: what it last read, and whether it should read again.
 *
 * `meta` is the row as it was when the panel opened, replaced by the answer's
 * own row while there is one. A command that ends drops off the session's
 * list, and that is the moment somebody is looking at this panel, so the
 * command line must not vanish with it.
 */
export interface ShellView {
  meta: SessionShell
  text: string
  signature: string
  ended: boolean
  truncated: boolean
  /** Nothing has been read yet. */
  loading: boolean
  /** The last read failed for a reason other than the command being over. */
  error: string
}

/** A panel just opened on a row, before anything has been read. */
export function openShell(meta: SessionShell): ShellView {
  return { meta, text: "", signature: "", ended: false, truncated: false, loading: true, error: "" }
}

/** What one read said: an answer, the command gone (404), or a failure. */
export type ShellRead =
  | { kind: "answer"; reply: ShellOutputReply }
  | { kind: "missing" }
  | { kind: "failed"; sentence: string }

/** The panel after one read, whether it repaints, and whether it keeps asking. */
export interface ShellStep {
  view: ShellView
  /** The output changed: redraw it (and keep the reader's place). */
  repaint: boolean
  /** Ask again on the next beat. */
  poll: boolean
}

/**
 * One read applied to the panel.
 *
 * - An answer repaints only when its signature moved; redrawing an unchanged
 *   build log every beat would throw away the reader's scroll position every
 *   beat with it. It stops asking once the command has ended.
 * - A 404 is the ordinary end of watching one: the command finished and its
 *   id is no longer served. The last text stays and the panel says finished.
 * - Any other failure is said, keeps the last text, and stops the beat; a
 *   poller hammering a line that is down helps nobody.
 */
export function stepShell(view: ShellView, read: ShellRead): ShellStep {
  if (read.kind === "answer") {
    const reply = read.reply
    const moved = reply.signature !== view.signature || view.loading || !!view.error
    const next: ShellView = {
      meta: { ...view.meta, ...reply.shell, command: reply.shell.command || view.meta.command, what: reply.shell.what || view.meta.what },
      text: reply.text,
      signature: reply.signature,
      ended: reply.ended,
      truncated: !!reply.truncated,
      loading: false,
      error: "",
    }
    return { view: next, repaint: moved || next.ended !== view.ended, poll: !reply.ended }
  }
  if (read.kind === "missing") {
    return { view: { ...view, ended: true, loading: false, error: "" }, repaint: true, poll: false }
  }
  return { view: { ...view, loading: false, error: read.sentence }, repaint: true, poll: false }
}

/**
 * Whether a redraw should leave the reader at the bottom: always the first
 * time, and afterwards only when they were already there. Scrolled up is them
 * reading something further back, and yanking them away is worse than being
 * a screen behind.
 */
export function sticksToBottom(first: boolean, scrollTop: number, clientHeight: number, scrollHeight: number): boolean {
  return first || scrollTop + clientHeight >= scrollHeight - 24
}
