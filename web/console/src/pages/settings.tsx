import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react"
import type { PageModule } from "./types.js"
import * as L from "../legacy/bridge.js"
import {
  revealSettingsDiagnostics,
  setSettingsAssistantIcons,
  setSettingsNewestFirst,
  settingsAssistantIcons,
  settingsBuildVersion,
  settingsMacVersion,
  settingsNewestFirst,
} from "../legacy/settings-bridge.js"
import { pushShape, sendPushTest, startPush, subscribePush, togglePush } from "../push/push.js"
import { legacyState } from "../legacy/overlay-bridge.js"
import { ShellBlocks } from "./settings/ShellBlocks.js"
import { BoardBlock } from "./settings/BoardBlock.js"

/**
 * The settings page, `section#settings` in `index.html` and `input/settings.js`.
 *
 * Same elements, ids, classes and words as there, so `sheets.css` and
 * `pages.css` style it as they style the original. What each block shows is
 * what the original shows against this daemon, which differs from the Swift
 * app's in what it owns:
 *
 * - **Notifications**: `Settings.drawNotify` and `Settings.test`, drawn from
 *   the state `push/push.ts` worked out. Four states and only one of them is
 *   "on", so the block says which; the test button appears only where there is
 *   something subscribed for it to reach.
 * - **Assistant icons** and **Transcript** are this browser's own, as there.
 * - **Project Board**: `BoardBlock` (settings/BoardBlock.tsx) is
 *   `BoardControls`: drawn from the last board answer that says whether the
 *   board is on, and its two toggles send `set_enabled` and `set_ai_consent`.
 * - **Project Timeline**: `view/timeline.js` paints its three words when it is
 *   bound and draws its toggle only from a timeline it has read; with no
 *   project entered it has read none, so the toggle stays "Loading…" there too.
 * - **Version**: the Mac's version from `/v1/health`, which this daemon's does
 *   not carry, so the line is empty and hidden by `.foot span:empty`.
 *
 * Inside the native shell two more blocks come first — see ShellBlocks.
 */

/** `Pages.go` through the drawer's own row, so a move from here is the move the drawer makes. */
function goToPage(name: string): void {
  const row = document.querySelector<HTMLButtonElement>(`#sidebar [data-page-to="${name}"]`)
  if (row && !row.disabled) row.click()
}

/** `Settings.drawNotify`'s sentence: one per state, and "" for a state with none. */
function notifySay(T: Record<string, string>, state: string): string {
  switch (state) {
    case "homescreen":
      return T.webNotifyHomeScreen
    case "unsupported":
      return T.webNotifyUnsupported
    case "blocked":
      return T.webNotifyBlocked
    case "on":
      return T.webNotifyOn
    case "off":
      return T.webNotifySheetOff
    default:
      return ""
  }
}

/** `words(en, zh)` (`view/timeline.js`): the page's language decides. */
function timelineWords(en: string, zh: string): string {
  return /^zh(?:-|$)/i.test(String(document.documentElement.lang || "")) ? zh : en
}

