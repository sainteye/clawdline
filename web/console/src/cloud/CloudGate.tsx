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
import { afterForget, forgetMachine, honestyIsOurs, type ForgetOutcome } from "./forget.js"
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
  // Forgetting a machine (`forget.ts`): which one is being asked about, which
  // ones this tab has forgotten, and what the account answered about the last
  // one. The list itself is the relay's, not the control plane's, so a
  // forgotten machine stays on it and is marked rather than disappearing —
  // "I revoked it" is visible, as it is on the Mac's own device list
  // (`internal/transport/cloud/link.go`).
  const [asking, setAsking] = useState<CloudMachine | null>(null)
  const [forgetting, setForgetting] = useState(false)
  const [forgotten, setForgotten] = useState<readonly string[]>([])
  const [told, setTold] = useState<{ machine: string; outcome: ForgetOutcome } | null>(null)

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
    // A machine this tab has forgotten still has decrypted snapshots behind it
    // and would open a console reading a line the account has stopped routing.
    if (forgotten.includes(machine.id)) return
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
    // Another machine is another page, for the same reason.
    location.reload()
  }, [who])

  // Which machine this is, in the header beside the connection light, and the
  // way back to the list.
  const aside = chosen && (
    <button
      className="cloud-switch"
      id="cloud-switch"
      type="button"
      title={(chosen.label || chosen.id) + " · " + nextWord("cloudSwitch")}
      onClick={leave}
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
        <GateCard
          screen={screen}
          who={who}
          machines={machines}
          syncing={syncing}
          problem={problem}
          onChoose={choose}
          onRetry={start}
          asking={asking}
          forgetting={forgetting}
          forgotten={forgotten}
          told={told}
          reading={chosen?.id ?? null}
          onAsk={setAsking}
          onForget={forget}
          onLeave={leave}
        />
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
  asking: CloudMachine | null
  forgetting: boolean
  forgotten: readonly string[]
  told: { machine: string; outcome: ForgetOutcome } | null
  reading: string | null
  onAsk: (machine: CloudMachine | null) => void
  onForget: (machine: CloudMachine) => void
  onLeave: () => void
}) {
  const { screen, who, machines, syncing, problem, onChoose, onRetry } = props
  const { asking, forgetting, forgotten, told, reading, onAsk, onForget, onLeave } = props
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
  const T = L.strings
  // The question belongs to the list it was asked from. A line that drops
  // while it is open puts its own screen back, rather than leaving an
  // irreversible button over a card that is now saying something else.
  const question = asking && screen.at === "machines" ? asking : null
  return (
    <div className="door" data-step="cloud" data-cloud-screen={question ? "forget" : screen.at}>
      <div
        className="door-card"
        role={question ? "alertdialog" : "dialog"}
        aria-modal="true"
        aria-label="clawdline"
        aria-busy={forgetting ? "true" : undefined}
        aria-describedby={question ? "cloud-forget-say" : undefined}
      >
        <div className="door-head">
          <canvas ref={mark} />
          <b>clawdline</b>
        </div>
        <section data-step="cloud">{question ? forgetQuestion(question) : body()}</section>
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
                {machines.map((m) => {
                  const gone = forgotten.includes(m.id)
                  return (
                    <li key={m.id} data-forgotten={gone ? "true" : undefined}>
                      <button
                        type="button"
                        data-machine={m.id}
                        disabled={!m.selectable || gone}
                        onClick={() => onChoose(m)}
                      >
                        <span className="cloud-machine-name">{m.label || m.id}</span>
                        <span className="cloud-machine-facts">
                          {nextWord("cloudMachineSessions", { count: m.sessions })}
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
                      {/* A machine this browser was never paired with cannot be
                          chosen and is exactly the kind that has to be
                          removable, so this sits outside the row's own button
                          and does not share its `disabled`. */}
                      {!gone && (
                        <button
                          className="chip danger cloud-forget"
                          type="button"
                          data-forget={m.id}
                          disabled={forgetting}
                          title={nextWord("cloudForgetOne", { machine: m.name || m.label || m.id })}
                          aria-label={nextWord("cloudForgetOne", { machine: m.name || m.label || m.id })}
                          onClick={() => onAsk(m)}
                        >
                          {nextWord("cloudForget")}
                        </button>
                      )}
                    </li>
                  )
                })}
              </ul>
            ) : (
              <p className="say calm">{syncing || machines === null ? nextWord("cloudMachinesWaiting") : nextWord("cloudMachinesNone")}</p>
            )}
            {forgetOutcome()}
            {/* The console behind this card is reading a machine that has just
                been forgotten: choosing another one is a new page, which is
                what the header's own switch does. */}
            {reading && forgotten.includes(reading) && (
              <button className="go" type="button" id="cloud-forget-leave" onClick={onLeave}>
                {nextWord("cloudSwitch")}
              </button>
            )}
            {problem && <p className="say">{nextWord("cloudAccessProblem", { code: problem })}</p>}
          </>
        )
      case "console":
        return null
    }
  }
}
