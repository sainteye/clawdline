import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { nextWord } from "../next-strings.js"
import { readBrowserSettings, writeBrowserSettings } from "../pages/settings/api.js"
import {
  beginCloudPairing,
  cancelCloudPairing,
  readCloudLogin,
  readCloudStatus,
  setCloudCommands,
  setCloudEnabled,
  startCloudLogin,
  type CloudLogin,
  type CloudPairing,
  type CloudStatus,
  type MachineOnly,
} from "../pages/settings/cloud.js"
import { settingsFailureSentence } from "../pages/settings/window/failure.js"
import { QR_QUIET, encodeQr, qrPath } from "../pages/settings/window/qr.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import { localPlatform } from "../legacy/devices-bridge.js"
import { completionChange, parseSkipped, readSetup, setupChip, setupItems, type SetupItem, type SetupReading } from "./progress.js"
import "./setup.css"

/**
 * The setup guide: a button beside the connection light that counts the three
 * pairings setup is finished by (`progress.ts`), and the wizard it opens.
 *
 * It is drawn only by the console served to this machine's own browser — the
 * macOS app's console tab or a browser at 127.0.0.1 — because every step
 * starts something only this machine can start: its Cloud sign-in, its line,
 * and the pairing invitations a browser or phone opens.
 *
 * The wizard opens by itself once, for a machine whose person has not chosen
 * yet (`setup_mode` empty) and that has no Cloud. The app's Help → Setup Guide…
 * sends `OPEN_SETUP_EVENT`, and `?setup` in the address opens it for a test.
 */
export const OPEN_SETUP_EVENT = "clawdline-open-setup"

/** How often the header re-reads this machine's devices while the wizard is closed, and while it is open. */
const IDLE_READ_MS = 60_000
const OPEN_READ_MS = 2_000

type Screen = "welcome" | SetupItem | "done" | "local"

type Settings = { mode: string; completedAt: number; appDevice: string; skipped: SetupItem[] }

const PHONE_SITE = "app.clawdline.com"

/** Whether this page was served to the machine it runs on. */
function servedHere(): boolean {
  const host = location.hostname
  return host === "127.0.0.1" || host === "localhost" || host === "::1" || host === "[::1]"
}

/** The macOS app's console tab can open a pairing link in its own Cloud tab (shell/darwin/main.swift). */
function appBridge(): { postMessage: (body: unknown) => void } | null {
  return window.webkit?.messageHandlers?.shellCloudPair ?? null
}

export function SetupGuide() {
  return servedHere() ? <Guide /> : null
}

