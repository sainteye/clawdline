/**
 * Deliberate corrections around the byte-locked Swift web copy.
 *
 * The copied files remain the evidence of what the former app shipped. Each
 * entry names the exact source line this console compensates for, why the
 * copied behaviour is false in this console, and the truthful replacement.
 * `refusals/scan.test.ts` keeps every source anchor live: once the locked copy
 * no longer contains it, the test fails and the obsolete correction must go.
 */
export interface LegacyCorrection {
  id:
    | "session-state-unrecognized"
    | "device-session-count-unknown"
    | "session-activity-not-drawn"
    | "forgotten-machine-not-marked"
    | "ledger-reader-busy"
    | "timeline-board-pills-unmapped"
  sources: readonly {
    file: string
    line: number
    contains: string
  }[]
  copiedBehaviour: string
  whyWrong: string
  consoleBehaviour: string
}

export const LEGACY_CORRECTIONS: readonly LegacyCorrection[] = [
  {
    id: "session-state-unrecognized",
    sources: [
      {
        file: "web/console/src/legacy/js/core/i18n.js",
        line: 45,
        contains: 'webStateUnreadable: "screen could not be read"',
      },
      {
        file: "web/console/public/strings/zh-Hant.json",
        line: 744,
        contains: '"webStateUnreadable": "畫面讀不到"',
      },
    ],
    copiedBehaviour: "Says the screen could not be read when a session state is unknown.",
    whyWrong: "The screen bytes were read; ReadState could not identify what the visible screen meant.",
    consoleBehaviour: "Says the state could not be identified and directs the reader into the session to inspect it.",
  },
  {
    id: "device-session-count-unknown",
    sources: [
      {
        file: "web/console/src/legacy/js/view/devices.js",
        line: 46,
        contains: "sessions: Number.isSafeInteger(machine.sessions)",
      },
    ],
    copiedBehaviour: "Draws every supplied safe integer as a session count, and omits a non-integer.",
    whyWrong: "The copied Cloud client supplied 0 when this browser had no key with which to read the machine's sessions.",
    consoleBehaviour: "Supplies no integer without a readable count and adds whether the count is unreadable here or not known yet.",
  },
  {
    id: "session-activity-not-drawn",
    sources: [
      {
        file: "web/console/src/legacy/list.css",
        line: 68,
        contains: ".row .meta {",
      },
    ],
    copiedBehaviour: "Leaves the session row's last activity out of the metadata line.",
    whyWrong: "The daemon supplies that timestamp and the list already orders by it, so hiding it leaves the visible order unexplained.",
    consoleBehaviour: "Shows a known activity timestamp as relative time and draws nothing when the timestamp is absent or unknown.",
  },
  {
    id: "forgotten-machine-not-marked",
    sources: [
      {
        file: "web/console/src/legacy/js/view/devices.js",
        line: 127,
        contains: "[row.connection, row.pairing, row.sessions]",
      },
    ],
    copiedBehaviour: "Drops the New session action from an unselectable machine card without saying this tab has forgotten the machine.",
    whyWrong: "After a successful forget, the retained relay row is deliberately unselectable; silence makes that deliberate state look like a broken control.",
    consoleBehaviour: "Adds a visible forgotten fact to that retained, deliberately unselectable account row.",
  },
  {
    id: "ledger-reader-busy",
    sources: [
      {
        file: "web/console/src/legacy/js/view/ledger.js",
        line: 386,
        contains: 'sentence: code === "graph_not_found" ? T.webLedgerNotFound : ""',
      },
    ],
    copiedBehaviour: "Explains graph_not_found but sends usage_analytics_busy to the generic ledger failure sentence.",
    whyWrong: "A busy reader is available but occupied; the sessions and their records are not broken.",
    consoleBehaviour: "Keeps the usage_analytics_busy code and says the reader is busy and to try again shortly.",
  },
  {
    id: "timeline-board-pills-unmapped",
    sources: [
      {
        file: "web/console/src/legacy/js/view/timeline.js",
        line: 152,
        contains: "function boardPills(parent, entry)",
      },
    ],
    copiedBehaviour: "Draws a clickable pill for each of an entry's boardItemIds and opens the old Project Board on it.",
    whyWrong:
      "The old Board was removed as a frozen store nobody writes, and a boardItemId has no proved mapping to a work id. A pill that opens nothing, or that guesses which work item it meant, would draw an unknown relation as a certainty.",
    consoleBehaviour: "Removes the pills as they are drawn, so an entry shows only what it can stand behind.",
  },
] as const
