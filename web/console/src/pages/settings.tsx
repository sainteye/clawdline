import { catalogWord } from "../catalog.js"
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
import { followsRelay } from "../client.js"
import { SettingsWindow } from "./settings/window/SettingsWindow.js"
import "./settings/window/window.css"
import "./settings/page.css"
import { BoardBlock } from "./settings/BoardBlock.js"
import { CapacityBlock } from "./settings/CapacityBlock.js"
import { UpdatePanel } from "../machine/UpdatePanel.js"
import { asksForUpdatePanel } from "../machine/update-model.js"
import { DefaultModelsBlock } from "./settings/DefaultModelsBlock.js"
import { GateSettingsBlock } from "./settings/GateSettingsBlock.js"
import { nextWord } from "../next-strings.js"
import { BrowserLanguageControl } from "../ui-language/BrowserLanguageControl.js"
import {
  DEFAULT_FONT_SCALE,
  FONT_SCALE_STEPS,
  applyFontScale,
  readFontScale,
  rememberFontScale,
  stepFontScale,
} from "../font-scale.js"

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
 *   board is on, and its toggle sends `set_enabled`.
 * - **Capacity**: `CapacityBlock` (settings/CapacityBlock.tsx), this app's own
 *   block and not the original's: the register's rows and the dead letters from
 *   `/v1/capacity`. Every capacity push names it, so it reads through Clawdline
 *   Cloud as well.
 * - **Project Timeline**: this settings page has no project context to read,
 *   so its permanently disabled toggle says that the setting belongs to each
 *   project instead of presenting an unfinished read.
 * - **Version**: the Mac's version from `/v1/health`, which this daemon's does
 *   not carry, so the line is empty and hidden by `.foot span:empty`.
 *
 * The machine controls are embedded only for a browser served by the local
 * daemon. Cloud transports expose their narrower settings through dedicated
 * routes, so a hosted browser never mounts the local settings window.
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
  const cloud = followsRelay()
  const [tab, setTab] = useState(() => asksForUpdatePanel(location.hash) ? 2 : 0)
  const tabs = [
    catalogWord("settings", "browserTab"),
    catalogWord("settings", "workTab"),
    catalogWord("settings", "statusTab"),
    ...(!cloud ? [catalogWord("settings", "machineTab")] : []),
  ]
  // `enter` has not run until the page has been shown once; before that the
  // blocks it draws are as the markup has them.
  const [entered, setEntered] = useState(false)
  const [icons, setIcons] = useState(settingsAssistantIcons)
  const [newest, setNewest] = useState(settingsNewestFirst)
  const [fontScale, setFontScale] = useState(readFontScale)
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

  useEffect(() => {
    if (!shown) return
    const openUpdate = () => {
      if (asksForUpdatePanel(location.hash)) setTab(2)
    }
    openUpdate()
    window.addEventListener("hashchange", openUpdate)
    return () => window.removeEventListener("hashchange", openUpdate)
  }, [shown])

  // `Pages.go` lands the keyboard on the page's own control, `settings-close`.
  // A page the address asked for on arrival is reached while the document is
  // still hidden for its words, and nothing hidden takes focus; then the
  // landing waits for the words, as the shell's does for the wordmark.
  useLayoutEffect(() => {
    if (!shown) return
    // An address that asks for the update panel lands there instead
    // (`landOnUpdatePanel`), and a landing it already made is not taken back.
    const land = () => {
      if (asksForUpdatePanel(location.hash) || document.activeElement?.id === "settings-update-title") return
      closeRef.current?.focus({ preventScroll: true })
    }
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

  const chooseFontScale = (percent: number) => {
    applyFontScale(percent)
    setFontScale({ percent, storageAvailable: rememberFontScale(percent) })
  }

  const adjustFontScale = (direction: -1 | 1) => {
    setFontScale((current) => {
      const percent = stepFontScale(current.percent, direction)
      applyFontScale(percent)
      return { percent, storageAvailable: rememberFontScale(percent) }
    })
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

        <div className="settings-tabs" role="tablist" aria-label={T.webSettings}>
          {tabs.map((title, index) => (
            <button
              key={title}
              id={`settings-tab-${index}`}
              type="button"
              role="tab"
              aria-selected={tab === index}
              aria-controls={`settings-pane-${index}`}
              tabIndex={tab === index ? 0 : -1}
              onClick={() => setTab(index)}
              onKeyDown={(event) => {
                if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return
                event.preventDefault()
                const next = (index + (event.key === "ArrowRight" ? 1 : -1) + tabs.length) % tabs.length
                setTab(next)
                document.getElementById(`settings-tab-${next}`)?.focus()
              }}
            >{title}</button>
          ))}
        </div>

        <div className="settings-pane" id="settings-pane-0" role="tabpanel" aria-labelledby="settings-tab-0" hidden={tab !== 0}>
          <div className="settings-grid">
            <div className="settings-column">

        <div className="block" id="settings-ui-language">
          <BrowserLanguageControl id="settings-ui-language-select" />
        </div>

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

        <div className="block" id="settings-font-scale">
          <b id="settings-font-scale-title">{nextWord("fontScaleTitle")}</b>
          <p className="say" id="settings-font-scale-say">
            {nextWord("fontScaleHint")}
          </p>
          <div className="row font-scale-row" role="group" aria-labelledby="settings-font-scale-title">
            <button
              className="chip font-scale-button"
              id="settings-font-scale-smaller"
              type="button"
              aria-label={nextWord("fontScaleSmaller")}
              title={nextWord("fontScaleSmaller")}
              disabled={fontScale.percent === FONT_SCALE_STEPS[0]}
              onClick={() => adjustFontScale(-1)}
            >
              −
            </button>
            <output className="font-scale-value" aria-live="polite" aria-atomic="true">
              {fontScale.percent}%
            </output>
            <button
              className="chip font-scale-button"
              id="settings-font-scale-larger"
              type="button"
              aria-label={nextWord("fontScaleLarger")}
              title={nextWord("fontScaleLarger")}
              disabled={fontScale.percent === FONT_SCALE_STEPS[FONT_SCALE_STEPS.length - 1]}
              onClick={() => adjustFontScale(1)}
            >
              +
            </button>
            <button
              className="chip font-scale-reset"
              id="settings-font-scale-reset"
              type="button"
              disabled={fontScale.percent === DEFAULT_FONT_SCALE}
              onClick={() => chooseFontScale(DEFAULT_FONT_SCALE)}
            >
              {nextWord("fontScaleReset")}
            </button>
          </div>
          <p className="said" role="status" aria-live="polite">
            {fontScale.storageAvailable ? "" : nextWord("fontScaleNotRemembered")}
          </p>
        </div>

            </div>
            <div className="settings-column">

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
                {entered ? (newest ? T.webOrderNewest : T.webOrderOldest) : ""}
              </span>
            </button>
          </div>
        </div>

            </div>
          </div>
        </div>

        <div className="settings-pane" id="settings-pane-1" role="tabpanel" aria-labelledby="settings-tab-1" hidden={tab !== 1}>
          <div className="settings-grid">
            <div className="settings-column">
              <DefaultModelsBlock shown={shown && tab === 1} />

        <BoardBlock shown={shown} goToPage={goToPage} />
            </div>
            <div className="settings-column">
        <GateSettingsBlock shown={shown && tab === 1} />
        <div className="block" id="settings-timeline">
          <b id="settings-timeline-title">{timelineWords("Enable Project Timeline", catalogWord("literal", "b593e064f518"))}</b>
          <p className="say" id="settings-timeline-say">
            {timelineWords("Keep a traceable history of deliveries and availability.", catalogWord("literal", "acc0b43a1d07"))}
          </p>
          <button className="chip" id="settings-timeline-toggle" type="button" aria-pressed="false" disabled>
            {nextWord("timelineSetPerProject")}
          </button>{" "}
          <button
            className="chip"
            id="settings-timeline-history"
            type="button"
            onClick={() => goToPage("projects")}
          >
            {timelineWords("Open timeline", catalogWord("literal", "6f1c2c5c46b4"))}
          </button>
          <p className="say" id="settings-timeline-status" role="status"></p>
        </div>
            </div>
          </div>
        </div>

        <div className="settings-pane" id="settings-pane-2" role="tabpanel" aria-labelledby="settings-tab-2" hidden={tab !== 2}>
          <div className="settings-grid">
            <div className="settings-column">
        <UpdatePanel shown={shown} />
            </div>
            <div className="settings-column">
        <CapacityBlock shown={shown} />
            </div>
          </div>
        </div>

        {!cloud && <div className="settings-pane settings-machine-pane" id="settings-pane-3" role="tabpanel" aria-labelledby="settings-tab-3" hidden={tab !== 3}>
          {shown && tab === 3 && <SettingsWindow embedded />}
        </div>}
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