function Guide() {
  const [status, setStatus] = useState<CloudStatus | MachineOnly | null | undefined>(undefined)
  const [settings, setSettings] = useState<Settings | null>(null)
  const [screen, setScreen] = useState<Screen | null>(null)
  const [login, setLogin] = useState<CloudLogin | null>(null)
  const [running, setRunning] = useState(false)
  const [said, setSaid] = useState("")
  const [opened, setOpened] = useState(false)
  // The invitation made for the app's own Cloud tab. It is opened there only
  // when the person presses for it, after this page has shown the fingerprint
  // the Cloud tab will ask them to compare.
  const [appPairing, setAppPairing] = useState<CloudPairing | null>(null)
  const autoOpened = useRef(false)
  const asked = useRef({ login: false, line: false, app: "", pairing: "" })
  const inApp = appBridge() !== null
  // Whether this machine runs the macOS app, and so whether the app is one of
  // the items. Until the daemon says, nothing is counted: a reading without the
  // app would stamp a completion that the app's item then takes back.
  const [platform, setPlatform] = useState<string | null | undefined>(undefined)
  useEffect(() => { void localPlatform().then(setPlatform) }, [])
  const hasApp = inApp || platform === "darwin"
  const reading: SetupReading = useMemo(() => platform === undefined && !inApp
    ? { kind: "unknown" } : readSetup(status, hasApp, settings?.appDevice ?? "", settings?.skipped ?? []),
  [status, hasApp, platform, inApp, settings?.appDevice, settings?.skipped])
  const items = reading.kind === "known" ? reading.items : setupItems(hasApp)
  const after = (item: SetupItem | "welcome"): Screen => {
    const rest = item === "welcome" ? items : items.slice(items.indexOf(item) + 1)
    return rest.find((next) => !(reading.kind === "known" && (reading.done[next] || reading.skipped[next]))) ?? "done"
  }
  const before = (item: SetupItem): Screen => items[items.indexOf(item) - 1] ?? "welcome"
  const open = screen !== null

  const refresh = useCallback(async () => {
    const answer = await readCloudStatus()
    setStatus(answer)
    return answer
  }, [])

  const takeSettings = (snapshot: { setup_mode?: string | null; setup_completed_at?: number | null;
    setup_app_device?: string | null; setup_skipped?: string | null }) =>
    setSettings({ mode: snapshot.setup_mode ?? "", completedAt: snapshot.setup_completed_at ?? 0,
      appDevice: snapshot.setup_app_device ?? "", skipped: parseSkipped(snapshot.setup_skipped) })

  const saveSettings = useCallback((change: { setup_mode?: string; setup_completed_at?: number;
    setup_app_device?: string; setup_skipped?: string }) =>
    writeBrowserSettings(change).then(takeSettings, (error: unknown) => setSaid(settingsFailureSentence(error))), [])

  // This machine's devices: once a minute behind the header, every two seconds
  // while somebody is pairing.
  useEffect(() => {
    void refresh()
    const timer = setInterval(() => void refresh(), open ? OPEN_READ_MS : IDLE_READ_MS)
    return () => clearInterval(timer)
  }, [open, refresh])

  useEffect(() => {
    readBrowserSettings().then(takeSettings, () => setSettings(null))
  }, [])

  // The day the button folds away is counted from the first reading with
  // everything paired, and starts again after something is unpaired.
  useEffect(() => {
    if (!settings) return
    const change = completionChange(reading, settings.completedAt, Math.floor(Date.now() / 1000))
    if (change !== null) void saveSettings({ setup_completed_at: change })
  }, [reading, settings, saveSettings])

  const show = useCallback((next: Screen) => {
    setSaid("")
    setOpened(false)
    setScreen(next)
  }, [])

  // The three ways in that are not the header button.
  useEffect(() => {
    const onAsk = () => show("welcome")
    window.addEventListener(OPEN_SETUP_EVENT, onAsk)
    if (new URLSearchParams(location.search).has("setup")) show("welcome")
    return () => window.removeEventListener(OPEN_SETUP_EVENT, onAsk)
  }, [show])

  useEffect(() => {
    if (autoOpened.current || !settings || reading.kind !== "known") return
    autoOpened.current = true
    if (settings.mode === "" && !reading.cloud && reading.count === 0) show("welcome")
  }, [settings, reading, show])

  const firstOpen: Screen = after("welcome")

  const close = () => {
    setRunning(false)
    const pairing = status && typeof status === "object" ? status.pairing : undefined
    if (asked.current.pairing && pairing?.phase === "waiting") void cancelCloudPairing().catch(() => undefined)
    asked.current.pairing = ""
    // Closing without choosing is "not now": the header keeps the count and
    // the wizard does not open by itself again.
    if (settings?.mode === "") void saveSettings({ setup_mode: "cloud" })
    setScreen(null)
  }

  // "Keep it local" from any step: the header then shows the quiet Local
  // mark, and pressing it brings this guide back.
  // Skip one item: setup can finish without it, and the finished screen
  // offers it again. A waiting invitation for it is withdrawn.
  const skip = (item: SetupItem) => {
    setRunning(false)
    const pairing = status && typeof status === "object" ? status.pairing : undefined
    if (asked.current.pairing && pairing?.phase === "waiting") void cancelCloudPairing().catch(() => undefined)
    asked.current.pairing = ""
    const list = [...new Set([...(settings?.skipped ?? []), item])]
    void saveSettings({ setup_skipped: list.join(",") })
    const rest = items.slice(items.indexOf(item) + 1)
    show(rest.find((next) => !(reading.kind === "known" && (reading.done[next] || reading.skipped[next]))) ?? "done")
  }

  const keepLocal = () => {
    setRunning(false)
    const pairing = status && typeof status === "object" ? status.pairing : undefined
    if (asked.current.pairing && pairing?.phase === "waiting") void cancelCloudPairing().catch(() => undefined)
    asked.current.pairing = ""
    void saveSettings({ setup_mode: "local" })
    show("local")
  }

  const fail = (error: unknown) => {
    setRunning(false)
    setSaid(nextWord("setupFailed", { why: settingsFailureSentence(error) }))
  }

  const cloud = status && typeof status === "object" ? status : null
  const configured = !!cloud?.configured
  const lineUp = !!cloud?.enabled && cloud.state === "connected"
  const done = reading.kind === "known" ? reading.done : { app: false, browser: false, phone: false }

  // The app step starts its device sign-in as soon as it is shown, so the
  // button that opens the sign-in page is a real link the press opens; a page
  // opened after an await is a popup the browser, and the app, refuse.
  //
  //
  // The login route says "complete" whenever an identity is on disk; the line
  // loads it only once it is switched on, so until then `configured` is false
  // for a machine that is signed in. Signed in is either. A credential the
  // control plane no longer accepts shows up on the line as `not_signed_in`
  // or `device_not_approved`, and only then is a new sign-in asked to replace it.
  const staleLogin = !!cloud && cloud.state !== "connected" &&
    (cloud.last_error_kind === "not_signed_in" || cloud.last_error_kind === "device_not_approved")
  const signedIn = (configured || login?.phase === "complete") && !staleLogin
  const signIn = useCallback((replace: boolean) => {
    asked.current.login = true
    startCloudLogin(replace).then(setLogin, fail)
  }, [])
  useEffect(() => {
    if (screen !== "app" || configured || asked.current.login || status === undefined) return
    asked.current.login = true
    readCloudLogin().then((answer) => {
      setLogin(answer)
      if (answer.phase !== "complete" && answer.phase !== "waiting") signIn(answer.phase === "failed")
    }, fail)
  }, [screen, configured, status, signIn])

  useEffect(() => {
    if (screen !== "app" || login?.phase !== "waiting") return
    const timer = setInterval(() => void readCloudLogin().then(setLogin, () => undefined), OPEN_READ_MS)
    return () => clearInterval(timer)
  }, [screen, login?.phase])

  // After sign-in, the rest of the app step runs by itself: the line, then
  // the app's own invitation, each asked for once.
  useEffect(() => {
    if (!running || !cloud) return
    if (done.app) { setRunning(false); return }
    // Not signed in and no sign-in waiting: nothing will move by itself, so
    // the button must be pressable again rather than read "Connecting…".
    if (!signedIn && login?.phase !== "waiting") { setRunning(false); return }
    if (signedIn && !cloud.enabled && !asked.current.line) {
      asked.current.line = true
      setCloudEnabled(true).then(() => setCloudCommands(true)).then(() => void refresh(), fail)
      return
    }
    if (!lineUp) return
    if (!inApp) { setRunning(false); return }
    if (asked.current.app) return
    asked.current.app = "asked"
    beginCloudPairing().then((pairing) => {
      if (!pairing.link) throw new Error(pairing.error ?? "no pairing link")
      asked.current.app = pairing.invitation_id ?? "asked"
      setAppPairing(pairing)
    }).catch(fail)
  }, [running, cloud, signedIn, lineUp, inApp, done.app, refresh, login?.phase])

  // The device that answered the app's invitation is the app, whatever name
  // its Cloud tab registered under years ago (`progress.ts` deviceItem).
  useEffect(() => {
    const pairing = cloud?.pairing
    const device = pairing?.viewer_device_id
    if (!settings || pairing?.phase !== "paired" || !device || !asked.current.app) return
    if (pairing.invitation_id !== asked.current.app || settings.appDevice === device) return
    void saveSettings({ setup_app_device: device })
  }, [cloud?.pairing, settings, saveSettings])

  // The browser and phone steps each hold one invitation while they are shown.
  useEffect(() => {
    if (screen !== "browser" && screen !== "phone") return
    if (screen === "browser" && done.browser) return
    if (!lineUp || asked.current.pairing === screen) return
    asked.current.pairing = screen
    beginCloudPairing().then(() => void refresh(), fail)
  }, [screen, lineUp, done, refresh])

  // A step finished elsewhere — the browser tab, the phone — moves the wizard on.
  useEffect(() => {
    if (screen === "browser" && done.browser && opened) {
      asked.current.pairing = ""
      show(after("browser"))
    }
    if (screen === "phone" && done.phone && asked.current.pairing === "phone") {
      asked.current.pairing = ""
      show("done")
    }
  }, [screen, done.browser, done.phone, opened, reading, show])

  const chip = settings ? setupChip(reading, settings.mode, settings.completedAt, Math.floor(Date.now() / 1000)) : { kind: "hidden" as const }

  return (
    <>
      <HeaderButton chip={chip} onOpen={() => show(chip.kind === "progress" && (chip.count > 0 || settings?.mode === "cloud")
        ? chip.next : chip.kind === "done" ? "done" : "welcome")} />
      {screen && (
        <Wizard onClose={close} items={items} current={items.includes(screen as SetupItem) ? screen as SetupItem : null} done={done}
          skipped={reading.kind === "known" ? reading.skipped : { app: false, browser: false, phone: false }}>
          {screen === "welcome" && <Welcome
            onLocal={keepLocal}
            onStart={() => { void saveSettings({ setup_mode: "cloud" }); show(firstOpen) }} />}
          {screen === "local" && <Ending title={nextWord("setupLocalTitle")} body={nextWord("setupLocalBody")} local
            primary={{ label: nextWord("setupLocalOk"), run: close }}
            secondary={{ label: nextWord("setupLocalCloud"), run: () => { void saveSettings({ setup_mode: "cloud" }); show(firstOpen) } }} />}
          {screen === "done" && <Finished items={items} done={done}
            onGo={(item) => { asked.current.pairing = ""; show(item) }} onClose={close} />}
          {screen === "app" && <AppStep login={login} configured={signedIn} lineUp={lineUp} paired={done.app}
            running={running} inApp={inApp} onRun={() => { setSaid(""); setRunning(true) }}
            onOpenCloud={appPairing?.link ? () => appBridge()?.postMessage({ link: appPairing.link }) : null}
            onSignIn={() => { setSaid(""); signIn(login?.phase !== "idle") }}
            onBack={() => show("welcome")} onLocal={keepLocal} onSkip={() => skip("app")} onNext={() => show(after("app"))} />}
          {screen === "browser" && <BrowserStep pairing={cloud?.pairing} opened={opened} paired={done.browser}
            onOpen={() => setOpened(true)} onBack={() => show(before("browser"))} onLocal={keepLocal}
            onSkip={() => skip("browser")} />}
          {screen === "phone" && <PhoneStep pairing={cloud?.pairing}
            onRenew={() => { asked.current.pairing = "phone"; beginCloudPairing().then(() => void refresh(), fail) }}
            onBack={() => { asked.current.pairing = ""; show(before("phone")) }} onLocal={keepLocal} onSkip={() => skip("phone")} />}
          {said && <p className="setup-said" role="alert">{said}</p>}
        </Wizard>
      )}
    </>
  )
}

