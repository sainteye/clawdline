import { useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore, type MouseEvent } from "react"
import { createPortal } from "react-dom"
import type { PageModule } from "./types.js"
import { accountMachinesInstalled, bindDevices, devicesLede, offerForgetting, offerLastSeen, offerPairing, watchAccountMachines, watchMachineActions, type DevicesPage } from "../legacy/devices-bridge.js"
import { AccountMachines } from "./devices/AccountMachines.js"
import { nextWord } from "../next-strings.js"
import { machineSeenWord } from "../cloud/machine-seen.js"
import sectionMarkup from "./devices/section.html?raw"
import { SignedInBlock } from "./devices/SignedInBlock.js"
import { MachineAccess, localAccessSource } from "./devices/MachineAccess.js"
import { CloudTerminalPermission } from "./devices/CloudTerminalPermission.js"
import { useCloudAccount } from "../cloud/account-context.js"
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
  const cloudAccount = useCloudAccount()
  const page = useRef<DevicesPage | null>(null)
  const was = useRef(false)
  const [signedIn, setSignedIn] = useState<HTMLElement | null>(null)
  const [cloudPermission, setCloudPermission] = useState<HTMLElement | null>(null)
  // On the hosted console the account's machines are a React list with a sheet
  // (`devices/AccountMachines.tsx`); the copied cards stay for the daemon's own page.
  const accountList = useSyncExternalStore(watchAccountMachines, accountMachinesInstalled, accountMachinesInstalled)
  const [accountNode, setAccountNode] = useState<HTMLElement | null>(null)

  useLayoutEffect(() => {
    const section = document.getElementById("devices")
    const rows = document.getElementById("devices-rows")
    if (!section || !rows || !accountList) return
    section.setAttribute("data-account-list", "")
    const node = document.createElement("div")
    node.id = "devices-account-machines"
    rows.before(node)
    setAccountNode(node)
    return () => {
      section.removeAttribute("data-account-list")
      node.remove()
      setAccountNode(null)
    }
  }, [accountList])

  useLayoutEffect(() => {
    const shell = document.querySelector("#devices .devices-shell")
    if (!shell || accountMachinesInstalled()) return
    // The machine's own console puts its access section first, as Cloud does.
    const node = document.createElement("div")
    node.id = "devices-signed-in"
    // Under the page's heading and before the machine cards, where Cloud draws it too.
    const rows = document.getElementById("devices-rows")
    if (rows && shell.contains(rows)) rows.before(node)
    else shell.appendChild(node)
    setSignedIn(node)
    return () => node.remove()
  }, [])

  useLayoutEffect(() => {
    if (!cloudAccount) return
    const shell = document.querySelector("#devices .devices-shell")
    if (!shell) return
    const node = document.createElement("div")
    node.id = "devices-cloud-terminal-permission"
    shell.appendChild(node)
    setCloudPermission(node)
    return () => node.remove()
  }, [cloudAccount?.apiOrigin, cloudAccount?.deviceID])

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
      page.current?.leave()
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
      {signedIn && createPortal(<LocalAccess shown={shown} />, signedIn)}
      {cloudPermission && createPortal(<CloudTerminalPermission shown={shown} />, cloudPermission)}
      {accountNode && createPortal(<AccountMachines shown={shown} onStart={() => navigate("sessions")} />, accountNode)}
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

/**
 * The machine's own console's access section (`devices/MachineAccess.tsx`):
 * the same card and groups as Cloud's, read from this machine's routes, with
 * the directly signed-in devices kept in their own block for its permission
 * controls. Cloud status says whether invitations can work at all.
 */
function LocalAccess({ shown }: { shown: boolean }) {
  const [status, setStatus] = useState<{ name: string; cloud: boolean; owner: boolean } | null>(null)
  useEffect(() => {
    if (!shown) return
    let live = true
    const read = (path: string) => fetch(path, { cache: "no-store" })
      .then(async (res) => ({ status: res.status, body: res.ok ? await res.json() as Record<string, unknown> : {} }))
      .catch(() => ({ status: 0, body: {} as Record<string, unknown> }))
    // Only this machine's own key may invite; a browser signed in as a device of its own is
    // refused (403) and is told so instead of meeting the refusal at the button.
    Promise.all([read("/v1/cloud/status"), read("/v1/cloud/browser-invites")]).then(([cloud, invites]) => {
      if (!live) return
      const body = cloud.body
      const name = typeof body.machine_name === "string" ? body.machine_name : ""
      setStatus({ name, cloud: body.enabled === true && typeof body.machine_id === "string" && body.machine_id !== "",
        owner: invites.status !== 403 })
    })
    return () => { live = false }
  }, [shown])
  const source = useMemo(() => status
    ? { ...localAccessSource(status.name, status.cloud && status.owner, <SignedInBlock shown={shown} bare />),
      unavailableWord: status.owner ? "accessCloudOff" as const : "accessNotOwner" as const }
    : null, [status, shown])
  return source ? <MachineAccess source={source} /> : null
}
