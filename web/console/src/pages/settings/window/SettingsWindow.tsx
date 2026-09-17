import { useCallback, useEffect, useRef, useState } from "react"
import type { SettingsSnapshot } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import { readSettings, writeSettings } from "../api.js"
import { ASSISTANT_LABEL, W, dictationStatus, hotkeyFailedTitle, seconds } from "./copy.js"
import { DEFAULTS, reading, type SettingKey } from "./defaults.js"
import {
  APP_EVENT,
  HOTKEY_EVENT,
  STATE_EVENT,
  ask,
  inShell,
  listen,
  type ShellChosenApp,
  type ShellRecording,
  type ShellState,
} from "./bridge.js"
import { Block, Chip, Head, MemoField, Mono, Note, PopUp, Row, Slider, Switch, TabStrip } from "./controls.js"

/**
 * The native "Clawdline 設定" window, as a web page.
 *
 * The Swift app draws this window in AppKit, 3,822 lines of it
 * (`Sources/Settings.swift`). This is the same window: the same six tabs in the
 * same order, the same rows in the same columns, the same words — every one of
 * them a property of `Copy+Chinese.swift`, in copy.ts — and the same behaviour,
 * which is that **there is no OK button** because there is nothing to cancel.
 * Each control writes what the config file would have said, and the shell picks
 * it up the way it would pick up a hand edit.
 *
 * ## Why the window is a page
 *
 * Because of what has to be true on Linux and on Windows. The shell around this
 * is a window, a hotkey and a file picker; everything above that — layout,
 * words, state, what a row does — is the same on all three, so it is written
 * once, here, and the next platform ships a shell rather than a settings
 * window. The parts that genuinely cannot be done by a page go over the bridge
 * in bridge.ts, and the list is short enough to read in one sitting.
 *
 * In a browser with no shell around it the window still draws and still works:
 * every row that is a value in the file reads and writes, and the handful that
 * need the machine — recording a key, picking an application, the Claude Code
 * hook — say plainly that they need one instead of pretending.
 *
 * ## What is not here
 *
 * The tabs the original fills from state this daemon does not keep: paired
 * devices and the Cloud account, the tunnel's own reading, the schedule list,
 * the dispatch policy card, the smart-notification health card. Those are
 * features rather than settings, and inventing a card that reports nothing
 * would be worse than the honest gap. Their switches — the ones that are a
 * value in the file — are all here. See docs/shell-bridge.md.
 */

/** The value being written, over what the file last said, so a slider moves while it saves. */
type Draft = Partial<Record<SettingKey, string | number | boolean>>