function HeaderButton({ chip, onOpen }: { chip: ReturnType<typeof setupChip>; onOpen: () => void }) {
  if (chip.kind === "hidden") return null
  if (chip.kind === "local") {
    return (
      <button type="button" className="setup-chip" data-kind="local" title={nextWord("setupChipLocalTip")}
        aria-label={nextWord("setupChipLocalTip")} onClick={onOpen}>
        <LaptopIcon />
        <span>{nextWord("setupChipLocal")}</span>
      </button>
    )
  }
  if (chip.kind === "done") {
    return (
      <button type="button" className="setup-chip" data-kind="done" title={nextWord("setupChipDoneTip")} onClick={onOpen}>
        <CheckIcon />
        <span>{nextWord("setupChipDone")}</span>
      </button>
    )
  }
  return (
    <button type="button" className="setup-chip" data-kind="progress"
      title={nextWord("setupChipProgressTip", { count: chip.count, total: chip.total })} onClick={onOpen}>
      <span className="setup-pips" aria-hidden="true">
        {Array.from({ length: chip.total }, (_, i) => <span key={i} data-on={i < chip.count ? "true" : undefined} />)}
      </span>
      <span>{nextWord("setupChipProgress", { count: chip.count, total: chip.total })}</span>
    </button>
  )
}

