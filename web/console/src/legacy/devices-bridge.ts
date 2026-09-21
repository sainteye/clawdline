// The Devices page's way into its copied module.
//
// `js/view/devices.js` is the Swift app's, byte for byte. It is bound here as
// that app's `main.js` binds it: one table of the page's elements by id, and an
// environment whose answers are the ones the page's transport gives.
//
// **Which transport that is decides what this page is allowed to claim.** The
// heading the copied module paints is the catalog's `webDevicesLede`,
// "Machines connected to this account, their status, and whether this browser
// can read them", and there are two consoles here with two different rights to
// that sentence:
//
// - Served by Clawdline Cloud, the browser holds the account itself. Its
//   client lists the account's machines out of what it has decrypted — the
//   same list the machine picker is chosen from — and `setAccountMachines`
//   installs it. There the sentence is true, and it is the one shown.
// - Served by the daemon on this machine, there is no such list. The local
//   page can reach exactly one machine: the one serving it. `/v1/cloud/status`
//   does carry `devices`, and they are the **viewers** enrolled on the account
//   — browsers and phones — not its machines, so nothing there answers this
//   page's question. The account's own `GET /v1/machines` would (it takes this
//   machine's credential as well as a browser cookie, `requireAccount`), but
//   no local route asks it today, and what it answers is the control plane's
//   rows: names and a last-seen stamp, with no freshness against the relay, no
//   session counts and no "can this browser read it" — three of the four
//   things the sentence promises. So the local page keeps its one card and
//   says *that*, rather than half-answering a question about an account.
//
// The rest of what this environment does and does not give:
//
// - `voiceHost` and `setVoiceHost` are absent: the original's `main.js` asks the
//   transport for them and neither transport here has them, so both answer
//   null and no card is marked for voice.
// - `events` is absent. The original reloads the page on every Session and
//   orchestrator frame and redraws only when the cards would say something
//   different. The local list is a constant and never would; the account list
//   is already reloaded by the gate for the picker, and a second event stream
//   would only take one more of the browser's six connections.
// - `start` goes back to the Session list. The original then opens the Start
//   sheet for the machine; this console has no Start sheet yet.
import type { NextWord } from "../next-strings.js"
import { T } from "./js/core/i18n.js"
import { LOCAL_SESSION_MACHINE } from "./js/session/selection.js"
import { bindDevicesPage as bindDevicesPageOriginal } from "./js/view/devices.js"

/** What `bindDevicesPage` hands back. */
export interface DevicesPage {
  enter(): void
  leave(): void
  load(quiet?: boolean): Promise<void>
}

/** One machine, as the copied module reads one (`deviceViewModel`). */
export type MachineRow = Record<string, unknown>

/** What a machine source answers, as `net/live.js`'s `machines()` answers. */
export interface MachineAnswer {
  machines: MachineRow[]
  syncing?: boolean
  retryAfterMs?: number
}

/** Where this page's machines come from. */
export type MachineSource = () => Promise<MachineAnswer>

/**
 * The two sentences this side of the page needs, read when the page asks.
 *
 * They are passed in rather than imported: `next-strings.ts` is a `.ts` module
 * and this one is loaded as it is by `node --test`, which resolves no `.js`
 * specifier to it. Both are read late, because the catalog lands after the
 * first render (`legacy/bridge.ts` `loadStrings`).
 */
export interface DevicesWords {
  /** `nextWord("devicesThisMachine")`: what to call the machine serving this page. */
  thisMachine(): string
  /** `nextWord(devicesLede())`: the sentence that is true of the list below it. */
  lede(): string
}

/** `main.js`'s element table for `bindDevicesPage`, in its order. */
export const DEVICES_ELEMENT_IDS = [
  "devices",
  "devices-title",
  "devices-lede",
  "devices-close",
  "devices-status",
  "devices-empty",
  "devices-rows",
] as const

/**
 * The account's machines, when something can list them.
 *
 * The Cloud gate installs its connected client's list here before the console
 * is drawn (`cloud/CloudGate.tsx`); nothing installs one on the console the
 * daemon serves. Passing null puts the page back to the machine serving it,
 * which is what a gate that has lost its line leaves behind.
 */
