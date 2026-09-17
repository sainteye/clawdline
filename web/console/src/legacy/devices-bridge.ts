// The Devices page's way into its copied module.
//
// `js/view/devices.js` is the Swift app's, byte for byte. It is bound here as
// that app's `main.js` binds it: one table of the page's elements by id, and an
// environment whose answers are the ones the original's local transport gives
// (`net/live.js`).
//
// What that transport gives, and so what this gives:
//
// - `machines` is a constant, not a route. The Mac serving the page is the only
//   machine a local page can reach, and `net/live.js` writes it out by hand:
//   `this-mac`, current, paired as `local`, selectable. The words in it are the
//   catalog's, read when the page asks, after the strings have arrived.
// - `voiceHost` and `setVoiceHost` are absent: the original's `main.js` asks the
//   transport for them and the local transport has neither, so both answer
//   null and no card is marked for voice.
// - `events` is absent. The original reloads the page on every Session and
//   orchestrator frame and redraws only when the cards would say something
//   different; with a constant list they never do, so the reload has nothing to
//   show here, and a second event stream would only take one more of the
//   browser's six connections to this daemon.
// - `start` goes back to the Session list. The original then opens the Start
//   sheet for the machine; this console has no Start sheet yet.
import { T } from "./js/core/i18n.js"
import { LOCAL_SESSION_MACHINE } from "./js/session/selection.js"
import { bindDevicesPage as bindDevicesPageOriginal } from "./js/view/devices.js"

/** What `bindDevicesPage` hands back. */
export interface DevicesPage {
  enter(): void
  leave(): void
  load(quiet?: boolean): Promise<void>
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
 * `machines()` in `net/live.js`: this Mac, and nothing else.
 *
 * `platform` is the original's literal. This daemon also runs where that is
 * not true, and there it should name the system it is on — see the to-do in
 * docs/replica.md.
 */
export function localMachines(): Promise<{ machines: Record<string, unknown>[] }> {
  const copy = T as Record<string, string>
  return Promise.resolve({
    machines: [
      {
        id: LOCAL_SESSION_MACHINE,
        name: copy.webMachineThisMac,
        platform: "macos",
        provider: null,
        kind: "mac",
        label: copy.webMachineMac + " · " + copy.webMachineThisMac,
        observedAt: Date.now(),
        freshness: "current",
        pairing: "local",
        selectable: true,
        autoSelectable: true,
      },
    ],
  })
}

/**
 * Bind the page once its markup is in the document. `start` is what a card's
 * "New session" does with the machine it names.
 */
export function bindDevices(doc: Document, start: (machine: string) => void): DevicesPage {
  const elements: Record<string, HTMLElement | null> = {}
  for (const id of DEVICES_ELEMENT_IDS) elements[id] = doc.getElementById(id)
  return (bindDevicesPageOriginal as (elements: Record<string, HTMLElement | null>, environment: Record<string, unknown>) => DevicesPage)(
    elements,
    {
      machines: localMachines,
      start,
    },
  )
}