function SettingsPage({ shown }: { shown: boolean }) {
  const T = L.strings
  // `enter` has not run until the page has been shown once; before that the
  // blocks it draws are as the markup has them.
  const [entered, setEntered] = useState(false)
  const [icons, setIcons] = useState(settingsAssistantIcons)
  const [newest, setNewest] = useState(settingsNewestFirst)
  const [version, setVersion] = useState("")
  const closeRef = useRef<HTMLButtonElement>(null)
  // `Push.redraw()` from `Settings.enter`: the notifications block is drawn
  // from the one state the module worked out, wherever it changed.
  const push = useSyncExternalStore(subscribePush, pushShape)
  const presses = useRef(0)
  const idle = useRef<ReturnType<typeof setTimeout> | null>(null)

  // `Settings.enter`, on every arrival however it happened.
  useEffect(() => {
    if (!shown) return
    setEntered(true)
    // `Push.start()` is the page's, not boot's, for the same reason the
    // original's is: registering a worker is a thing this browser is asked to
    // do, and nothing should ask before a reader has opened the place where
    // the answer is shown. The footer calls it too, and the second call is a
    // no-op.
    startPush()
    setIcons(settingsAssistantIcons())
    setNewest(settingsNewestFirst())
    const v = settingsBuildVersion(
      settingsMacVersion(),
      (window as { __clawdlineCloud?: { build?: string } }).__clawdlineCloud,
    )
    setVersion(v ? L.fillString(T.webSettingsVersion, { v }) : "")
  }, [shown])

  // `Pages.go` lands the keyboard on the page's own control, `settings-close`.
  // A page the address asked for on arrival is reached while the document is
  // still hidden for its words, and nothing hidden takes focus; then the
  // landing waits for the words, as the shell's does for the wordmark.
  useLayoutEffect(() => {
    if (!shown) return
    const land = () => closeRef.current?.focus({ preventScroll: true })
    const root = document.documentElement
    if (!root.classList.contains("booting")) {
      land()
      return
    }
    const watch = new MutationObserver(() => {
      if (root.classList.contains("booting")) return
      watch.disconnect()
      land()
    })
    watch.observe(root, { attributes: true, attributeFilter: ["class"] })
    return () => watch.disconnect()
  }, [shown])

  useEffect(
    () => () => {
      if (idle.current) clearTimeout(idle.current)
    },
    [],
  )

  // The door to the recorder: five presses on the version line, forgotten
  // after two seconds without one.
  const pressVersion = () => {
    presses.current += 1
    if (idle.current) clearTimeout(idle.current)
    idle.current = setTimeout(() => {
      presses.current = 0
    }, 2000)
    if (presses.current < 5) return
    presses.current = 0
    try {
      revealSettingsDiagnostics()
    } catch {
      /* the recorder is a door for diagnosis; a panel that will not open is not the page's failure */
    }
  }

  /**
   * `Settings.test`: the session on screen, and **failing that the one the list
   * is pointing at** — because on a phone the first of those is never true when
   * this button can be pressed. `.pane-detail` is `position: fixed; inset: 0;
   * z-index: 40` there, so an open transcript covers the header this page is
   * reached through: `S.openId` and "the reader can see this control" are
   * mutually exclusive on the one device the whole lever exists for.
   *
   * `S.selectedId` survives `closeDetail`, is set by opening a session and by
   * the first list highlighting its top row, and the list clears it the moment
   * that session leaves. So it names something live or it names nothing, and
   * the daemon checks it again before promising an address. `openId` still wins
   * when both are set: on a desktop the pane and the header are on screen
   * together, and there the transcript in front of the reader is the better
   * answer.
   */
  const pressTest = () => {
    sendPushTest(legacyState.openId || legacyState.selectedId || null)
  }

  const toggleIcons = () => {
    const on = !settingsAssistantIcons()
    setSettingsAssistantIcons(on)
    setIcons(on)
  }

  // `toggleOrder` (`input/keys.js`): the state, this button, and the
  // transcript back to its top.
  const toggleOrder = () => {
    const on = !settingsNewestFirst()
    setSettingsNewestFirst(on)
    setNewest(on)
    const tx = document.getElementById("tx-scroll")
    if (tx) tx.scrollTop = 0
  }

  return (
    <section
      className="page page-settings"
      id="settings"
      data-page-view="settings"
      aria-labelledby="settings-title"
      hidden={!shown}
    >
      <div className="sheet" id="settings-sheet">
        <h2 id="settings-title">{T.webSettings}</h2>

        <ShellBlocks shown={shown} />

        <div className="block">
          <b id="settings-notify-title">{T.webSettingsNotify}</b>
          <p className="say" id="settings-notify-say">
            {notifySay(T, push.state)}
          </p>
          <div className="row">
            <button
              className={push.state === "on" ? "chip on" : "chip"}
              id="settings-notify-go"
              type="button"
              hidden={!(push.state === "off" || push.state === "on")}
              disabled={push.busy}
              onClick={togglePush}
            >
              {push.state === "on"
                ? push.busy
                  ? T.webNotifyStopping
                  : T.webNotifyStop
                : push.busy
                  ? T.webNotifyAsking
                  : T.webNotifyGo}
            </button>
            {/* Only offered where there is something subscribed for it to reach. */}
            <button
              className="chip"
              id="settings-notify-test"
              type="button"
              hidden={push.state !== "on"}
              disabled={push.testing}
              onClick={pressTest}
            >
              {push.testing ? T.webSending : T.webNotifyTest}
            </button>
          </div>
          <p
            className={push.saidCalm ? "said calm" : "said"}
            id="settings-notify-said"
            role="status"
            aria-live="polite"
          >
            {push.said}
          </p>
        </div>

        <div className="block">
          <b id="settings-assistant-icons-title">{T.webSettingsAssistantIcons}</b>
          <p
            className="say"
            id="settings-assistant-icons-say"
            dangerouslySetInnerHTML={{ __html: L.wordsHTML(T.webSettingsAssistantIconsSay) }}
          />
          <div className="row">
            <button
              className={entered && icons ? "chip assistant-icon-setting on" : "chip assistant-icon-setting"}
              id="settings-assistant-icons"
              type="button"
              aria-pressed={entered ? (icons ? "true" : "false") : "true"}
              onClick={toggleIcons}
            >
              <span
                className="marks"
                id="settings-assistant-icons-marks"
                aria-hidden="true"
                dangerouslySetInnerHTML={{
                  __html: entered ? L.assistantLogoHTML("claude") + L.assistantLogoHTML("codex") : "",
                }}
              />
              <span id="settings-assistant-icons-label">{T.webSettingsAssistantIconsShow}</span>
            </button>
          </div>
        </div>

        <div className="block">
          <b id="settings-order-title">{T.webSettingsOrder}</b>
          <p className="say" id="settings-order-say" dangerouslySetInnerHTML={{ __html: L.wordsHTML(T.webSettingsOrderSay) }} />
          <div className="row">
            <button
              className={entered && newest ? "chip on" : "chip"}
              id="settings-order"
              type="button"
              title={T.webOrderTip}
              onClick={toggleOrder}
            >
              <svg className="ico arrow" viewBox="0 0 14 14" aria-hidden="true" focusable="false">
                <path
                  d="M7 2.4v9.2M3.6 8.2 7 11.6l3.4-3.4"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.4"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                ></path>
              </svg>
              <span id="settings-order-label">
                {entered ? (newest ? T.webOrderNewest : T.webOrderOldest) : "Oldest first"}
              </span>
            </button>
          </div>
        </div>

        <BoardBlock shown={shown} goToPage={goToPage} />
        <div className="block" id="settings-timeline">
          <b id="settings-timeline-title">{timelineWords("Enable Project Timeline", "啟用專案時間軸")}</b>
          <p className="say" id="settings-timeline-say">
            {timelineWords("Keep a traceable history of deliveries and availability.", "保留可追溯的交付與上線紀錄。")}
          </p>
          <button className="chip" id="settings-timeline-toggle" type="button" aria-pressed="false" disabled>
            Loading…
          </button>{" "}
          <button
            className="chip"
            id="settings-timeline-history"
            type="button"
            onClick={() => goToPage("projects")}
          >
            {timelineWords("Open timeline", "開啟時間軸")}
          </button>
          <p className="say" id="settings-timeline-status" role="status"></p>
        </div>
        <div className="foot">
          <span id="settings-version" style={entered ? { cursor: "pointer" } : undefined} onClick={pressVersion}>
            {version}
          </span>
        </div>
        <button
          className="chip wide"
          id="settings-close"
          type="button"
          data-page-to="sessions"
          ref={closeRef}
          onClick={() => goToPage("sessions")}
        >
          {T.webClose}
        </button>
      </div>
    </section>
  )
}

export const page: PageModule = { id: "settings", Component: SettingsPage }