let account: MachineSource | null = null

export function setAccountMachines(source: MachineSource | null): void {
  account = source
}

/** Whether an account's list is installed — that is, whether the lede is true. */
export function accountMachinesInstalled(): boolean {
  return account !== null
}

/** The sentence this page may put under its heading, for the source it has. */
export function devicesLede(): NextWord {
  return account ? "devicesLedeAccount" : "devicesLedeThisMachine"
}

/**
 * What this daemon says it is running on: `darwin`, `linux`, `windows`, or
 * null where this browser could not be told.
 *
 * `/v1/diagnostics` is this machine's own token's, and a browser opened on
 * this machine is exactly who reads this page, so the answer is normally
 * there; `Dashboard.tsx` reads the same route the same way. The path is
 * written out rather than taken from `client`, because this module is loaded
 * as it is by `node --test` and the client is a workspace package; the console
 * is served by the daemon it asks, so the path is the whole address.
 *
 * It is asked once per page and remembered, including the failure — an
 * operating system does not change under a running daemon, and a page that
 * could not be told is not going to be told by asking again.
 */
let platform: Promise<string | null> | null = null

export function localPlatform(read: typeof fetch = fetch): Promise<string | null> {
  if (!platform) {
    platform = (async () => {
      const res = await read("/v1/diagnostics", { credentials: "same-origin" })
      if (!res.ok) return null
      const body = (await res.json()) as { platform?: { os?: unknown } }
      const os = body?.platform?.os
      return typeof os === "string" && os ? os : null
    })().catch(() => null)
  }
  return platform
}

/** Ask again on the next call. For the tests, which have no daemon behind them. */
export function forgetLocalPlatform(): void {
  platform = null
}

/**
 * The kind word this page puts in front of the machine's name.
 *
 * It is `machinePresentation`'s mapping (`js/session/selection.js`) for the
 * one platform a local page can name. That function is not called for it
 * because it treats `this-mac` as proof of macOS — the original's local
 * transport was only ever on a Mac — and this daemon also runs where that is
 * false. The opaque `mac_…` ids the control plane mints are the same story and
 * that module says so in as many words: a namespace, not a platform.
 */
export function localMachineKind(os: string | null): "mac" | "linux" | "unknown" {
  return os === "darwin" ? "mac" : os === "linux" ? "linux" : "unknown"
}

/**
 * `machines()` for the console this daemon serves: the machine serving it, and
 * nothing else.
 *
 * Only a machine that says it is a Mac is called one. Where the daemon answers
 * something else, or will not say, the card carries the machine's own words
 * and no platform, because "Mac · This Mac" on a Linux host is the thing this
 * page was in trouble for.
 */
export async function localMachines(thisMachine: string, read: typeof fetch = fetch): Promise<MachineAnswer> {
  const copy = T as Record<string, string>
  const os = await localPlatform(read)
  const kind = localMachineKind(os)
  const name = kind === "mac" ? copy.webMachineThisMac : thisMachine
  const prefix = kind === "mac" ? copy.webMachineMac : kind === "linux" ? copy.webMachineLinux : ""
  return {
    machines: [
      {
        id: LOCAL_SESSION_MACHINE,
        name,
        platform: os,
        provider: null,
        kind,
        label: prefix ? prefix + " · " + name : name,
        observedAt: Date.now(),
        freshness: "current",
        pairing: "local",
        selectable: true,
        autoSelectable: true,
      },
    ],
  }
}

/**
 * How a machine this browser is not paired with gets paired, when something
 * here can do it.
 *
 * The copied module draws such a card with `webDevicePairHelp` under it — "Start
 * Pair a Browser on this machine" — which names a control the machine may not
 * have (a Linux daemon has a terminal, not a window) and gives the person
 * nothing to press. The Cloud gate installs the one thing that can finish the
 * job: it opens its pairing card for that machine (`cloud/CloudGate.tsx`,
 * `cloud/pair.ts`). The console the daemon serves installs nothing, and never
 * draws an unpaired card either — the only machine it lists is itself.
 */
