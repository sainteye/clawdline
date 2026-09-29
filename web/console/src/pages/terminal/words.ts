import type { PlatformCapability, TerminalHolder, TerminalStatus } from "@clawdline/contract"
import { nextWord, type NextWord } from "../../next-strings.js"
import { TERMINAL_REFUSAL_WORDS } from "./refusal-table.js"

export { TERMINAL_REFUSAL_WORDS }

/** A refusal code as words; anything that is not one is said as it came. */
export function terminalRefusalWords(code: string): string {
  const key = (TERMINAL_REFUSAL_WORDS as Record<string, NextWord | undefined>)[code]
  if (key) return nextWord(key)
  if (code === "network") return nextWord("terminalNetwork")
  return code
}

const STATUS_WORDS: Record<TerminalStatus, NextWord> = {
  running: "terminalStatusRunning",
  exited: "terminalStatusExited",
  closed: "terminalStatusClosed",
  unreachable: "terminalStatusUnreachable",
}

export function terminalStatusWords(status: TerminalStatus): string {
  return nextWord(STATUS_WORDS[status] ?? "terminalStatusUnreachable")
}

/**
 * Who holds the lease, as this tab reads it: itself, another tab of the same
 * browser device, or a device named by its name.
 */
export function holderWords(holder: TerminalHolder | undefined | null): string {
  if (!holder) return nextWord("terminalControlNobody")
  if (holder.same_client) return nextWord("terminalControlYou")
  if (holder.same_device) return nextWord("terminalControlOtherTab")
  return holder.name
}

/**
 * Why the platform capability `terminal` is not there, as words — never an
 * input surface. `null` when terminals are available. The daemon's own
 * `reason` is in whatever language the daemon wrote it, so it is never
 * spliced into this sentence; `unavailableDetail` gives it apart.
 */
export function unavailableWords(cap: PlatformCapability | undefined): string | null {
  if (!cap || cap.state === "available") return null
  if (cap.state === "unknown") return nextWord("terminalUnavailableUnknown")
  switch (cap.code) {
    case "tmux_not_installed":
      return nextWord("terminalUnavailableTmux")
    case "no_backend":
      return nextWord("terminalUnavailableWindows")
    case "tmux_too_old":
    case "tmux_version_unread":
      return nextWord("terminalUnavailableOld")
  }
  return nextWord("terminalUnavailableUnknown")
}

/** The daemon's own words behind `unavailableWords`, shown as it said them, or null. */
export function unavailableDetail(cap: PlatformCapability | undefined): string | null {
  if (!cap || cap.state === "available") return null
  if (cap.state !== "unknown" && (cap.code === "tmux_not_installed" || cap.code === "no_backend")) return null
  const reason = (cap.reason ?? cap.code ?? "").trim()
  return reason ? nextWord("terminalMachineReported", { reason }) : null
}