export function SettingsWindow() {
  const [snapshot, setSnapshot] = useState<SettingsSnapshot | null>(null)
  const [shell, setShell] = useState<ShellState | null>(null)
  const [draft, setDraft] = useState<Draft>({})
  const [tab, setTab] = useState(0)
  const [said, setSaid] = useState("")
  const [recording, setRecording] = useState(false)
  const [pendingHotkey, setPendingHotkey] = useState<string | null>(null)
  const [busyHooks, setBusyHooks] = useState(false)
  const rememberedScope = useRef("")
  const recordingRef = useRef(false)
  recordingRef.current = recording

  const has = inShell()

  const snapshotRef = useRef(snapshot)
  snapshotRef.current = snapshot

  const read = useCallback(() => {
    readSettings().then(
      (answer) => {
        setSnapshot(answer)
        setDraft({})
        setSaid("")
      },
      (error: unknown) => setSaid(sentence(error)),
    )
  }, [])

  useEffect(() => {
    read()
    ask({ kind: "state" })
  }, [read])

  /**
   * Write, then tell the shell. The Swift app's `apply()`: save the file and
   * post `clawdlineConfigChanged`, and let the one place that re-applies things
   * re-apply them. What a control then shows is what came back, not what was
   * asked for.
   */
  const change = useCallback(async (keys: Record<string, string | number | boolean>) => {
    setDraft((prior) => ({ ...prior, ...(keys as unknown as Draft) }))
    setSaid("")
    try {
      const answer = await writeSettings(keys)
      setSnapshot(answer)
      setDraft((prior) => {
        const next = { ...prior }
        for (const key of Object.keys(keys)) delete next[key as SettingKey]
        return next
      })
      ask({ kind: "changed" })
      return answer
    } catch (error) {
      setDraft((prior) => {
        const next = { ...prior }
        for (const key of Object.keys(keys)) delete next[key as SettingKey]
        return next
      })
      setSaid(sentence(error))
      throw error
    }
  }, [])

  // The shell's answers. A reading replaces what the page believed about the
  // machine; a recording is the one thing the page asked for and could not do.
  useEffect(() => {
    const stop = [
      listen<ShellState>(STATE_EVENT, (next) => {
        if (next.scopeApp) rememberedScope.current = next.scopeApp
        setShell(next)
        setPendingHotkey(null)
        setRecording(false)
      }),
      listen<ShellRecording>(HOTKEY_EVENT, (answer) => {
        setRecording(false)
        if (!answer.spec) return
        setPendingHotkey(answer.display || answer.spec)
        void change({ hotkey: answer.spec }).then(
          () => {},
          () => {
            // The shell let the old combination go while listening; it comes back.
            ask({ kind: "stopRecording" })
            setPendingHotkey(null)
          },
        )
      }),
      listen<ShellChosenApp>(APP_EVENT, (answer) => {
        if (!answer.id) return
        // From the file rather than from this closure: the answer arrives after
        // a trip through a file picker, and the render it was asked in is gone.
        const ids = String(reading(snapshotRef.current, "scope_app"))
          .split(",")
          .map((id) => id.trim())
          .filter(Boolean)
        if (ids.includes(answer.id)) return
        void change({ scope_app: [...ids, answer.id].join(",") })
      }),
    ]
    return () => stop.forEach((undo) => undo())
    // The listeners read the freshest state through refs held by the closures
    // below; re-subscribing on every keystroke would drop an answer in flight.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // A recording does not outlive the window it was started from.
  useEffect(() => {
    const leave = () => {
      if (recordingRef.current) ask({ kind: "stopRecording" })
    }
    window.addEventListener("pagehide", leave)
    return () => {
      leave()
      window.removeEventListener("pagehide", leave)
    }
  }, [])

  /** What a control shows: what is being written, else what the file says, else the default. */
  function now<K extends SettingKey>(key: K): (typeof DEFAULTS)[K] {
    const pending = draft[key]
    if (pending !== undefined) return pending as (typeof DEFAULTS)[K]
    return reading(snapshotRef.current, key)
  }

  function scopeIds(): string[] {
    return String(now("scope_app"))
      .split(",")
      .map((id) => id.trim())
      .filter(Boolean)
  }

  const flip = (key: SettingKey) => (on: boolean) => {
    void change({ [key]: on })
  }
  const pick = (key: SettingKey) => (value: string) => {
    void change({ [key]: value })
  }
  const slide = (key: SettingKey) => ({
    value: Number(now(key)),
    onChange: (value: number) => setDraft((prior) => ({ ...prior, [key]: value })),
    onCommit: (value: number) => {
      if (value === Number(reading(snapshotRef.current, key))) {
        setDraft((prior) => {
          const next = { ...prior }
          delete next[key]
          return next
        })
        return
      }
      void change({ [key]: value })
    },
  })

  // MARK: the tabs

  const panes = [generalPane, barPane, voicePane, remotePane, orchestratorPane, hooksPane]
  const titles = [
    W.settingsGeneral,
    W.settingsBar,
    W.settingsVoice,
    W.settingsRemote,
    W.settingsOrchestrator,
    W.settingsHooks,
  ]

  function generalPane() {
    const ids = scopeIds()
    const global = String(now("scope_app")) === ""
    const named = new Map((shell?.apps ?? []).map((app) => [app.id, app]))
    const naming = now("codex_auto_name")
      ? String(now("auto_name_assistant"))
      : "off"
    return (
      <>
        <div className="sw-column">
          <Row label={W.settingsHotkey} first>
            <Chip
              wide
              armed={recording}
              disabled={!has || pendingHotkey !== null}
              title={has ? undefined : NO_SHELL}
              onClick={() => {
                if (recording) {
                  ask({ kind: "stopRecording" })
                  setRecording(false)
                  return
                }
                if (ask({ kind: "record" })) setRecording(true)
              }}
            >
              {recording
                ? W.settingsRecording
                : (pendingHotkey ?? (shell ? (shell.hotkey ? shell.display : W.settingsOff) : hotkeyText(String(now("hotkey")))))}
            </Chip>
          </Row>
          {shell?.failed || (!shell && !has) ? (
            <p className="sw-said">{shell?.failed ? hotkeyFailedTitle(shell.display) : NO_SHELL}</p>
          ) : null}

          <Row label={W.settingsLanguage}>
            <PopUp
              label={W.settingsLanguage}
              value={String(now("language"))}
              options={LANGUAGES}
              onPick={pick("language")}
            />
          </Row>
          <Row label={W.menuMascot}>
            <PopUp
              label={W.menuMascot}
              value={String(now("mascot"))}
              options={(shell?.mascots?.length ? shell.mascots : [String(now("mascot"))]).map((name) => ({
                label: name,
                value: name,
              }))}
              onPick={pick("mascot")}
            />
          </Row>
          <Row label={W.settingsSessionTerminal} hint={W.settingsSessionTerminalHint}>
            <PopUp
              label={W.settingsSessionTerminal}
              value={String(now("terminal"))}
              options={[
                { label: W.settingsAuto, value: "auto" },
                { label: "iTerm2", value: "iterm" },
                { label: "tmux", value: "tmux" },
              ]}
              onPick={pick("terminal")}
            />
          </Row>
          <Row label={W.settingsScopeGlobal}>
            <Switch
              label={W.settingsScopeGlobal}
              on={global}
              onChange={(on) => {
                // "Every app" on empties the list; off restores what was there,
                // and only a config that has always been global falls back to
                // the one terminal this is known to work with.
                if (on) {
                  if (ids.length) rememberedScope.current = ids.join(",")
                  void change({ scope_app: "" })
                  return
                }
                const back = ids.length ? ids.join(",") : rememberedScope.current || DEFAULTS.scope_app
                void change({ scope_app: back })
              }}
            />
          </Row>
          <Block label={W.settingsScope} hint={W.settingsScopeHint}>
            <div className={global ? "sw-apps dimmed" : "sw-apps"}>
              {ids.map((id) => {
                const app = named.get(id)
                return (
                  <div className="sw-app" key={id}>
                    {app?.icon ? (
                      <img className="sw-app-icon" src={app.icon} alt="" width={16} height={16} />
                    ) : (
                      <span className="sw-app-icon" aria-hidden="true" />
                    )}
                    <span className={app && !app.unresolved ? "sw-app-name" : "sw-app-name plain"}>
                      {app && !app.unresolved ? app.name : id}
                    </span>
                    <button
                      type="button"
                      className="sw-chip sw-app-remove"
                      title={W.settingsScopeRemove}
                      aria-label={W.settingsScopeRemove}
                      disabled={global}
                      onClick={() => {
                        const left = ids.filter((other) => other !== id)
                        void change({ scope_app: left.join(",") })
                      }}
                    >
                      ✕
                    </button>
                  </div>
                )
              })}
              <AddApp
                disabled={global}
                running={(shell?.runningApps ?? []).filter((app) => !ids.includes(app.id))}
                onPick={(id) => {
                  if (ids.includes(id)) return
                  void change({ scope_app: [...ids, id].join(",") })
                }}
                onBrowse={() => ask({ kind: "chooseApp" })}
                canBrowse={has}
              />
            </div>
          </Block>
        </div>

        <div className="sw-column">
          <Row label={W.settingsReopen} hint={W.settingsReopenHint} first>
            <Switch label={W.settingsReopen} on={!!now("reopen_on_return")} onChange={flip("reopen_on_return")} />
          </Row>
          <Row label={W.settingsFollow} hint={W.settingsFollowHint}>
            <Switch label={W.settingsFollow} on={!!now("follow_target")} onChange={flip("follow_target")} />
          </Row>
          <Row label={W.settingsCodexAutoName} hint={W.settingsCodexAutoNameHint}>
            <PopUp
              label={W.settingsCodexAutoName}
              value={naming}
              options={[
                { label: W.settingsOff, value: "off" },
                { label: ASSISTANT_LABEL.codex, value: "codex" },
                { label: ASSISTANT_LABEL.claude, value: "claude" },
              ]}
              onPick={(value) => {
                // One control, three states, and no impossible pair of an off
                // switch beside a still-selected provider: off remembers who it
                // was, so turning it back on does not change whose quota it spends.
                if (value === "off") void change({ codex_auto_name: false })
                else void change({ codex_auto_name: true, auto_name_assistant: value })
              }}
            />
          </Row>
          <Row label={W.settingsNotch} hint={W.settingsNotchHint}>
            <Switch label={W.settingsNotch} on={!!now("notch")} onChange={flip("notch")} />
          </Row>
        </div>
      </>
    )
  }

  function barPane() {
    return (
      <>
        <div className="sw-column">
          <Head>{W.settingsBar}</Head>
          <Row label={W.settingsPosition} first>
            <Slider
              label={W.settingsPosition}
              min={0.05}
              max={0.8}
              step={0.01}
              format={(v) => `${Math.round(v * 100)}%`}
              {...slide("y_fraction")}
            />
          </Row>
          <Row label={W.settingsWidth}>
            <Slider
              label={W.settingsWidth}
              min={360}
              max={1400}
              step={1}
              format={(v) => `${Math.round(v)} pt`}
              {...slide("width")}
            />
          </Row>
          <Row label={W.settingsOpacity}>
            <Slider
              label={W.settingsOpacity}
              min={0}
              max={1}
              step={0.01}
              format={(v) => `${Math.round(v * 100)}%`}
              {...slide("card_opacity")}
            />
          </Row>
        </div>
        <div className="sw-column">
          <Head>{W.settingsReading}</Head>
          <Row label={W.settingsShow} first>
            <PopUp
              label={W.settingsShow}
              value={String(now("output_mode"))}
              options={[
                { label: W.settingsAuto, value: "auto" },
                { label: W.settingsTranscript, value: "transcript" },
                { label: W.settingsTerminal, value: "terminal" },
              ]}
              onPick={pick("output_mode")}
            />
          </Row>
          <Row label={W.settingsPaneHeight}>
            <Slider
              label={W.settingsPaneHeight}
              min={80}
              max={900}
              step={1}
              format={(v) => `${Math.round(v)} pt`}
              {...slide("output_height")}
            />
          </Row>
          <Row label={W.settingsTextSize}>
            <Slider
              label={W.settingsTextSize}
              min={8}
              max={28}
              step={0.5}
              format={(v) => `${v.toFixed(1)} pt`}
              {...slide("output_size")}
            />
          </Row>
          <Row label={W.settingsPaneFont}>
            <PopUp
              label={W.settingsPaneFont}
              value={String(now("output_font"))}
              options={(shell?.fonts?.length ? shell.fonts : [String(now("output_font"))]).map((name) => ({
                label: name,
                value: name,
              }))}
              onPick={pick("output_font")}
            />
          </Row>
          <Row label={W.settingsBlur}>
            <Slider
              label={W.settingsBlur}
              min={0}
              max={1}
              step={0.01}
              format={(v) => `${Math.round(v * 100)}%`}
              {...slide("backdrop")}
            />
          </Row>
          <Row label={W.settingsNewestFirst}>
            <Switch
              label={W.settingsNewestFirst}
              on={!!now("output_newest_first")}
              onChange={flip("output_newest_first")}
            />
          </Row>
        </div>
      </>
    )
  }

  function voicePane() {
    return (
      <>
        <div className="sw-column">
          <Row label={W.settingsEngine} first>
            <PopUp
              label={W.settingsEngine}
              value={String(now("voice_engine"))}
              options={[
                { label: W.settingsAuto, value: "auto" },
                { label: "Apple", value: "apple" },
                { label: "Whisper", value: "whisper" },
              ]}
              onPick={pick("voice_engine")}
            />
          </Row>
          <Row label={W.settingsSettle}>
            <Slider
              label={W.settingsSettle}
              min={0}
              max={8}
              step={0.1}
              format={(v) => (v === 0 ? W.settingsOff : seconds(v))}
              {...slide("voice_settle_seconds")}
            />
          </Row>
          <Row label={W.settingsStop}>
            <Slider
              label={W.settingsStop}
              min={0}
              max={30}
              step={0.1}
              format={(v) => (v === 0 ? W.settingsOff : seconds(v))}
              {...slide("voice_stop_seconds")}
            />
          </Row>
        </div>
        <div className="sw-column">
          {shell?.dictation ? (
            <Block>
              <Note dot={shell.dictation.kind === "ready" ? "live" : "warn"}>
                {dictationStatus(shell.dictation)}
              </Note>
            </Block>
          ) : null}
        </div>
      </>
    )
  }

  function remotePane() {
    return (
      <>
        <div className="sw-column">
          <Row label={W.settingsRemoteServe} hint={W.settingsRemoteHint} first>
            <Switch label={W.settingsRemoteServe} on={!!now("remote")} onChange={flip("remote")} />
          </Row>
          <Row label={W.settingsRemoteWrite} hint={W.settingsRemoteWriteHint}>
            <Switch label={W.settingsRemoteWrite} on={!!now("remote_write")} onChange={flip("remote_write")} />
          </Row>
          <Row label={W.settingsPushDelivery} hint={W.settingsPushDeliveryHint}>
            <Switch
              label={W.settingsPushDelivery}
              on={!!now("push_on_delivery")}
              onChange={flip("push_on_delivery")}
            />
          </Row>
          <Row label={W.settingsPushFanout} hint={W.settingsPushFanoutHint}>
            <Switch label={W.settingsPushFanout} on={!!now("push_on_fanout")} onChange={flip("push_on_fanout")} />
          </Row>
          <Row label={W.settingsSmartNotifications} hint={W.settingsSmartNotificationsHint}>
            <Switch
              label={W.settingsSmartNotifications}
              on={!!now("smart_notifications")}
              onChange={flip("smart_notifications")}
            />
          </Row>
          <Row label={W.settingsPushDeploy} hint={W.settingsPushDeployHint}>
            <Switch label={W.settingsPushDeploy} on={!!now("push_on_deploy")} onChange={flip("push_on_deploy")} />
          </Row>
          <Row label={W.settingsAgentNotify} hint={W.settingsAgentNotifyNote}>
            <Switch
              label={W.settingsAgentNotify}
              on={!!now("orchestrator_agent_notify")}
              onChange={flip("orchestrator_agent_notify")}
            />
          </Row>
        </div>
        <div className="sw-column">
          <Row label={W.settingsTunnel} hint={W.settingsTunnelHint} first>
            <PopUp
              label={W.settingsTunnel}
              value={String(now("remote_tunnel"))}
              options={[
                { label: W.settingsOff, value: "off" },
                { label: W.settingsTunnelQuick, value: "quick" },
                { label: W.settingsTunnelNamed, value: "named" },
              ]}
              onPick={pick("remote_tunnel")}
            />
          </Row>
          <Row label={W.settingsTunnelHostname}>
            <MemoField
              label={W.settingsTunnelHostname}
              value={String(now("remote_hostname"))}
              example="clawdline.example.com"
              onCommit={(value) => {
                if (value === String(reading(snapshotRef.current, "remote_hostname"))) return
                void change({ remote_hostname: value })
              }}
            />
          </Row>
        </div>
      </>
    )
  }

  function orchestratorPane() {
    const linger = Number(now("orchestrator_child_linger"))
    return (
      <>
        <div className="sw-column">
          <Row label={W.settingsOrchestratorEnabled} hint={W.settingsOrchestratorEnabledHint} first>
            <Switch
              label={W.settingsOrchestratorEnabled}
              on={!!now("orchestrator_enabled")}
              onChange={flip("orchestrator_enabled")}
            />
          </Row>
          <Row label={W.settingsOrchestratorMax} hint={W.settingsOrchestratorMaxHint}>
            <PopUp
              label={W.settingsOrchestratorMax}
              value={String(now("orchestrator_max_children"))}
              options={Array.from({ length: 10 }, (_, i) => ({ label: String(i + 1), value: String(i + 1) }))}
              onPick={(value) => void change({ orchestrator_max_children: Number(value) })}
            />
          </Row>
          <Row label={W.settingsOrchestratorPermission} hint={W.settingsOrchestratorPermissionHint}>
            <PopUp
              label={W.settingsOrchestratorPermission}
              value={String(now("orchestrator_permission"))}
              options={[
                { label: W.settingsOrchestratorPermissionAsk, value: "ask" },
                { label: W.settingsOrchestratorPermissionEdits, value: "edits" },
                { label: W.settingsOrchestratorPermissionFull, value: "full" },
              ]}
              onPick={pick("orchestrator_permission")}
            />
          </Row>
          <Row label={W.settingsOrchestratorNotify} hint={W.settingsOrchestratorNotifyHint}>
            <Switch
              label={W.settingsOrchestratorNotify}
              on={!!now("orchestrator_notify_root")}
              onChange={flip("orchestrator_notify_root")}
            />
          </Row>
          <Row label={W.settingsOrchestratorClose} hint={W.settingsOrchestratorCloseHint}>
            <PopUp
              label={W.settingsOrchestratorClose}
              // A hand-edited value between the stops shows as the nearest one
              // and is left alone until this control is touched.
              value={linger < 0 ? "-1" : linger === 0 ? "0" : "180"}
              options={[
                { label: W.settingsOrchestratorCloseNow, value: "0" },
                { label: W.settingsOrchestratorCloseLinger, value: "180" },
                { label: W.settingsOrchestratorCloseKeep, value: "-1" },
              ]}
              onPick={(value) => void change({ orchestrator_child_linger: Number(value) })}
            />
          </Row>
        </div>
        <div className="sw-column" />
      </>
    )
  }

  function hooksPane() {
    const hooks = shell?.hooks
    const state = !hooks?.installed ? "off" : hooks.heard ? "live" : "on"
    return (
      <>
        <div className="sw-column">
          <Block hint={W.settingsHooksHint}>
            <Note
              dot={state === "off" ? "idle" : state === "live" ? "live" : "warn"}
              trailing={
                <Chip
                  disabled={!hooks?.supported || busyHooks}
                  title={hooks?.supported ? undefined : NO_HOOKS}
                  onClick={() => {
                    if (!hooks) return
                    setBusyHooks(true)
                    ask({ kind: "hooks", install: !hooks.installed })
                    window.setTimeout(() => setBusyHooks(false), 1500)
                  }}
                >
                  {hooks?.installed ? W.settingsHooksRemove : W.settingsHooksInstall}
                </Chip>
              }
            >
              {state === "off" ? W.settingsHooksOff : state === "live" ? W.settingsHooksLive : W.settingsHooksOn}
            </Note>
          </Block>
          {/* Whose file the button writes into. Naming the path is the difference
              between "a switch in this app" and "an edit to somebody else's
              settings", which is what it actually is. */}
          <Mono>{hooks?.path ?? "~/.claude/settings.json"}</Mono>
          {!hooks?.supported ? <p className="sw-said">{has ? NO_HOOKS : NO_SHELL}</p> : null}
        </div>
        <div className="sw-column">
          <Block label={W.settingsStateHook} hint={W.settingsStateHookHint}>
            <Note dot={(snapshot?.on_state_change?.length ?? 0) > 0 ? "live" : "idle"} mono>
              {`"on_state_change": [` +
                (snapshot?.on_state_change ?? []).map((part) => `"${part}"`).join(", ") +
                `]`}
            </Note>
          </Block>
        </div>
      </>
    )
  }

  const Pane = panes[tab]
  return (
    <div className="sw-window">
      <TabStrip titles={titles} current={tab} onPick={setTab} />
      <div className="sw-scroll">
        <div className="sw-pane" id={`sw-pane-${tab}`} role="tabpanel" aria-labelledby={`sw-tab-${tab}`}>
          {Pane()}
        </div>
      </div>
      {said ? (
        <p className="sw-said sw-said-foot" role="status" aria-live="polite">
          {said}
        </p>
      ) : null}
      {/* Where the file is. Text, not a button — this window is not the whole of
          the settings and should not pretend to be, and the one thing somebody
          needs in order to reach the rest is to know where to look. */}
      <div className="sw-foot">
        <span
          className="sw-foot-path"
          title={shell?.configPath ?? snapshot?.path ?? ""}
          onDoubleClick={() => ask({ kind: "reveal", what: "config" })}
        >
          {shell?.configPath ?? snapshot?.path ?? ""}
        </span>
      </div>
    </div>
  )
}

/** `settingsScopeAdd`: the apps that are open, and a way to go and find one that is not. */
function AddApp({
  running,
  disabled,
  canBrowse,
  onPick,
  onBrowse,
}: {
  running: { id: string; name: string; icon?: string }[]
  disabled?: boolean
  canBrowse: boolean
  onPick: (id: string) => void
  onBrowse: () => void
}) {
  const [open, setOpen] = useState(false)
  useEffect(() => {
    if (!open) return
    const shut = () => setOpen(false)
    window.addEventListener("pointerdown", shut)
    window.addEventListener("keydown", shut)
    return () => {
      window.removeEventListener("pointerdown", shut)
      window.removeEventListener("keydown", shut)
    }
  }, [open])
  return (
    <div className="sw-add">
      <Chip
        disabled={disabled}
        onClick={() => {
          // With nothing open to offer, the only thing behind the chip is the
          // picker, so it opens straight into it.
          if (!running.length) {
            onBrowse()
            return
          }
          setOpen((was) => !was)
        }}
      >
        {W.settingsScopeAdd}
      </Chip>
      {open ? (
        <div className="sw-menu" onPointerDown={(e) => e.stopPropagation()}>
          <div className="sw-menu-head">{W.settingsScopeRunning}</div>
          {running.map((app) => (
            <button
              type="button"
              className="sw-menu-item"
              key={app.id}
              onClick={() => {
                setOpen(false)
                onPick(app.id)
              }}
            >
              {app.icon ? <img src={app.icon} alt="" width={16} height={16} /> : <span className="sw-app-icon" />}
              {app.name}
            </button>
          ))}
          <div className="sw-menu-rule" />
          <button
            type="button"
            className="sw-menu-item"
            disabled={!canBrowse}
            onClick={() => {
              setOpen(false)
              onBrowse()
            }}
          >
            <span className="sw-app-icon" />
            {W.settingsScopeChoose}
          </button>
        </div>
      ) : null}
    </div>
  )
}

/**
 * What the chip says with no shell to ask: the combination the file holds,
 * written as it is written, because nothing here can turn a spec into this
 * platform's symbols.
 */
function hotkeyText(spec: string): string {
  return spec || W.settingsOff
}

/**
 * `languagePopUp` (`Settings.swift`): the tags the catalog resolves, each shown
 * in its own language, because a list of languages written in a language you do
 * not read is not a list you can pick from. The names come from the platform's
 * own table, as they do there — `Locale.localizedString(forIdentifier:)` on that
 * side, `Intl.DisplayNames` on this one — rather than being written out here,
 * where they would be fourteen strings nobody in this repository can proofread.
 */
const LANGUAGE_TAGS = ["en", "zh-Hant", "zh-Hans", "ja", "ko", "es", "pt", "fr", "de", "ru", "it", "hi", "id", "tr"]

const LANGUAGES = [
  { label: W.settingsAuto, value: "auto" },
  ...LANGUAGE_TAGS.map((tag) => {
    let name = tag
    try {
      name = new Intl.DisplayNames([tag], { type: "language" }).of(tag) ?? tag
    } catch {
      /* a runtime without the table shows the tag, which is still a thing you can pick */
    }
    return { label: name.charAt(0).toUpperCase() + name.slice(1), value: tag }
  }),
]

/**
 * A refusal's own sentence, or the transport's.
 *
 * `detail` and never `code`: the code is the machine-readable half and the only
 * part anything may branch on, and a window that showed it would be showing
 * `invalid_output_size` to somebody who moved a slider.
 */
function sentence(error: unknown): string {
  if (error instanceof RefusalError) return error.detail || error.code
  if (error instanceof Error) return error.message
  return String(error)
}

/** Said under a control that needs a shell and has none. Not a `Copy+Chinese` word: the
 *  original window is only ever inside one, so it never had a sentence for this. */
const NO_SHELL = "這一項要在 Clawdline app 裡才能改"
const NO_HOOKS = "這個版本還沒有讀 hook 通知的東西，所以不會去動 ~/.claude/settings.json"