const ITEM_WORD = { app: "setupItemApp", browser: "setupItemBrowser", phone: "setupItemPhone" } as const

/**
 * The wizard (exported, with its steps, for `capture.tsx`). Its top names every item setup is made of — the app, a browser,
 * a phone — so the steps inside one item are never read as the whole of it.
 */
export function Wizard({ items, current, done, skipped, onClose, children }: {
  items: readonly SetupItem[]
  current: SetupItem | null
  done: Record<SetupItem, boolean>
  skipped: Record<SetupItem, boolean>
  onClose: () => void
  children: ReactNode
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    showModalWithoutChoice(dialog.current)
  }, [])
  return (
    <dialog ref={dialog} className="setup-dialog" aria-label={nextWord("setupDialog")}
      onCancel={(event) => { event.preventDefault(); onClose() }}>
      <div className="setup-top">
        {current ? (
          <ol className="setup-track">
            {items.map((item) => (
              <li key={item} data-current={item === current ? "true" : undefined} data-done={done[item] ? "true" : undefined}
                data-skipped={skipped[item] && item !== current ? "true" : undefined}
                aria-current={item === current ? "step" : undefined}>
                {done[item] ? <CheckIcon /> : <span className="setup-track-dot" aria-hidden="true" />}
                <span>{nextWord(ITEM_WORD[item])}</span>
              </li>
            ))}
          </ol>
        ) : <span />}
        <button type="button" className="setup-x" aria-label={nextWord("setupClose")} onClick={onClose}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18" /></svg>
        </button>
      </div>
      {children}
    </dialog>
  )
}

