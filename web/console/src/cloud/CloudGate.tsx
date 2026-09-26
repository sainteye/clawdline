import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import App, { BRAND_MARK } from "../App.js"
import type { ConnectionLight } from "../connection-state.js"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { cardsAreFor } from "../session/send.js"
import {
  chooseTransport,
  cloudOnboardingMode,
  cloudViewerDeviceMetadata,
  decodePairingInvitation,
  keepConnected,
  newCloudSession,
  readCloudConfig,
  type PairingInvitation,
  type CloudClientHandle,
  type CloudConfig,
  type CloudConnection,
  type CloudMachine,
  type CloudSession,
  type CloudUpdate,
} from "./copied.js"
import { CARRY_TABLE } from "./carry.js"
import { afterForget, forgetMachine, honestyIsOurs, type ForgetOutcome } from "./forget.js"
import { NAME_MAX, renameMachine, type RenameOutcome } from "./rename.js"
import { setAccountMachines, setMachineForgetting, setMachinePairing } from "../legacy/devices-bridge.js"
import { setProjectSyncSeam, syncSeamFor, type SyncClient } from "./project-sync.js"
import { machinePresentation } from "../legacy/js/session/selection.js"
import { accountMachineRoster, machineIdentityFacts, sessionsFact, withAccountNames, type AccountName } from "./unpaired-rows.js"
import { machineSeenWord } from "./machine-seen.js"
import { PairingRun, dropInvitation, watchInvitations, type PairStart, type PairState } from "./pair.js"
import {
  browserPendingPairings,
  durablePairViewer,
  durablePairViewerFromInvitation,
  resumePendingPairing,
} from "./pair-pending.js"
import { PairPanel, type PairRequest } from "./PairPanel.js"
import { readThroughRelay } from "./install.js"
import { machinesByCapability } from "./machine-access.js"
import { publishScheduleFleet, type ScheduleMachine } from "./schedule-machines.js"
import { BUILTIN_TAG, bundledCatalog } from "./strings.js"
import { RelayReader } from "./relay-reader.js"
import { RelayWriter, writeRoute } from "./relay-writer.js"
import { installScheduleWebhookManagement } from "./schedule-webhooks.js"
import { installCloudPush, type CloudPushClient } from "./cloud-push.js"
import {
  machineAccessProblem,
  machineListAnswer,
  machineListRefusal,
  retryReason,
  type AccessProblem,
  type MachineListState,
  type RetryReason,
} from "./state.js"
import "./cloud.css"

/**
 * What stands in front of the console when the build says it is Clawdline
 * Cloud's: this browser's account, then the machines on it, then one of them.
 *
 * The order is the Swift console's (`main.js`, the `transportKind === "cloud"`
 * branch), and so is every decision in it, because they are its copied modules
 * making them: `CloudViewerSession` checks the cookie session, registers the
 * device key, reads the capabilities and mints the relay token; `keepConnected`
 * holds the line and says what state it is in. This file only draws those
 * states and, once there is a line, lets a person pick the machine the console
 * then reads (`relay-reader.ts`).
 *
 * Pairing a machine with this browser happens here too, where a machine this
 * browser cannot read is met: on its row, on a Devices card, on the door of a
 * browser that holds no key yet, and from a machine's own pairing link
 * (`pair.ts`, `PairPanel.tsx`). What the Swift console does at the same points
 * and this does not, yet — recovering a device slot — is said on the screen
 * where it would have happened rather than left as a dead end. Acting on the
 * machine — sending, answering, starting, ending — goes through the same seam
 * as reading it (`relay-writer.ts`).
 */

export type Declared =
  | { kind: "cloud"; config: CloudConfig }
  | { kind: "blocked"; config: CloudConfig }
  | { kind: "misdeclared"; reason: string }

/**
 * The build's declaration, checked as the Swift console checks its own
 * (`readCloudConfig`, `chooseTransport`). A declaration that cannot be used is
 * refused on the screen rather than quietly becoming a console for a daemon
 * that is not there.
 */
export function readDeclaration(declared: string): Declared {
  let raw: unknown
  try {
    raw = JSON.parse(declared)
  } catch {
    return { kind: "misdeclared", reason: "not JSON" } // refusal-ok: a build declaration that will not parse carries no code; this names the reason itself
  }
  let config: CloudConfig | null
  try {
    config = readCloudConfig({ __clawdlineCloud: raw })
  } catch (error) {
    return { kind: "misdeclared", reason: error instanceof Error ? error.message : String(error) }
  }
  if (!config) return { kind: "misdeclared", reason: "empty" }
  return chooseTransport({ mock: false, origin: location.origin, config }) === "cloud"
    ? { kind: "cloud", config }
    : { kind: "blocked", config }
}

type Screen =
  | { at: "install" }
  | { at: "checking" }
  | { at: "sign_in"; url: string }
  | { at: "pairing"; account: string }
  | { at: "device_limit"; tier: string; limit: number | null }
  | { at: "retrying"; reason: RetryReason; seconds: number }
  | { at: "failed"; code: string }
  | { at: "revoked"; url: string }
  | { at: "machines" }
  | { at: "console" }
  | { at: "blocked"; origin: string }
  | { at: "misdeclared"; reason: string }

/** The machine a tab chose, so a reload reads the same one. Per account, per tab. */
const CHOSEN = "clawdline.cloud.machine:"

/** The screens on which this browser is signed in with a device key, so a pairing can start. */
const SIGNED_IN = new Set<Screen["at"]>(["machines", "console", "pairing"])

/** The copied presentation of a machine the account names (`selection.js`), so it reads like any other row. */
function present(id: string, name: string, platform: string) {
  const shown = (machinePresentation as (value: unknown, copy: unknown) => { name: string; label: string; kind: string })(
    { id, machineName: name, machinePlatform: platform },
    L.strings,
  )
  return { name: shown.name, label: shown.label, kind: shown.kind }
}

/** A platform word only when this browser or the account actually supplied one. */
function platformWord(platform: ReturnType<typeof machineIdentityFacts>["platform"]): string {
  switch (platform) {
    case "macos":
      return nextWord("cloudMachinePlatformMac")
    case "linux":
      return nextWord("cloudMachinePlatformLinux")
    case "linux_aws":
      return nextWord("cloudMachinePlatformLinuxAWS")
    default:
      return nextWord("cloudMachinePlatformUnknown")
  }
}

/**
 * The build's own catalog for this browser (`cloud/strings.ts`).
 *
 * It used to return `{}` for a browser whose language matched no declared
 * alias, and a declaration may carry no aliases at all — `strings` is optional
 * in `readCloudConfig`. Both are the same silence: `applyStrings` takes no
 * `lang` from an empty object, the document keeps the one it shipped with, and
 * the whole hosted console is English for somebody whose Mac is not. So there
 * is a default now, and it is the daemon's own (`DEFAULT_TAG`).
 */
async function catalog(config: CloudConfig): Promise<Record<string, string>> {
  return bundledCatalog(config, navigator.languages ?? [navigator.language], document.baseURI)
}

