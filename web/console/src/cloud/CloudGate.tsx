import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import App, { BRAND_MARK } from "../App.js"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { cardsAreFor } from "../session/send.js"
import {
  chooseTransport,
  cloudOnboardingMode,
  cloudViewerDeviceMetadata,
  keepConnected,
  newCloudSession,
  readCloudConfig,
  type CloudClientHandle,
  type CloudConfig,
  type CloudConnection,
  type CloudMachine,
  type CloudSession,
  type CloudUpdate,
} from "./copied.js"
import { CARRY_TABLE } from "./carry.js"
import { readThroughRelay } from "./install.js"
import { BUILTIN_TAG, bundledCatalog } from "./strings.js"
import { RelayReader } from "./relay-reader.js"
import { RelayWriter, writeRoute } from "./relay-writer.js"
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
 * What the Swift console does at the same points and this does not, yet —
 * pairing a browser, recovering a device slot — is said on the screen where it
 * would have happened rather than left as a dead end. Acting on the machine —
 * sending, answering, starting, ending — goes through the same seam as reading
 * it (`relay-writer.ts`).
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
    return { kind: "misdeclared", reason: "not JSON" }
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
  | { at: "retrying"; code: string; seconds: number }
  | { at: "failed"; code: string }
  | { at: "revoked"; url: string }
  | { at: "machines" }
  | { at: "console" }
  | { at: "blocked"; origin: string }
  | { at: "misdeclared"; reason: string }

/** The machine a tab chose, so a reload reads the same one. Per account, per tab. */
const CHOSEN = "clawdline.cloud.machine:"

/**
 * The receive failures that mean this browser cannot read what arrived, as
 * opposed to a line that dropped: `cloudSessionAccessProblem` in the Swift
 * console's `input/cloud-pairing.js`, the same codes. A dropped socket also
 * reports an error, and saying "cannot be read" for it would be false.
 */