let pairing: ((machine: string) => void) | null = null

export function setMachinePairing(open: ((machine: string) => void) | null): void {
  pairing = open
}

/** A drawn element, as far as `offerPairing` reads one: the DOM's, or a test's stand-in. */
interface Drawn {
  className: string
  title: string
  textContent: string | null
  type?: string
  dataset: Record<string, string | undefined>
  children: ArrayLike<Drawn>
  ownerDocument: { createElement(tag: string): Drawn } | null
  appendChild(child: Drawn): unknown
  onclick: ((this: unknown, ev: never) => unknown) | null
  setAttribute?(name: string, value: string): void
}

/** The words `offerPairing` puts on a card, read when it draws. */
export interface PairWords {
  /** What the button says. */
  pair(): string
  /** What it says to a screen reader, with the machine's name in it. */
  pairOne(machine: string): string
  /** The sentence that replaces the copied one under the card. */
  help(): string
}

function classes(node: Drawn): string[] {
  return String(node.className || "").split(/\s+/)
}

function childWith(node: Drawn, className: string): Drawn | null {
  for (const child of Array.from(node.children)) if (classes(child).includes(className)) return child
  return null
}

/**
 * Put a Pair button on every card the copied module drew for a machine this
 * browser is not paired with, and say what it does in place of the sentence
 * that sends the person to a control that may not exist. Answers how many
 * cards it changed.
 *
 * It runs after the copied module has drawn — `pages/devices.tsx` watches the
 * list — because `js/view/devices.js` is byte for byte and rebuilds its cards
 * whenever what they say changes. So it is idempotent: a card that already
 * carries the button is left alone. The machine's id is the one the copied
 * card itself carries, in the title of the short id under its name.
 */
export function offerPairing(list: Drawn | null, words: PairWords, open: ((machine: string) => void) | null = pairing): number {
  if (!list || !open) return 0
  let changed = 0
  for (const card of Array.from(list.children)) {
    if (card.dataset.pairing !== "not_paired") continue
    if (childWith(card, "device-pair")) continue
    const heading = childWith(card, "device-card-heading")
    const labelled = heading ? Array.from(heading.children) : []
    const id = labelled.find((node) => node.title)?.title ?? ""
    if (!id || !card.ownerDocument) continue
    const name = labelled[0]?.textContent || id
    const help = childWith(card, "device-help")
    if (help) help.textContent = words.help()
    const button = card.ownerDocument.createElement("button")
    button.type = "button"
    button.className = "device-start device-pair"
    button.textContent = words.pair()
    button.title = words.pairOne(name)
    button.setAttribute?.("aria-label", words.pairOne(name))
    button.onclick = () => open(id)
    card.appendChild(button)
    changed += 1
  }
  return changed
}

/**
 * Bind the page once its markup is in the document. `start` is what a card's
 * "New session" does with the machine it names.
 *
 * `enter` is the copied module's, plus the one sentence it cannot write for
 * itself: `labels()` paints the catalog's lede on every arrival, and only this
 * side knows whether the list under it is an account's or one machine's.
 */
export function bindDevices(
  doc: Document,
  start: (machine: string) => void,
  words: DevicesWords,
): DevicesPage {
  const elements: Record<string, HTMLElement | null> = {}
  for (const id of DEVICES_ELEMENT_IDS) elements[id] = doc.getElementById(id)
  const page = (bindDevicesPageOriginal as (elements: Record<string, HTMLElement | null>, environment: Record<string, unknown>) => DevicesPage)(
    elements,
    {
      machines: () => (account ? account() : localMachines(words.thisMachine())),
      start,
    },
  )
  return {
    enter() {
      page.enter()
      const lede = elements["devices-lede"]
      if (lede) lede.textContent = words.lede()
    },
    leave: () => page.leave(),
    load: (quiet?: boolean) => page.load(quiet),
  }
}