export function CloudGate({ declared }: { declared: string }) {
  const transport = useMemo(() => readDeclaration(declared), [declared])
  const [words, setWords] = useState(false)
  // A catalog that arrives late has words for a screen already on show
  // (`legacy/bridge.ts` `loadStrings`), and nothing else here would redraw it.
  const [, redraw] = useState(0)
  const [screen, setScreen] = useState<Screen>(() =>
    transport.kind === "misdeclared"
      ? { at: "misdeclared", reason: transport.reason }
      : transport.kind === "blocked"
        ? { at: "blocked", origin: transport.config.appOrigin }
        : { at: "checking" },
  )
  const [who, setWho] = useState<{ account: string; device: string } | null>(null)
  const [machineList, setMachineList] = useState<MachineListState>({ phase: "loading" })
  const machines = machineList.phase === "ready" ? machineList.machines : null
  const [problem, setProblem] = useState<AccessProblem | null>(null)
  const [chosen, setChosen] = useState<CloudMachine | null>(null)
  const [switcherOpen, setSwitcherOpen] = useState(false)
  const switcherRef = useRef<HTMLDivElement>(null)
  const switchButtonRef = useRef<HTMLButtonElement>(null)
  // Forgetting a machine (`forget.ts`): which one is being asked about, which
  // ones this tab or the account says are forgotten, and what the account
  // answered about the last one. The relay can retain an old snapshot after
  // revocation, so the account roster keeps those rows off the picker.
  const [asking, setAsking] = useState<CloudMachine | null>(null)
  const [forgetting, setForgetting] = useState(false)
  const [forgotten, setForgotten] = useState<readonly string[]>([])
  const [told, setTold] = useState<{ machine: string; outcome: ForgetOutcome } | null>(null)
  // Cancel returns to the list that opened the question. A picker press stays
  // on the picker; a Devices-page press uncovers that page again.
  const forgetReturn = useRef<"machines" | "console">("machines")
  // Renaming one (`rename.ts`): which machine is being renamed, and what the
  // account answered about the last one. The new name is not written into the
  // list here, because this list is not the control plane's — it is what the
  // machines published — so the row keeps saying what its machine last said
  // and the answer says where the new name has arrived instead.
  const [naming, setNaming] = useState<CloudMachine | null>(null)
  const [renaming, setRenaming] = useState(false)
  const [renamed, setRenamed] = useState<{ machine: string; outcome: RenameOutcome } | null>(null)
  // Pairing (`pair.ts`): what was asked, how far it got, and the account's
  // names for the machines this browser cannot name itself.
  const [pairRequest, setPairRequest] = useState<PairRequest | null>(null)
  const [pairState, setPairState] = useState<PairState>({ phase: "idle" })
  const [names, setNames] = useState<ReadonlyMap<string, AccountName>>(new Map())
  const [connectionVersion, setConnectionVersion] = useState(0)
  const pairingStore = useMemo(() => browserPendingPairings(), [])

  /** Apply the account's durable answer, not only the relay's retained snapshots. */
  const readAccountRoster = useCallback((apiOrigin: string) => {
    void accountMachineRoster(apiOrigin).then((roster) => {
      if (!roster) return
      setNames(roster.names)
      setForgotten((was) => [...new Set([...was, ...roster.revoked])])
    })
  }, [])
  // An opened handover is the first cryptographic proof for a browser that has
  // never decrypted this machine. Keep it until the reconnect below has read
  // the machine's retained envelopes and `viewerVerified` can take over.
  const claimedPairings = useRef(new Set<string>())
  const run = useRef<PairingRun | null>(null)
  const recoveringPairing = useRef<Promise<void> | null>(null)
  const invitation = useRef<PairingInvitation | null>(null)
  const namesRef = useRef(names)
  namesRef.current = names

  const session = useRef<CloudSession | null>(null)
  const line = useRef<CloudConnection | null>(null)
  const client = useRef<CloudClientHandle | null>(null)
  const reader = useRef<RelayReader | null>(null)
  const unlisten = useRef<(() => void) | null>(null)
  const recheck = useRef<ReturnType<typeof setTimeout> | null>(null)
  const uninstallScheduleWebhooks = useRef<(() => void) | null>(null)
  const uninstallCloudPush = useRef<(() => void) | null>(null)
  // What this tab has forgotten, where the machine source installed once can
  // read it. The source outlives every render that changes the list.
  const gone = useRef<readonly string[]>([])
  gone.current = forgotten

  // The Devices page's list belongs to this gate's line. A gate that is gone
  // leaves no source behind for a page to read an account through.
  useEffect(() => () => setAccountMachines(null), [])
  useEffect(() => () => setProjectSyncSeam(null), [])
  useEffect(() => () => {
    uninstallScheduleWebhooks.current?.()
    uninstallScheduleWebhooks.current = null
    uninstallCloudPush.current?.()
    uninstallCloudPush.current = null
  }, [])

  useEffect(() => {
    if (transport.kind === "misdeclared") {
      console.error("clawdline: " + transport.reason)
      setWords(true)
      document.documentElement.classList.remove("booting")
      return
    }
    // What the document says it is in has to be what it is showing: a screen
    // reader picks a voice from it, a browser offers to translate against it,
    // and `next-strings.ts` chooses this app's own sentences by it. The
    // catalog sets it when one lands (`applyStrings`); when none does, the
    // words on the screen are the built-in English and this says so, rather
    // than leaving the tag the document was built with.
    let landed = false
    void L.loadStrings(async () => {
      const words = await catalog(transport.config)
      landed = typeof words.lang === "string" && !!words.lang
      return words
    }, () => redraw((n) => n + 1)).finally(() => {
      if (!landed) document.documentElement.lang = BUILTIN_TAG
      setWords(true)
      document.documentElement.classList.remove("booting")
    })
  }, [transport])

  /** The machine list as the client computes it from what it has decrypted. */
  const listMachines = useCallback(() => {
    const current = client.current
    if (!current) return
    if (recheck.current) clearTimeout(recheck.current)
    machinesByCapability(current, claimedPairings.current).then(
      (answer) => {
        setMachineList(machineListAnswer(answer))
        // Inside the window after connecting, machines are still arriving
        // channel by channel; look again when it closes.
        if (answer.syncing) recheck.current = setTimeout(listMachines, Math.max(1000, answer.retryAfterMs))
      },
      (error) => {
        setMachineList(machineListRefusal(error))
      },
    )
  }, [])

  const choose = useCallback((machine: CloudMachine) => {
    const current = client.current
    if (!current || !machine.selectable) return
    // A machine this tab has forgotten still has decrypted snapshots behind it
    // and would open a console reading a line the account has stopped routing.
    if (forgotten.includes(machine.id)) return
    try {
      sessionStorage.setItem(CHOSEN + (current.account ?? ""), machine.id)
    } catch {
      /* a tab that cannot remember asks again after a reload */
    }
    // The console's fetch and event seams are installed once for one machine.
    // Picking the machine already underneath this chooser merely closes it;
    // picking another records the destination first, then remounts those seams
    // in this same tab rather than taking the person through an empty picker.
    if (reader.current) {
      if (reader.current.machine === machine.id) {
        setScreen({ at: "console" })
      } else {
        location.reload()
      }
      return
    }
    const config = transport.kind === "cloud" ? transport.config : null
    const next = new RelayReader(machine.id, {
      strings: () => (config ? catalog(config) : Promise.resolve({})),
      carry: CARRY_TABLE,
    })
    const writer = new RelayWriter(next.writeHost)
    next.carryWrites({ route: writeRoute, answer: (route, method, url, init) => writer.answer(route, method, url, init) })
    next.attach(current)
    reader.current = next
    readThroughRelay(next)
    uninstallScheduleWebhooks.current?.()
    uninstallCloudPush.current?.()
    if (config) {
      // Notifications are the account's, not this machine's: the browser
      // subscribes once with Cloud's key and every machine on the account is
      // handed the subscription (`cloud-push.ts`).
      uninstallCloudPush.current = installCloudPush({
        apiOrigin: config.apiOrigin,
        connected: () => {
          const active = client.current
          if (!active) throw Object.assign(new Error("the cloud connection is not ready"), { code: "offline" })
          return active as unknown as CloudPushClient
        },
      })
      uninstallScheduleWebhooks.current = installScheduleWebhookManagement({
        apiOrigin: config.apiOrigin,
        machineID: machine.id,
        connected: () => {
          const active = client.current
          if (!active) throw Object.assign(new Error("the cloud connection is not ready"), { code: "offline" })
          return active
        },
      })
    }
    // What the seam answered and how, for whoever is looking at this page's
    // behaviour from devtools; nothing reads it back.
    ;(globalThis as { __clawdlineCloudSeam?: RelayReader }).__clawdlineCloudSeam = next
    // Which machine this page is talking to is settled here, and only here, so
    // this is where the cards kept for it come back (F4, `session/persist.ts`).
    // Not before: a session id is a terminal id, and a card kept for one
    // machine's `%1` put back under another's would be words for whatever that
    // one holds.
    cardsAreFor(machine.id)
    setChosen(machine)
    setScreen({ at: "console" })
  }, [transport, forgotten])

  /** The machine this tab remembers choosing, for the account it is signed in to. */
  const remembered = useCallback(() => {
    try {
      return sessionStorage.getItem(CHOSEN + (who?.account ?? ""))
    } catch {
      return null
    }
  }, [who])

  /**
   * Forget `machine`, once the person has said so to its name.
   *
   * This is the only irreversible, outward-facing thing the gate does, and
   * everything it can be told is said rather than reduced to a boolean: what
   * the account answered, including the sentence about what a revoke does not
   * undo, and which of "refused", "no such machine" and "could not be read" it
   * was. Nothing is retried; a second DELETE on an answer nobody could read
   * would be a second irreversible act on a guess.
   */
  const forget = useCallback(
    async (machine: CloudMachine) => {
      if (transport.kind !== "cloud" || forgetting) return
      setForgetting(true)
      setTold(null)
      const outcome = await forgetMachine(transport.config.apiOrigin, machine.id)
      setForgetting(false)
      setAsking(null)
      setTold({ machine: machine.name || machine.label || machine.id, outcome })
      if (outcome.kind !== "forgotten") return
      setForgotten((was) => (was.includes(machine.id) ? was : [...was, machine.id]))
      const next = afterForget({
        forgotten: machine.id,
        remembered: remembered(),
        reading: chosen?.id ?? null,
      })
      if (next.clearRemembered) {
        try {
          sessionStorage.removeItem(CHOSEN + (who?.account ?? ""))
        } catch {
          /* a tab that cannot remember asks again after a reload */
        }
      }
      if (next.backToList) {
        // The console on screen is reading a machine the account no longer
        // routes to. Say so over it, as a dropped line is said over it, and
        // leave the way out where the header's own way out is: another machine
        // is another page (`aside`).
        reader.current?.lost()
        setScreen({ at: "machines" })
      }
    },
    [transport, forgetting, remembered, chosen, who],
  )

  /**
   * Call `machine` something else on this account, once the person has typed
   * it against the machine's own name.
   *
   * Nothing is retried and nothing is assumed: `rename.ts` answers which of
   * "refused", "no such machine" and "could not be read" it was, and this only
   * decides where that is said. It is not destructive, so unlike forgetting it
   * leaves the list and this tab's chosen machine exactly as they were.
   */
  const rename = useCallback(
    async (machine: CloudMachine, name: string) => {
      if (transport.kind !== "cloud" || renaming) return
      setRenaming(true)
      setRenamed(null)
      const outcome = await renameMachine(transport.config.apiOrigin, machine.id, name)
      setRenaming(false)
      setRenamed({ machine: machine.name || machine.label || machine.id, outcome })
      // A name this page would not send, or a bound the account would refuse,
      // never left the browser: the question stays open with what was typed.
      if (outcome.kind === "blank" || outcome.kind === "too_long") return
      setNaming(null)
    },
    [transport, renaming],
  )

  /**
   * Finish a pairing whose waiting card went away. The X25519 private half is
   * in IndexedDB, not in this component; after the handover opens, reconnect
   * so retained envelopes are read with the keys just stored.
   */
  const recoverPairing = useCallback(() => {
    const current = session.current
    const active = run.current?.state.phase
    if (!current || recoveringPairing.current || active === "asking" || active === "waiting") return
    const recovery = resumePendingPairing(current, pairingStore)
      .then((opened) => {
        if (opened) {
          claimedPairings.current.add(opened.machineID)
          setConnectionVersion((version) => version + 1)
        }
      })
      .catch((error: unknown) => {
        // A live untyped interruption stays durable for the next reconnect.
        // Typed terminal answers remove themselves in `pair-pending.ts`.
        console.warn("clawdline: pending browser pairing was not settled", error)
      })
      .finally(() => {
        if (recoveringPairing.current === recovery) recoveringPairing.current = null
      })
    recoveringPairing.current = recovery
  }, [pairingStore])

  /**
   * Start one pairing. It is only ever called from a press — a row, a card,
   * the door's button, a link's confirm — never from a render, because a
   * machine's link accepts one answer and a render can happen twice.
   */
  const pair = useCallback((request: PairRequest, start: PairStart) => {
    run.current?.stop()
    setAsking(null)
    setNaming(null)
    setPairRequest(request)
    const next = new PairingRun(start, (state) => {
      if (run.current === next) setPairState(state)
    })
    run.current = next
    setPairState(next.state)
    void next.begin().then((state) => {
      // A machine's link is good for one answer, whatever the answer was.
      if (request.mode === "invitation") dropInvitation(sessionStorage)
      // The active card used to stop here and require a reload. Treat its
      // authenticated handover as the bootstrap capability immediately, then
      // reconnect so the relay replays the machine's retained snapshots.
      if (state.phase === "paired") {
        claimedPairings.current.add(state.machineID)
        setConnectionVersion((version) => version + 1)
      }
      // Stopping only puts the card away. The offer and its private claim key
      // remain live, so settle them without requiring this UI to stay open.
      if (state.phase === "stopped") recoverPairing()
    })
  }, [recoverPairing])

  /** Show this browser's code for `machine`, or for whichever machine runs it. */
  const pairOffer = useCallback(
    (machine: { id: string; name: string } | null) => {
      const current = session.current
      if (!current) return
      pair({ mode: "offer", machine }, durablePairViewer(current, pairingStore))
    },
    [pair, pairingStore],
  )

  /** Open the guide first; choosing the browser-first path is what mints an offer. */
  const openPairing = useCallback((machine: { id: string; name: string } | null) => {
    run.current?.stop()
    run.current = null
    setAsking(null)
    setNaming(null)
    setPairState({ phase: "idle" })
    setPairRequest({ mode: "offer", machine })
  }, [])

  /** Answer the machine's link this page was opened with, once the person has said so. */
  const pairInvitation = useCallback(() => {
    const current = session.current
    const link = invitation.current
    if (!current || !link) return
    pair({ mode: "invitation", ok: true, code: "" }, durablePairViewerFromInvitation(current, link, pairingStore))
  }, [pair, pairingStore])

  /** Put the card away. A run still waiting stops waiting; see `PairingRun.stop`. */
  const closePairing = useCallback(() => {
    run.current?.stop()
    run.current = null
    if (pairRequest?.mode === "invitation") dropInvitation(sessionStorage)
    invitation.current = null
    setPairRequest(null)
    setPairState({ phase: "idle" })
    recoverPairing()
  }, [pairRequest, recoverPairing])

  /**
   * A machine's pairing link, when this page was opened from one — at boot,
   * in place once a standalone window keeps the link (`same-page-links.ts`),
   * or when iOS brings an already-open Home Screen app back to the front. It
   * is read, taken out of the address, and waits for a press.
   */
  const readInvitation = useCallback((raw: string) => {
    run.current?.stop()
    run.current = null
    setPairState({ phase: "idle" })
    try {
      invitation.current = decodePairingInvitation(raw, Date.now())
      setPairRequest({ mode: "invitation", ok: true, code: "" })
    } catch (error) {
      invitation.current = null
      dropInvitation(sessionStorage)
      const code = error && typeof (error as { code?: unknown }).code === "string" ? (error as { code: string }).code : "bad_invitation"
      setPairRequest({ mode: "invitation", ok: false, code })
    }
  }, [])

  useEffect(() => {
    const stopWatching = watchInvitations(window, readInvitation)
    return () => {
      stopWatching()
      run.current?.stop()
    }
  }, [readInvitation])

  // The Devices page's Pair button opens this card for its machine. A gate
  // that is gone leaves nothing behind for a card to press.
  const machinesRef = useRef(machines)
  machinesRef.current = machines
  useEffect(() => {
    setMachinePairing((id) => {
      const known = machinesRef.current?.find((m) => m.id === id)
      const named = known ? withAccountNames([known], namesRef.current, described, present)[0] : null
      openPairing({ id, name: named ? named.name || named.label || id : id })
    })
    setMachineForgetting((id) => {
      const known = machinesRef.current?.find((m) => m.id === id)
      if (!known || gone.current.includes(id)) return
      const named = withAccountNames([known], namesRef.current, described, present)[0]
      setTold(null)
      forgetReturn.current = "console"
      setAsking(named)
      setScreen({ at: "machines" })
    })
    return () => {
      setMachinePairing(null)
      setMachineForgetting(null)
    }
  }, [openPairing])

  const askForget = useCallback((machine: CloudMachine | null) => {
    if (machine) forgetReturn.current = "machines"
    setAsking(machine)
    if (!machine && forgetReturn.current === "console") {
      forgetReturn.current = "machines"
      setScreen({ at: "console" })
    }
  }, [])

  /** Whether this browser has read `machine`'s own snapshot, and so has its own word for its name. */
  function described(machine: string): boolean {
    return !!client.current?.machineDescriptor?.(machine)
  }

  const onUpdate = useCallback(
    (update: CloudUpdate) => {
      switch (update.state) {
        case "connected": {
          const next = update.client
          client.current = next
          setWho({ account: next.account ?? "", device: next.deviceID ?? "" })
          // The Devices page reads the account's machines through this, and
          // says so under its heading. It is the client at the moment of the
          // call rather than this one, because a renewal replaces it
          // (`legacy/devices-bridge.ts`). A machine this tab has forgotten is
          // still on the list the relay decrypted, and a card offering "New
          // session" on it would be the page saying something the account has
          // stopped being true.
          // Project settings sync reads a source machine while this console
          // shows another (`cloud/project-sync.ts`); the client is read per
          // call because a renewal replaces it.
          setProjectSyncSeam(syncSeamFor(
            () => client.current as unknown as SyncClient | null,
            () => reader.current?.machine ?? null,
          ))
          setAccountMachines(async () => {
            const current = client.current
            if (!current) return { machines: [], syncing: true, retryAfterMs: 1000 }
            const answer = await machinesByCapability(current, claimedPairings.current)
            // The same two corrections the gate's list makes: the account's
            // name where this browser has none of its own, and no session
            // count where it could not read the sessions to count them.
            const named = withAccountNames(answer.machines, namesRef.current, described, present)
            return {
              machines: named.map((m) => {
                const row: Record<string, unknown> = gone.current.includes(m.id)
                  ? { ...m, selectable: false, autoSelectable: false }
                  : { ...m }
                if (typeof sessionsFact(m) === "string") delete row.sessions
                return row
              }),
              syncing: answer.syncing,
              retryAfterMs: answer.retryAfterMs,
            }
          })
          unlisten.current?.()
          unlisten.current = next.events((event) => {
            // The list is for choosing; once a machine is on screen nobody is looking at it.
            if (!reader.current && (event.type === "orchestrator" || event.type === "sessions")) listMachines()
            // A decryptable inventory clears only the problem attributed to
            // that machine. One machine answering is not evidence that a
            // different machine's key problem went away.
            if (event.type === "sessions") {
              const machine = event.machine ?? event.identity?.machine
              setProblem((held) => !held || !machine || held.machine === machine ? null : held)
            }
            const listed = machinesRef.current
            const access = machineAccessProblem(
              event,
              next.viewerEvents,
              listed?.length === 1 ? listed[0]!.id : null,
            )
            if (access) setProblem(access)
          })
          recoverPairing()
          if (reader.current) {
            // A renewal or a reconnect: the console keeps reading, through the new client.
            reader.current.attach(next)
            setScreen({ at: "console" })
            return
          }
          listMachines()
          setScreen({ at: "machines" })
          if (transport.kind === "cloud") {
            readAccountRoster(transport.config.apiOrigin)
          }
          return
        }
        case "sign_in":
          setScreen({ at: "sign_in", url: update.url })
          return
        case "pairing_required":
          setScreen({ at: "pairing", account: update.accountID })
          if (transport.kind === "cloud") {
            readAccountRoster(transport.config.apiOrigin)
          }
          recoverPairing()
          return
        case "device_limit_reached":
          setScreen({ at: "device_limit", tier: update.tier, limit: update.limit })
          return
        case "retrying":
          reader.current?.lost()
          if (!reader.current) {
            setScreen({
              at: "retrying",
              reason: retryReason(update.error, navigator.onLine !== false),
              seconds: Math.max(1, Math.ceil(update.afterMs / 1000)),
            })
          }
          return
        case "terminal_error":
          reader.current?.lost()
          setScreen({ at: "failed", code: update.error?.code ?? update.reason ?? "terminal_error" })
          return
        case "revoked":
          reader.current?.lost()
          setScreen({ at: "revoked", url: session.current?.signInURL() ?? "" })
          return
        case "reconnecting":
        case "paused":
          reader.current?.lost()
          return
      }
    },
    [listMachines, transport, recoverPairing, readAccountRoster],
  )

  const start = useCallback(() => {
    if (transport.kind !== "cloud") return
    // Before any session or device exists: a Safari page on an iPhone would
    // spend a device slot on a key the Home Screen app can never read.
    if (cloudOnboardingMode(window) === "install") {
      setScreen({ at: "install" })
      return
    }
    if (!session.current) {
      const device = cloudViewerDeviceMetadata(window)
      session.current = newCloudSession({ config: transport.config, deviceKind: device.kind, deviceName: device.name })
    }
    line.current?.stop()
    if (!reader.current) setScreen({ at: "checking" })
    line.current = keepConnected(session.current, { onState: onUpdate })
  }, [transport, onUpdate])

  useEffect(() => {
    start()
    return () => {
      line.current?.stop()
      line.current = null
      unlisten.current?.()
      unlisten.current = null
      if (recheck.current) clearTimeout(recheck.current)
    }
  }, [start, connectionVersion])

  // A machine this tab chose before, once it is listed again.
  useEffect(() => {
    if (screen.at !== "machines" || !machines || !who) return
    const id = remembered()
    const again = machines.find((m) => m.id === id && m.selectable)
    if (again) choose(again)
  }, [screen, machines, who, choose, remembered])

  /**
   * Leave the machine this tab chose and start again at the list. This drops
   * only this tab's choice; the machine keeps its place on the account, which
   * is what `forget` above is for.
   */
  const leave = useCallback(() => {
    try {
      sessionStorage.removeItem(CHOSEN + (who?.account ?? ""))
    } catch {
      /* nothing remembered */
    }
    setScreen({ at: "machines" })
  }, [who])

  /** Put away the in-place picker without changing the machine underneath it. */
  const closeMachinePicker = useCallback(() => {
    if (!chosen) return
    try {
      sessionStorage.setItem(CHOSEN + (who?.account ?? ""), chosen.id)
    } catch {
      /* the console still stays on the machine already attached */
    }
    setScreen({ at: "console" })
  }, [chosen, who])

  useEffect(() => {
    if (!switcherOpen) return
    const outside = (event: PointerEvent) => {
      if (!switcherRef.current?.contains(event.target as Node)) setSwitcherOpen(false)
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return
      event.preventDefault()
      event.stopPropagation()
      setSwitcherOpen(false)
      switchButtonRef.current?.focus()
    }
    document.addEventListener("pointerdown", outside)
    document.addEventListener("keydown", escape, true)
    return () => {
      document.removeEventListener("pointerdown", outside)
      document.removeEventListener("keydown", escape, true)
    }
  }, [switcherOpen])

  const shown = useMemo<MachineListState>(
    () => {
      if (machineList.phase !== "ready") return machineList
      const visible = withAccountNames(machineList.machines, names, described, present)
        .filter((machine) => !forgotten.includes(machine.id))
      return visible.length ? { ...machineList, machines: visible } : { phase: "empty_authoritative" }
    },
    // `described` reads the client, which changes only with a new line and
    // then with a new list.
    [machineList, names, forgotten],
  )
  const quickMachines = shown.phase === "ready" ? shown.machines.filter((machine) => machine.selectable) : []

  // The machines a schedule may be on are the ones this switcher offers, in
  // its words (`cloud/schedule-machines.ts`). Published by value, so the
  // schedule page redraws only when one of them changes.
  const scheduleMachines: ScheduleMachine[] = quickMachines.map((machine) => {
    const identity = machineIdentityFacts(machine)
    return {
      id: machine.id,
      name: machine.name || machine.label || machine.id,
      platform: platformWord(identity.platform),
      seenAt: identity.seenAt,
      online: machine.freshness === "current",
    }
  })
  const scheduleFleetKey = chosen ? JSON.stringify([chosen.id, scheduleMachines]) : ""
  useEffect(() => {
    publishScheduleFleet(scheduleFleetKey ? (() => {
      const [current, machines] = JSON.parse(scheduleFleetKey) as [string, ScheduleMachine[]]
      return { current, machines }
    })() : null)
  }, [scheduleFleetKey])

  const switchMachine = (machine: CloudMachine) => {
    setSwitcherOpen(false)
    choose(machine)
  }

  // Which machine this is and whether it answers, in one control: the
  // console's connection light is handed in (`App`'s `aside`) and drawn as the
  // dot before the name, so the phone's one header line keeps its room for the
  // counts. Colour is not the only witness — the state's word is in the title
  // and at the top of the menu, with the retry the light's press used to be.
  // Switching is an in-place header choice; the full machine screen remains
  // one explicit step away for pairing, renaming and forgetting.
  const aside = (light: ConnectionLight) => chosen && (
    <div className="cloud-switcher" ref={switcherRef}>
      <button
        className="cloud-switch"
        id="cloud-switch"
        type="button"
        data-state={light.state}
        // Down, the light's own tip says "press to retry", which this press
        // does not do; the retry is in the menu.
        title={
          (chosen.label || chosen.id) +
          " · " +
          light.label +
          (light.state === "live" ? " — " + light.tip : "") +
          " · " +
          nextWord("cloudSwitch")
        }
        aria-haspopup="dialog"
        aria-expanded={switcherOpen}
        aria-controls="cloud-quick-machines"
        ref={switchButtonRef}
        onClick={() => setSwitcherOpen((open) => !open)}
      >
        <span className="dot" aria-hidden="true" />
        <span className="cloud-switch-name">{chosen.name || chosen.label || chosen.id}</span>
        {/* Drawn, not typed: "⌄" sits at the bottom of its font box, so the
            text glyph hung low beside the name and high once turned over. */}
        <svg className="cloud-switch-chevron" viewBox="0 0 12 12" aria-hidden="true" focusable="false">
          <path d="m3 4.5 3 3 3-3" />
        </svg>
      </button>
      {switcherOpen && (
        <div
          className="cloud-switch-menu"
          id="cloud-quick-machines"
          role="dialog"
          aria-label={nextWord("cloudSwitch")}
        >
          <div className="cloud-switch-conn" data-state={light.state}>
            <span className="dot" aria-hidden="true" />
            <span className="cloud-switch-conn-word">{light.label}</span>
            {light.state === "live" ? (
              <span className="cloud-switch-conn-tip">{light.tip}</span>
            ) : (
              <button type="button" className="cloud-switch-retry" onClick={light.onRetry}>
                {nextWord("cloudConnRetry")}
              </button>
            )}
          </div>
          <p className="cloud-switch-title">{nextWord("cloudMachinesLede")}</p>
          <div className="cloud-switch-options">
            {quickMachines.map((machine) => {
              const current = machine.id === chosen.id
              const identity = machineIdentityFacts(machine)
              return (
                <button
                  type="button"
                  className="cloud-switch-option"
                  data-current={current ? "true" : undefined}
                  data-machine={machine.id}
                  key={machine.id}
                  onClick={() => switchMachine(machine)}
                >
                  <span className="cloud-switch-option-name">{machine.name || machine.label || machine.id}</span>
                  <span className="cloud-switch-option-platform">{platformWord(identity.platform)}</span>
                  {current && <span className="cloud-switch-current">{nextWord("cloudCurrentMachine")}</span>}
                </button>
              )
            })}
          </div>
          <button
            type="button"
            className="cloud-switch-manage"
            onClick={() => {
              setSwitcherOpen(false)
              leave()
            }}
          >
            {nextWord("cloudManageMachines")}
          </button>
        </div>
      )}
    </div>
  )
  // A pairing is drawn over whatever is on screen, the console included, but
  // only once this browser is signed in with a device key: a link opened
  // before signing in waits in this tab's storage for the round trip.
  const pairing =
    pairRequest && SIGNED_IN.has(screen.at)
      ? {
          request: pairRequest,
          state: pairState,
          nameOf: (id: string) => {
            const known = machines?.find((m) => m.id === id)
            if (known) {
              const named = withAccountNames([known], names, described, present)[0]
              return named.name || named.label || id
            }
            // A browser that held no key had no list to find it in; the
            // account still knows what the machine is called.
            const account = names.get(id)
            return account ? present(id, account.name, account.platform).label : id
          },
          onBegin: () => {
            if (pairRequest.mode === "offer") pairOffer(pairRequest.machine)
            else pairInvitation()
          },
          onStop: () => run.current?.stop(),
          onAgain: () => pairOffer(pairRequest.mode === "offer" ? pairRequest.machine : null),
          onClose: closePairing,
          // Pairing already reconnected the line and bootstrapped the row from
          // the authenticated handover. Closing reveals that updated list;
          // the fingerprint stays here until the person has compared it.
          onPaired: closePairing,
        }
      : null
  // Once drawn, the console stays: the copied modules bind to the document
  // once, so a refusal after that (a revoked device, a line that gave up) is
  // drawn over it, as the door is over a local console (`door/Door.tsx`).
  return (
    <>
      {chosen && <App aside={aside} />}
      {words && (screen.at !== "console" || pairing) && (
        <GateCard
          screen={screen}
          who={who}
          machineList={shown}
          problem={problem}
          onChoose={choose}
          onRetry={start}
          asking={asking}
          forgetting={forgetting}
          forgotten={forgotten}
          told={told}
          reading={chosen?.id ?? null}
          onAsk={askForget}
          onForget={forget}
          onLeave={leave}
          onCloseMachinePicker={closeMachinePicker}
          naming={naming}
          renaming={renaming}
          renamed={renamed}
          onName={setNaming}
          onRename={rename}
          pairing={pairing}
          onPair={openPairing}
        />
      )}
    </>
  )
}