const ACCESS_PROBLEMS = new Set([
  "forbidden", "unauthorized", "revoked", "missing_capability", "capability_denied",
  "unknown_key", "unknown_sender", "extractable_key", "unreadable_envelope",
  "machine_pairing_required",
])

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
  const [machines, setMachines] = useState<CloudMachine[] | null>(null)
  const [syncing, setSyncing] = useState(true)
  const [problem, setProblem] = useState<string | null>(null)
  const [chosen, setChosen] = useState<CloudMachine | null>(null)

  const session = useRef<CloudSession | null>(null)
  const line = useRef<CloudConnection | null>(null)
  const client = useRef<CloudClientHandle | null>(null)
  const reader = useRef<RelayReader | null>(null)
  const unlisten = useRef<(() => void) | null>(null)
  const recheck = useRef<ReturnType<typeof setTimeout> | null>(null)

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
    current.machines().then(
      (answer) => {
        setMachines(answer.machines)
        setSyncing(answer.syncing)
        // Inside the window after connecting, machines are still arriving
        // channel by channel; look again when it closes.
        if (answer.syncing) recheck.current = setTimeout(listMachines, Math.max(1000, answer.retryAfterMs))
      },
      () => {
        setMachines([])
        setSyncing(false)
      },
    )
  }, [])

  const choose = useCallback((machine: CloudMachine) => {
    const current = client.current
    if (!current || reader.current || !machine.selectable) return
    try {
      sessionStorage.setItem(CHOSEN + (current.account ?? ""), machine.id)
    } catch {
      /* a tab that cannot remember asks again after a reload */
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
  }, [transport])

  const onUpdate = useCallback(
    (update: CloudUpdate) => {
      switch (update.state) {
        case "connected": {
          const next = update.client
          client.current = next
          setWho({ account: next.account ?? "", device: next.deviceID ?? "" })
          unlisten.current?.()
          unlisten.current = next.events((event) => {
            // The list is for choosing; once a machine is on screen nobody is looking at it.
            if (!reader.current && (event.type === "orchestrator" || event.type === "sessions")) listMachines()
            // A decryptable inventory lowers it, as it lowers the Swift console's door.
            if (event.type === "sessions") setProblem(null)
            const code = event.type === "error" ? (event as { error?: { code?: unknown } }).error?.code : null
            if (typeof code === "string" && ACCESS_PROBLEMS.has(code)) setProblem(code)
          })
          if (reader.current) {
            // A renewal or a reconnect: the console keeps reading, through the new client.
            reader.current.attach(next)
            setScreen({ at: "console" })
            return
          }
          listMachines()
          setScreen({ at: "machines" })
          return
        }
        case "sign_in":
          setScreen({ at: "sign_in", url: update.url })
          return
        case "pairing_required":
          setScreen({ at: "pairing", account: update.accountID })
          return
        case "device_limit_reached":
          setScreen({ at: "device_limit", tier: update.tier, limit: update.limit })
          return
        case "retrying":
          reader.current?.lost()
          if (!reader.current) {
            setScreen({ at: "retrying", code: update.error?.code ?? "offline", seconds: Math.max(1, Math.ceil(update.afterMs / 1000)) })
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
    [listMachines],
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
  }, [start])

  // A machine this tab chose before, once it is listed again.
  useEffect(() => {
    if (screen.at !== "machines" || !machines || !who) return
    let remembered: string | null = null
    try {
      remembered = sessionStorage.getItem(CHOSEN + who.account)
    } catch {
      remembered = null
    }
    const again = machines.find((m) => m.id === remembered && m.selectable)
    if (again) choose(again)
  }, [screen, machines, who, choose])

  // Which machine this is, in the header beside the connection light, and the
  // way back to the list.
  const aside = chosen && (
    <button
      className="cloud-switch"
      id="cloud-switch"
      type="button"
      title={(chosen.label || chosen.id) + " · " + nextWord("cloudSwitch")}
      onClick={() => {
        try {
          sessionStorage.removeItem(CHOSEN + (who?.account ?? ""))
        } catch {
          /* nothing remembered */
        }
        // Another machine is another page, for the same reason.
        location.reload()
      }}
    >
      {chosen.name || chosen.label || chosen.id}
    </button>
  )
  // Once drawn, the console stays: the copied modules bind to the document
  // once, so a refusal after that (a revoked device, a line that gave up) is
  // drawn over it, as the door is over a local console (`door/Door.tsx`).
  return (
    <>
      {chosen && <App aside={aside} />}
      {words && screen.at !== "console" && (
        <GateCard screen={screen} who={who} machines={machines} syncing={syncing} problem={problem} onChoose={choose} onRetry={start} />
      )}
    </>
  )
}

function GateCard(props: {
  screen: Screen
  who: { account: string; device: string } | null
  machines: CloudMachine[] | null
  syncing: boolean
  problem: string | null
  onChoose: (machine: CloudMachine) => void
  onRetry: () => void
}) {
  const { screen, who, machines, syncing, problem, onChoose, onRetry } = props
  const mark = useRef<HTMLCanvasElement>(null)
  useLayoutEffect(() => {
    L.paintIcon(mark.current, BRAND_MARK, 3)
  }, [])
  const T = L.strings
  return (
    <div className="door" data-step="cloud" data-cloud-screen={screen.at}>
      <div className="door-card" role="dialog" aria-modal="true" aria-label="clawdline">
        <div className="door-head">
          <canvas ref={mark} />
          <b>clawdline</b>
        </div>
        <section data-step="cloud">{body()}</section>
      </div>
    </div>
  )

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
          </>
        )
      case "device_limit":
        return (
          <p className="fine">
            {nextWord("cloudDeviceLimit", { account: who?.account ?? "", limit: screen.limit ?? "?", tier: screen.tier })}
          </p>
        )
      case "retrying":
        return <p className="say calm">{nextWord("cloudRetrying", { code: screen.code, seconds: screen.seconds })}</p>
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
        return (
          <>
            <p className="lede">{nextWord("cloudMachinesLede")}</p>
            {who && <p className="fine">{nextWord("cloudMachinesFine", { account: who.account, device: who.device })}</p>}
            {machines && machines.length > 0 ? (
              <ul className="cloud-machines" id="cloud-machines">
                {machines.map((m) => (
                  <li key={m.id}>
                    <button type="button" data-machine={m.id} disabled={!m.selectable} onClick={() => onChoose(m)}>
                      <span className="cloud-machine-name">{m.label || m.id}</span>
                      <span className="cloud-machine-facts">
                        {nextWord("cloudMachineSessions", { count: m.sessions })}
                        {" · "}
                        {m.pairing === "not_paired"
                          ? T.webDeviceNotPaired
                          : m.freshness === "current"
                            ? T.webDeviceOnline
                            : T.webStartMachineStale}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="say calm">{syncing || machines === null ? nextWord("cloudMachinesWaiting") : nextWord("cloudMachinesNone")}</p>
            )}
            {problem && <p className="say">{nextWord("cloudAccessProblem", { code: problem })}</p>}
          </>
        )
      case "console":
        return null
    }
  }
}