export function Welcome({ onLocal, onStart }: { onLocal: () => void; onStart: () => void }) {
  const rows: { icon: ReactNode; title: string; hint: string }[] = [
    { icon: <ListIcon />, title: nextWord("setupWelcomeSessions"), hint: nextWord("setupWelcomeSessionsHint") },
    { icon: <BubbleIcon />, title: nextWord("setupWelcomeReply"), hint: nextWord("setupWelcomeReplyHint") },
    { icon: <PhoneIcon />, title: nextWord("setupWelcomePhone"), hint: nextWord("setupWelcomePhoneHint") },
  ]
  return (
    <div className="setup-body">
      <p className="setup-eyebrow">{nextWord("setupWelcomeEyebrow")}</p>
      <h2>{nextWord("setupWelcomeTitle")}</h2>
      <ul className="setup-features">
        {rows.map((row) => (
          <li key={row.title}>
            <span className="setup-feature-icon">{row.icon}</span>
            <span><b>{row.title}</b><span>{row.hint}</span></span>
          </li>
        ))}
      </ul>
      <div className="setup-actions">
        <button type="button" className="setup-quiet" onClick={onLocal}>{nextWord("setupStayLocal")}</button>
        <button type="button" className="setup-primary" onClick={onStart}>{nextWord("setupStart")}</button>
      </div>
    </div>
  )
}

type Step = { label: string; hint?: ReactNode; state: "done" | "active" | "pending" }

function Steps({ steps }: { steps: Step[] }) {
  return (
    <ol className="setup-steps">
      {steps.map((step) => (
        <li key={step.label} data-state={step.state}>
          <span className="setup-step-mark" aria-hidden="true">{step.state === "done" ? <CheckIcon /> : null}</span>
          <span className="setup-step-label">{step.label}</span>
          {step.state === "active" && step.hint ? <span className="setup-step-hint">{step.hint}</span> : null}
        </li>
      ))}
    </ol>
  )
}