function GateCard(props: {
  screen: Screen
  who: { account: string; device: string } | null
  machineList: MachineListState
  problem: AccessProblem | null
  onChoose: (machine: CloudMachine) => void
  onRetry: () => void
  asking: CloudMachine | null
  forgetting: boolean
  forgotten: readonly string[]
  told: { machine: string; outcome: ForgetOutcome } | null
  reading: string | null
  onAsk: (machine: CloudMachine | null) => void
  onForget: (machine: CloudMachine) => void
  onLeave: () => void
  onCloseMachinePicker: () => void
  naming: CloudMachine | null
  renaming: boolean
  renamed: { machine: string; outcome: RenameOutcome } | null
  onName: (machine: CloudMachine | null) => void
  onRename: (machine: CloudMachine, name: string) => void
  pairing: Parameters<typeof PairPanel>[0] | null
  onPair: (machine: { id: string; name: string } | null) => void
}) {
  const { screen, who, machineList, problem, onChoose, onRetry } = props
  const { asking, forgetting, forgotten, told, reading, onAsk, onForget, onLeave, onCloseMachinePicker } = props
  const { naming, renaming, renamed, onName, onRename, pairing, onPair } = props
  const mark = useRef<HTMLCanvasElement>(null)
  const cancel = useRef<HTMLButtonElement>(null)
  const go = useRef<HTMLButtonElement>(null)
  useLayoutEffect(() => {
    L.paintIcon(mark.current, BRAND_MARK, 3)
  }, [])
  // Cancel, not the destructive button, holds the focus the question opens
  // with. `#schedule-delete-confirm` focuses its own Delete, and that sheet is
  // reached from a form somebody opened for one schedule; this question is one
  // press away from a list whose rows are pressed to *use* a machine, so the
  // safe answer is the one under the return key.
  useLayoutEffect(() => {
    if (asking) cancel.current?.focus({ preventScroll: true })
  }, [asking])
  // The same rule for renaming, which is reached from the same rows: the
  // question opens on Cancel, not on the field and not on the button that
  // writes to the account. The field is one Tab away for whoever wants it.
  const nameCancel = useRef<HTMLButtonElement>(null)
  const nameGo = useRef<HTMLButtonElement>(null)
  const [typed, setTyped] = useState("")
  useLayoutEffect(() => {
    if (!naming) return
    setTyped(naming.name || "")
    nameCancel.current?.focus({ preventScroll: true })
  }, [naming])
  const T = L.strings
  // The question belongs to the list it was asked from. A line that drops
  // while it is open puts its own screen back, rather than leaving an
  // irreversible button over a card that is now saying something else.
  const question = !pairing && asking && screen.at === "machines" ? asking : null
  const nameQuestion = !pairing && !question && naming && screen.at === "machines" ? naming : null
  const open = pairing ? "pair" : question ? "forget" : nameQuestion ? "rename" : screen.at
  return (
    <div className="door" data-step="cloud" data-cloud-screen={open}>
      <div
        className="door-card"
        role={question || nameQuestion ? "alertdialog" : "dialog"}
        aria-modal="true"
        aria-label="clawdline"
        aria-busy={forgetting || renaming ? "true" : undefined}
        aria-describedby={question ? "cloud-forget-say" : nameQuestion ? "cloud-rename-say" : undefined}
      >
        <div className="door-head">
          <canvas ref={mark} />
          <b>clawdline</b>
        </div>
        <section data-step="cloud">
          {pairing ? (
            <PairPanel {...pairing} />
          ) : question ? (
            forgetQuestion(question)
          ) : nameQuestion ? (
            renameQuestion(nameQuestion)
          ) : (
            body()
          )}
        </section>
      </div>
    </div>
  )

  /**
   * The question, in place of the list rather than stacked over it — one
   * dialog on screen at a time, the rule `#schedule-delete-confirm` follows
   * over `#schedule-form`, so cancelling leaves the list exactly as it was.
   *
   * It names the machine, says what does not come back, says what forgetting a
   * machine that is reporting in right now does to it, and says the one thing
   * the control plane is careful to say about a revoke: routing stops, the
   * ciphertext somebody already recorded does not. That sentence is here,
   * before the button, because after it is too late to decide anything with.
   */
  function forgetQuestion(machine: CloudMachine) {
    const name = machine.name || machine.label || machine.id
    return (
      <div
        className="cloud-forget-ask"
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            event.preventDefault()
            if (!forgetting) onAsk(null)
            return
          }
          if (event.key !== "Tab") return
          // The two answers are the whole dialog; tab wraps between them
          // (`input/action-confirm.js`'s trap).
          const ends = [cancel.current, go.current].filter(Boolean) as HTMLButtonElement[]
          if (ends.length < 2) return
          const edge = event.shiftKey ? ends[0] : ends[ends.length - 1]
          if (document.activeElement !== edge) return
          event.preventDefault()
          ;(event.shiftKey ? ends[ends.length - 1] : ends[0]).focus({ preventScroll: true })
        }}
      >
        <p className="lede">{nextWord("cloudForgetTitle", { machine: name })}</p>
        <p className="say" id="cloud-forget-say">
          {nextWord("cloudForgetAsk", { machine: name })}
          {"\n"}
          {machine.freshness === "current"
            ? nextWord("cloudForgetAskCurrent")
            : machine.freshness === "stale"
              ? nextWord("cloudForgetAskStale")
              : nextWord("cloudForgetAskUnknown")}
          {"\n"}
          {nextWord("cloudForgetHonest")}
        </p>
        <div className="buttons">
          <button
            className="chip"
            id="cloud-forget-cancel"
            type="button"
            ref={cancel}
            disabled={forgetting}
            onClick={() => onAsk(null)}
          >
            {T.webCancel}
          </button>
          <button
            className="chip confirm-go"
            id="cloud-forget-go"
            type="button"
            ref={go}
            disabled={forgetting}
            onClick={() => onForget(machine)}
          >
            {forgetting ? nextWord("cloudForgetting", { machine: name }) : nextWord("cloudForget")}
          </button>
        </div>
      </div>
    )
  }

  /**
   * The rename question, in place of the list as the forget question is.
   *
   * It names the machine, says who sees the new name, and says the one thing
   * this console knows that the route does not mention: the list on this
   * screen is not the control plane's, so a rename that worked will not show
   * here until that machine reports in again. That is said before the button,
   * because afterwards it reads as an excuse.
   */
  function renameQuestion(machine: CloudMachine) {
    const name = machine.name || machine.label || machine.id
    return (
      <form
        className="cloud-rename-ask"
        onSubmit={(event) => {
          event.preventDefault()
          if (!renaming) onRename(machine, typed)
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            event.preventDefault()
            if (!renaming) onName(null)
          }
        }}
      >
        <p className="lede">{nextWord("cloudRenameTitle", { machine: name })}</p>
        <p className="say" id="cloud-rename-say">
          {nextWord("cloudRenameAsk", { machine: name })}
          {"\n"}
          {nextWord("cloudRenameLater")}
        </p>
        <label htmlFor="cloud-rename-name">{nextWord("cloudRenameField")}</label>
        <input
          id="cloud-rename-name"
          type="text"
          value={typed}
          maxLength={NAME_MAX}
          disabled={renaming}
          autoComplete="off"
          onChange={(event) => setTyped(event.target.value)}
        />
        <div className="buttons">
          <button
            className="chip"
            id="cloud-rename-cancel"
            type="button"
            ref={nameCancel}
            disabled={renaming}
            onClick={() => onName(null)}
          >
            {T.webCancel}
          </button>
          <button
            className="chip"
            id="cloud-rename-go"
            type="submit"
            ref={nameGo}
            disabled={renaming || !typed.trim() || typed.trim() === (machine.name || "")}
          >
            {renaming ? nextWord("cloudRenaming", { machine: name }) : nextWord("cloudRename")}
          </button>
        </div>
      </form>
    )
  }

  /**
   * What the account answered about the last machine this tab tried to rename.
   *
   * `blank` and `too_long` never reached the network and say so by naming the
   * bound rather than the account; the other three are the account's own three
   * different answers, and the last of them says it is not an answer.
   */
  function renameOutcome() {
    if (!renamed) return null
    const machine = renamed.machine
    const outcome = renamed.outcome
    if (outcome.kind === "renamed") {
      return (
        <p className="say" id="cloud-rename-told" data-rename-outcome="renamed">
          {nextWord("cloudRenamed", { machine, name: outcome.name })}
          {"\n"}
          {nextWord("cloudRenameLater")}
        </p>
      )
    }
    if (outcome.kind === "blank") return null
    if (outcome.kind === "too_long") {
      return (
        <p className="say" id="cloud-rename-told" data-rename-outcome="too_long">
          {nextWord("cloudRenameTooLong", { max: outcome.max })}
        </p>
      )
    }
    const word =
      outcome.kind === "refused"
        ? "cloudRenameRefused"
        : outcome.kind === "absent"
          ? "cloudRenameAbsent"
          : "cloudRenameUnreadable"
    return (
      <p className="say" id="cloud-rename-told" data-rename-outcome={outcome.kind}>
        {nextWord(word, { machine, code: outcome.code })}
      </p>
    )
  }

  /**
   * What the account answered about the last machine this tab tried to forget.
   *
   * Four answers, and three of them are different words on purpose: refused,
   * no such machine, and could not be read. The last one is the only one that
   * does not say what happened, and it says that.
   */
  function forgetOutcome() {
    if (!told) return null
    const machine = told.machine
    const outcome = told.outcome
    if (outcome.kind === "forgotten") {
      return (
        <p className="say" id="cloud-forget-told" data-forget-outcome="forgotten">
          {nextWord("cloudForgotten", { machine })}
          {"\n"}
          {/* Our own sentence while the route still answers the two words it
              was written against; the route's own note when it answered
              something else; and, when the answer to a revoke that did happen
              could not be read at all, neither. */}
          {honestyIsOurs(outcome)
            ? nextWord("cloudForgetHonest")
            : outcome.note
              ? nextWord("cloudForgetSaid", { note: outcome.note })
              : nextWord("cloudForgetHonestUnread")}
        </p>
      )
    }
    const word =
      outcome.kind === "refused"
        ? "cloudForgetRefused"
        : outcome.kind === "absent"
          ? "cloudForgetAbsent"
          : "cloudForgetUnreadable"
    return (
      <p className="say" id="cloud-forget-told" data-forget-outcome={outcome.kind}>
        {nextWord(word, { machine, code: outcome.code })}
      </p>
    )
  }

  function body() {
    switch (screen.at) {
      case "checking":
        return <p className="say calm">{nextWord("cloudChecking")}</p>
      case "install":
        return (
          <>
            <p className="lede">{nextWord("cloudInstallLede")}</p>
            <p className="fine">{nextWord("cloudInstallFine")}</p>
          </>
        )
      case "sign_in":
        return (
          <>
            <p className="lede">{nextWord("cloudSignInLede")}</p>
            <p className="fine">{nextWord("cloudSignInFine")}</p>
            <button className="go" type="button" id="cloud-sign-in" onClick={() => location.assign(screen.url)}>
              {T.webPlanSignIn}
            </button>
          </>
        )
      case "revoked":
        return (
          <>
            <p className="fine">{nextWord("cloudRevoked")}</p>
            <button className="go" type="button" onClick={() => location.assign(screen.url)}>
              {T.webPlanSignIn}
            </button>
          </>
        )
      case "pairing":
        return (
          <>
            <p className="lede">{nextWord("cloudPairingLede")}</p>
            <p className="fine">{nextWord("cloudPairingFine", { account: screen.account })}</p>
            <button className="go" type="button" id="cloud-pair-start" onClick={() => onPair(null)}>
              {nextWord("cloudPairStart")}
            </button>
          </>
        )
      case "device_limit":
        return (
          <p className="fine">
            {nextWord("cloudDeviceLimit", { account: who?.account ?? "", limit: screen.limit ?? "?", tier: screen.tier })}
          </p>
        )
      case "retrying":
        return (
          <p className="say calm">
            {screen.reason.kind === "named"
              ? nextWord("cloudRetrying", { code: screen.reason.code, seconds: screen.seconds })
              : screen.reason.kind === "browser_offline"
                ? nextWord("cloudRetryingBrowserOffline", { seconds: screen.seconds })
                : nextWord("cloudRetryingUnknown", { seconds: screen.seconds })}
          </p>
        )
      case "failed":
        return (
          <>
            <p className="say">{nextWord("cloudFailed", { code: screen.code })}</p>
            <button className="go" type="button" onClick={onRetry}>
              {nextWord("cloudRetry")}
            </button>
          </>
        )
      case "blocked":
        return <p className="say">{nextWord("cloudBlocked", { origin: screen.origin, here: location.origin })}</p>
      case "misdeclared":
        return <p className="say">{nextWord("cloudMisdeclared", { reason: screen.reason })}</p>
      case "machines":
        const machines = machineList.phase === "ready" ? machineList.machines : null
        return (
          <>
            <p className="lede">{nextWord("cloudMachinesLede")}</p>
            {who && <p className="fine">{nextWord("cloudMachinesFine", { account: who.account, device: who.device })}</p>}
            {machines ? (
              <ul className="cloud-machines" id="cloud-machines">
                {machines.map((m) => {
                  const gone = forgotten.includes(m.id)
                  const count = sessionsFact(m)
                  const name = m.name || m.label || m.id
                  const identity = machineIdentityFacts(m)
                  const pairable = !gone && m.pairing === "not_paired"
                  return (
                    <li
                      key={m.id}
                      data-forgotten={gone ? "true" : undefined}
                      data-pairable={pairable ? "true" : undefined}
                    >
                      <button
                        type="button"
                        data-machine={m.id}
                        disabled={!m.selectable || gone}
                        onClick={() => onChoose(m)}
                      >
                        <span className="cloud-machine-name">{name}</span>
                        <span className="cloud-machine-identity">
                          {platformWord(identity.platform)}
                          {" · "}
                          {nextWord("cloudMachineID", { id: identity.shortID })}
                        </span>
                        <span className="cloud-machine-facts">
                          {machineSeenWord(identity.seenAt)}
                          {" · "}
                          {typeof count === "object"
                            ? nextWord("cloudMachineSessions", { count: count.count })
                            : count === "unread"
                              ? nextWord("cloudMachineSessionsUnread")
                              : nextWord("cloudMachineSessionsUnknown")}
                          {" · "}
                          {gone
                            ? nextWord("cloudForgottenRow")
                            : m.pairing === "not_paired"
                              ? T.webDeviceNotPaired
                              : m.freshness === "current"
                                ? T.webDeviceOnline
                                : T.webStartMachineStale}
                        </span>
                      </button>
                      {!gone && (
                        /* The row and its controls are siblings: nesting buttons
                           would be invalid, while a disabled unpaired row must
                           leave Pair, Rename and Forget in the focus order. */
                        <div
                          className="cloud-machine-actions"
                          role="group"
                          aria-label={nextWord("cloudMachineActions", { machine: name })}
                        >
                          {pairable && (
                            <button
                              className="cloud-pair"
                              type="button"
                              data-pair={m.id}
                              disabled={forgetting || renaming}
                              title={nextWord("cloudPairOne", { machine: name })}
                              aria-label={nextWord("cloudPairOne", { machine: name })}
                              onClick={() => onPair({ id: m.id, name })}
                            >
                              {nextWord("cloudPair")}
                            </button>
                          )}
                          <button
                            className="cloud-rename"
                            type="button"
                            data-rename={m.id}
                            disabled={forgetting || renaming}
                            title={nextWord("cloudRenameOne", { machine: name })}
                            aria-label={nextWord("cloudRenameOne", { machine: name })}
                            onClick={() => onName(m)}
                          >
                            <span aria-hidden="true">✎</span>
                          </button>
                          <button
                            className="cloud-forget"
                            type="button"
                            data-forget={m.id}
                            disabled={forgetting}
                            title={nextWord("cloudForgetOne", { machine: name })}
                            aria-label={nextWord("cloudForgetOne", { machine: name })}
                            onClick={() => onAsk(m)}
                          >
                            <span aria-hidden="true">×</span>
                          </button>
                        </div>
                      )}
                    </li>
                  )
                })}
              </ul>
            ) : (
              <p className="say calm" data-machine-list-state={machineList.phase}>
                {machineList.phase === "loading"
                  ? nextWord("cloudMachinesWaiting")
                  : machineList.phase === "empty_authoritative"
                    ? nextWord("cloudMachinesNone")
                    : machineList.phase === "refused"
                      ? nextWord(
                          machineList.next === "sign_in"
                            ? "cloudMachinesRefusedSignIn"
                            : machineList.next === "pair"
                              ? "cloudMachinesRefusedPair"
                              : "cloudMachinesRefusedRetry",
                          { code: machineList.code },
                        )
                      : null}
              </p>
            )}
            {forgetOutcome()}
            {renameOutcome()}
            {reading && !forgotten.includes(reading) && (
              <button className="go" type="button" id="cloud-switch-cancel" onClick={onCloseMachinePicker}>
                {T.webCancel}
              </button>
            )}
            {/* The console behind this card is reading a machine that has just
                been forgotten: choosing another one is a new page, which is
                what the header's own switch does. */}
            {reading && forgotten.includes(reading) && (
              <button className="go" type="button" id="cloud-forget-leave" onClick={onLeave}>
                {nextWord("cloudSwitch")}
              </button>
            )}
            {problem && (
              <p className="say">
                {nextWord("cloudAccessProblem", { machine: problem.machine, code: problem.code })}
              </p>
            )}
          </>
        )
      case "console":
        return null
    }
  }
}
