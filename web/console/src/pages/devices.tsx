import { useLayoutEffect, useRef, useState, type MouseEvent } from "react"
import { createPortal } from "react-dom"
import type { PageModule } from "./types.js"
import { accountMachinesInstalled, bindDevices, devicesLede, offerForgetting, offerLastSeen, offerPairing, watchMachineActions, type DevicesPage } from "../legacy/devices-bridge.js"
import { nextWord } from "../next-strings.js"
import { machineSeenWord } from "../cloud/machine-seen.js"
import sectionMarkup from "./devices/section.html?raw"
import { SignedInBlock } from "./devices/SignedInBlock.js"
import "./devices-actions.css"

/**
 * The Devices page: `section#devices` in the Swift app's `index.html`, drawn
 * by its `view/devices.js`.
 *
 * The fragment beside this file is that section's markup between its own tags
 * (index.html lines 898–911), whitespace included, and the copied module fills
 * it: the heading's words, the status line and one card per machine. React
 * owns the section element and nothing inside it.
 *
 * Which machines it draws is the transport's answer, not this file's: the
 * hosted console's gate installs the account's own list before the console is
 * drawn, and the console the daemon serves has only the machine serving it.
 * The sentence under the heading is whichever of those two is true — the
 * copied module paints the catalog's, which describes an account, and only one
 * of the two consoles has an account's list to put under it. See
 * legacy/devices-bridge.ts.
 *
 * What the original's `main.js` and page registry do for this page is done
 * here: bind once, `enter` on arrival, `leave` on departure, the keyboard lands
 * on the heading (`focus: "devices-title"`), and the close button's
 * `data-page-to` goes where it says (`core/pages.js`'s delegate). Escape is
 * App's, which leaves any page but the list, as `input/keys.js` does.
 *
 * "New session" on the card goes back to the Session list. The original then
 * opens the Start sheet for that machine; this console has no Start sheet yet.
 *
 * A card for a machine this browser is not paired with gets a Pair button once
 * the copied module has drawn it (`offerPairing`), because that module draws a
 * sentence pointing at a control the machine may not have and nothing to press.
 * The list is watched rather than decorated once: the module rebuilds its cards
 * whenever what they say changes.
 *
 * Under the cards, on the console the daemon serves, is who is signed in to
 * this machine directly (`SignedInBlock`): the browsers `clawdline open` made
 * and the devices that paired, each with a Revoke. It is React's, drawn into
 * a node of its own at the end of the copied shell, so the copied module never
 * sees it.
 */
function DevicesPageView({ shown }: { shown: boolean }) {
  const page = useRef<DevicesPage | null>(null)
  const was = useRef(false)
  const [signedIn, setSignedIn] = useState<HTMLElement | null>(null)

  useLayoutEffect(() => {
    const shell = document.querySelector("#devices .devices-shell")
    if (!shell || accountMachinesInstalled()) return
    const node = document.createElement("div")
    node.id = "devices-signed-in"
    shell.appendChild(node)
    setSignedIn(node)
    return () => node.remove()
  }, [])

  useLayoutEffect(() => {
    if (!page.current) {
      page.current = bindDevices(document, () => navigate("sessions"), {
        thisMachine: () => nextWord("devicesThisMachine"),
        lede: () => nextWord(devicesLede()),
      })
    }
    const rows = document.getElementById("devices-rows")
    if (!rows) return
    const words = {
      pair: () => nextWord("cloudPair"),
      pairOne: (machine: string) => nextWord("cloudPairOne", { machine }),
      help: () => nextWord("devicesPairHelp"),
    }
    const forgetWords = {
      forget: () => nextWord("cloudForget"),
      forgetOne: (machine: string) => nextWord("cloudForgetOne", { machine }),
    }
    const decorate = () => {
      offerPairing(rows as unknown as Parameters<typeof offerPairing>[0], words)
      offerForgetting(rows as unknown as Parameters<typeof offerForgetting>[0], forgetWords)
      offerLastSeen(rows as unknown as Parameters<typeof offerLastSeen>[0], { seen: machineSeenWord })
    }
    const watch = new MutationObserver(decorate)
    watch.observe(rows, { childList: true })
    const stopActions = watchMachineActions(decorate)
    decorate()
    return () => {
      watch.disconnect()
      stopActions()
    }
  }, [])

  useLayoutEffect(() => {
    if (shown === was.current) return
    was.current = shown
    if (!shown) {
      page.current?.leave()
      return
    }
    // The words arrive in App's effect, which runs after this one on a cold
    // start at `#page=devices`; the page is still covered (`booting`) until
    // then, and `enter` paints the heading from them, so arrival waits.
    const arrive = () => {
      if (!was.current) return
      page.current?.enter()
      document.getElementById("devices-title")?.focus({ preventScroll: true })
    }
    const root = document.documentElement
    if (!root.classList.contains("booting")) {
      arrive()
      return
    }
    const watch = new MutationObserver(() => {
      if (root.classList.contains("booting")) return
      watch.disconnect()
      arrive()
    })
    watch.observe(root, { attributes: true, attributeFilter: ["class"] })
    return () => watch.disconnect()
  }, [shown])

  return (
    <>
      <section
        className="page devices"
        id="devices"
        data-page-view="devices"
        aria-labelledby="devices-title"
        hidden={!shown}
        onClick={followPageLink}
        dangerouslySetInnerHTML={{ __html: sectionMarkup }}
      />
      {signedIn && createPortal(<SignedInBlock shown={shown} />, signedIn)}
    </>
  )
}

/** The page router, as the drawer moves: its row for that page, clicked. */
function navigate(name: string): void {
  const row = document.querySelector<HTMLButtonElement>(`#sidebar [data-page-to="${name}"]`)
  if (row && !row.disabled) row.click()
}

/** `core/pages.js`'s delegate for the close button, which names its page in `data-page-to`. */
function followPageLink(ev: MouseEvent<HTMLElement>): void {
  const node = (ev.target as Element).closest?.("[data-page-to]")
  if (!node || !ev.currentTarget.contains(node)) return
  const name = node.getAttribute("data-page-to")
  const row = document.querySelector<HTMLButtonElement>(`#sidebar [data-page-to="${name}"]`)
  if (!row || row.disabled) return
  ev.preventDefault()
  row.click()
}

export const page: PageModule = { id: "devices", Component: DevicesPageView }