export function AppStep({ login, configured, lineUp, paired, running, inApp, onOpenCloud, onRun, onSignIn, onBack, onLocal, onSkip, onNext }: {
  login: CloudLogin | null
  /** Open the app's invitation in its Cloud tab; null until there is one. */
  onOpenCloud: (() => void) | null
  configured: boolean
  lineUp: boolean
  paired: boolean
  running: boolean
  inApp: boolean
  onRun: () => void
  /** Ask the daemon for a fresh sign-in, for when none is waiting. */
  onSignIn: () => void
  onBack: () => void
  onLocal: () => void
  onSkip: () => void
  onNext: () => void
}) {
  const signing = !configured && login?.phase === "waiting"
  const steps: Step[] = [
    {
      label: nextWord("setupAppSignIn"),
      state: configured ? "done" : signing && running ? "active" : "pending",
      hint: login?.verification_url
        ? <a href={login.verification_url} target="_blank" rel="noopener noreferrer">{nextWord("setupAppSignInHint", { code: login.user_code ?? "" })}</a>
        : null,
    },
    { label: nextWord("setupAppMachine"), state: lineUp ? "done" : configured && running ? "active" : "pending", hint: nextWord("setupAppMachineHint") },
    { label: nextWord("setupAppPair"), state: paired ? "done" : lineUp && running && inApp ? "active" : "pending", hint: nextWord("setupAppPairHint") },
  ]
  const startLink = !configured && login?.phase === "waiting" && login.verification_url && !running ? login.verification_url : ""
  return (
    <div className="setup-body">
      <h2>{nextWord("setupAppTitle")}</h2>
      <p className="setup-lede">{nextWord("setupAppBody")}</p>
      <Steps steps={steps} />
      {paired && <p className="setup-ok">{nextWord("setupAppDone")}</p>}
      {!paired && lineUp && !inApp && <p className="setup-note">{nextWord("setupAppElsewhere")}</p>}
      <div className="setup-actions">
        <span className="setup-actions-quiet">
          <button type="button" className="setup-quiet" onClick={onBack}>{nextWord("setupBack")}</button>
          <button type="button" className="setup-quiet" onClick={onSkip}>{nextWord("setupSkip")}</button>
          <button type="button" className="setup-quiet" onClick={onLocal}>{nextWord("setupStayLocal")}</button>
        </span>
        {paired || (lineUp && !inApp) ? (
          <button type="button" className="setup-primary" onClick={onNext}>{nextWord("setupNext")}</button>
        ) : onOpenCloud ? (
          <button type="button" className="setup-primary" onClick={onOpenCloud}>{nextWord("setupAppOpenCloud")}</button>
        ) : startLink ? (
          <a className="setup-primary" href={startLink} target="_blank" rel="noopener noreferrer" onClick={onRun}>{nextWord("setupAppStart")}</a>
        ) : (
          <button type="button" className="setup-primary" disabled={running}
            onClick={!configured && login?.phase !== "waiting" ? onSignIn : onRun}>
            {running ? nextWord("setupAppBusy") : configured ? nextWord("setupAppContinue") : nextWord("setupAppStart")}
          </button>
        )}
      </div>
    </div>
  )
}

export function BrowserStep({ pairing, opened, paired, onOpen, onBack, onLocal, onSkip }: {
  pairing: CloudPairing | undefined
  opened: boolean
  paired: boolean
  onOpen: () => void
  onBack: () => void
  onLocal: () => void
  onSkip: () => void
}) {
  const link = pairing?.phase === "waiting" ? pairing.link ?? "" : ""
  const waiting = opened && !paired
  return (
    <div className="setup-body">
      <h2>{nextWord("setupBrowserTitle")}</h2>
      <p className="setup-lede">{nextWord("setupBrowserBody")}</p>
      <div className="setup-card">
        <span className="setup-feature-icon" data-quiet="true"><GlobeIcon /></span>
        <span>
          <b>{waiting ? nextWord("setupBrowserWaiting") : nextWord("setupBrowserIdle")}</b>
          <span data-accent={waiting ? "true" : undefined}>{waiting ? nextWord("setupBrowserWaitingHint") : nextWord("setupBrowserIdleHint")}</span>
        </span>
      </div>
      <div className="setup-actions">
        <span className="setup-actions-quiet">
          <button type="button" className="setup-quiet" onClick={onBack}>{nextWord("setupBack")}</button>
          <button type="button" className="setup-quiet" onClick={onSkip}>{nextWord("setupSkip")}</button>
          <button type="button" className="setup-quiet" onClick={onLocal}>{nextWord("setupStayLocal")}</button>
        </span>
        {waiting || !link ? (
          <button type="button" className="setup-primary" disabled>{nextWord("setupBrowserBusy")}</button>
        ) : (
          <a className="setup-primary" href={link} target="_blank" rel="noopener noreferrer" onClick={onOpen}>{nextWord("setupBrowserStart")}</a>
        )}
      </div>
    </div>
  )
}

