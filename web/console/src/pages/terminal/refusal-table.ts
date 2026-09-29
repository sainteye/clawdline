import type { TerminalRefusalCode } from "@clawdline/contract"
import type { NextWord } from "../../next-strings.js"

/**
 * Every refusal a terminal route can answer, as the sentence a person reads.
 * A Record over the contract's own union, so a code added to
 * terminals.schema.json fails the type check here until it has words, and
 * refusal-table.test.ts enumerates `TerminalRefusalCodeValues` against it as well.
 * Nothing is imported at run time, so `node --test` loads this file as it is.
 */
export const TERMINAL_REFUSAL_WORDS: Record<TerminalRefusalCode, NextWord> = {
  terminal_forbidden: "terminalRefusalForbidden",
  not_controller: "terminalRefusalNotController",
  terminal_controlled: "terminalRefusalControlled",
  lease_superseded: "terminalRefusalLeaseSuperseded",
  lease_expired: "terminalRefusalLeaseExpired",
  input_gap: "terminalRefusalInputGap",
  input_state_unknown: "terminalRefusalInputStateUnknown",
  input_too_large: "terminalRefusalInputTooLarge",
  terminal_busy: "terminalRefusalBusy",
  terminal_closed: "terminalRefusalClosed",
  terminal_unreachable: "terminalRefusalUnreachable",
  terminal_unsupported: "terminalRefusalUnsupported",
  terminals_full: "terminalRefusalFull",
  terminal_viewers_full: "terminalRefusalViewersFull",
  terminal_access_revoked: "terminalRefusalAccessRevoked",
  terminal_cloud_not_supported: "terminalRefusalCloudNotSupported",
  terminal_socket_path_too_long: "terminalRefusalSocketPathTooLong",
  terminal_invalid: "terminalRefusalInvalid",
}