export function PhoneStep({ pairing, platform: initialPlatform = "apple", onRenew, onBack, onLocal, onSkip }: {
  pairing: CloudPairing | undefined
  /** The tab shown first; the capture page (`capture.tsx`) opens either. */
  platform?: "apple" | "android"
  onRenew: () => void
  onBack: () => void
  onLocal: () => void
  onSkip: () => void
}) {
  const [platform, setPlatform] = useState<"apple" | "android">(initialPlatform)
  const expired = pairing?.phase === "failed" || (pairing?.phase === "waiting" && !!pairing.expires_at && pairing.expires_at * 1000 < Date.now())
  const link = pairing?.phase === "waiting" && !expired ? pairing.link ?? "" : ""
  return (
    <div className="setup-body">
      <h2>{nextWord("setupPhoneTitle")}</h2>
      <div className="setup-phone">
        <div className="setup-qr-box">
          {link ? <Qr link={link} /> : <div className="setup-qr-empty" />}
          {expired ? (
            <span className="setup-qr-state">
              {nextWord("setupPhoneExpired")}
              <button type="button" className="setup-quiet" onClick={onRenew}>{nextWord("setupPhoneRenew")}</button>
            </span>
          ) : (
            <span className="setup-qr-state"><span className="setup-wait-dot" aria-hidden="true" />{nextWord("setupPhoneWaiting")}</span>
          )}
        </div>
        <div className="setup-phone-steps">
          <div className="setup-tabs" role="tablist">
            {(["apple", "android"] as const).map((name) => (
              <button key={name} type="button" role="tab" aria-selected={platform === name}
                className="setup-tab" onClick={() => setPlatform(name)}>
                {nextWord(name === "apple" ? "setupPhoneApple" : "setupPhoneAndroid")}
              </button>
            ))}
          </div>
          <ol className="setup-numbered" role="tabpanel">
            <li><SiteLine word={platform === "apple" ? "setupPhoneAppleOpen" : "setupPhoneAndroidOpen"} /></li>
            <li><span>{nextWord("setupPhoneSignIn")}</span></li>
            <li><span>{nextWord("setupPhoneScan")}</span></li>
          </ol>
        </div>
      </div>
      <div className="setup-actions">
        <span className="setup-actions-quiet">
          <button type="button" className="setup-quiet" onClick={onBack}>{nextWord("setupBack")}</button>
          <button type="button" className="setup-quiet" onClick={onSkip}>{nextWord("setupSkip")}</button>
          <button type="button" className="setup-quiet" onClick={onLocal}>{nextWord("setupStayLocal")}</button>
        </span>
      </div>
    </div>
  )
}

/** The site name inside a phone line, drawn in the accent without splitting a translation. */
function SiteLine({ word }: { word: "setupPhoneAppleOpen" | "setupPhoneAndroidOpen" }) {
  const marker = "\u0000"
  const [before, after] = nextWord(word, { site: marker }).split(marker)
  return <span className="setup-phone-open">{before}<code>{PHONE_SITE}</code>{after}</span>
}

/**
 * Setup is finished once every item is paired or skipped. This screen says
 * which, and takes the person back to any item that is not paired.
 */
export function Finished({ items, done, onGo, onClose }: {
  items: readonly SetupItem[]
  done: Record<SetupItem, boolean>
  onGo: (item: SetupItem) => void
  onClose: () => void
}) {
  const all = items.every((item) => done[item])
  return (
    <div className="setup-body setup-ending">
      <span className="setup-ending-mark"><CheckIcon /></span>
      <h2>{nextWord("setupDoneTitle")}</h2>
      <p className="setup-lede">{nextWord(all ? "setupDoneBody" : "setupDoneSkippedBody")}</p>
      <ul className="setup-summary">
        {items.map((item) => (
          <li key={item} data-done={done[item] ? "true" : undefined}>
            {done[item] ? <CheckIcon /> : <span className="setup-track-dot" aria-hidden="true" />}
            <span>{nextWord(ITEM_WORD[item])}</span>
            {done[item]
              ? <span className="setup-summary-state">{nextWord("setupDonePaired")}</span>
              : <button type="button" className="setup-quiet" onClick={() => onGo(item)}>{nextWord("setupDoneSetUp")}</button>}
          </li>
        ))}
      </ul>
      <div className="setup-ending-actions">
        <button type="button" className="setup-primary" onClick={onClose}>{nextWord("setupDoneClose")}</button>
      </div>
    </div>
  )
}

function Ending({ title, body, local = false, primary, secondary }: {
  title: string
  body: string
  local?: boolean
  primary: { label: string; run: () => void }
  secondary: { label: string; run: () => void }
}) {
  return (
    <div className="setup-body setup-ending">
      <span className="setup-ending-mark" data-local={local ? "true" : undefined}>{local ? <LaptopIcon /> : <CheckIcon />}</span>
      <h2>{title}</h2>
      <p className="setup-lede">{body}</p>
      <div className="setup-ending-actions">
        <button type="button" className="setup-secondary" onClick={secondary.run}>{secondary.label}</button>
        <button type="button" className="setup-primary" onClick={primary.run}>{primary.label}</button>
      </div>
    </div>
  )
}

/** Four screen pixels a module: a phone reads it off a laptop at arm's length. */
function Qr({ link }: { link: string }) {
  const code = useMemo(() => encodeQr(link, "L"), [link])
  const side = code.size + QR_QUIET * 2
  return (
    <svg className="setup-qr" role="img" aria-label={nextWord("setupPhoneQr")} viewBox={`0 0 ${side} ${side}`}
      width={side * 4} height={side * 4} shapeRendering="crispEdges">
      <rect width={side} height={side} fill="#fff" />
      <path d={qrPath(code)} fill="#000" />
    </svg>
  )
}

const stroke = { fill: "none", stroke: "currentColor", strokeWidth: 1.8, strokeLinecap: "round", strokeLinejoin: "round" } as const

function CheckIcon() {
  return <svg width="14" height="14" viewBox="0 0 24 24" {...stroke} strokeWidth={2.4} aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5" /></svg>
}
function LaptopIcon() {
  return <svg width="16" height="16" viewBox="0 0 24 24" {...stroke} aria-hidden="true"><rect x="4" y="5" width="16" height="11" rx="1.5" /><path d="M2.5 19h19" /></svg>
}
function ListIcon() {
  return <svg width="20" height="20" viewBox="0 0 24 24" {...stroke} aria-hidden="true"><path d="M4 6h16M4 12h16M4 18h10" /><circle cx="19" cy="18" r="1.6" fill="currentColor" /></svg>
}
function BubbleIcon() {
  return <svg width="20" height="20" viewBox="0 0 24 24" {...stroke} aria-hidden="true"><path d="M20 12a8 8 0 0 1-11.6 7.1L4 20l1-4.2A8 8 0 1 1 20 12z" /></svg>
}
function PhoneIcon() {
  return <svg width="20" height="20" viewBox="0 0 24 24" {...stroke} aria-hidden="true"><rect x="7" y="3" width="10" height="18" rx="2.5" /><path d="M11 18h2" /></svg>
}
function GlobeIcon() {
  return <svg width="22" height="22" viewBox="0 0 24 24" {...stroke} aria-hidden="true"><circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3c2.5 2.7 3.8 5.7 3.8 9s-1.3 6.3-3.8 9c-2.5-2.7-3.8-5.7-3.8-9S9.5 5.7 12 3z" /></svg>
}
