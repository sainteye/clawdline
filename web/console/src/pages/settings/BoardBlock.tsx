import { useEffect, useRef, useState, useSyncExternalStore } from "react"
import * as L from "../../legacy/bridge.js"
import {
  applyBoardMode,
  boardCommand,
  boardMode,
  refreshBoardMode,
  subscribeBoardMode,
} from "../../legacy/board-bridge.js"

/**
 * The settings page's Project Board block: `#settings-board` in `index.html`,
 * drawn and driven by `BoardControls` (`input/board-settings.js`).
 *
 * `apply` draws from the last board answer that said whether the board is on,
 * whoever read it — this page on arrival (`Settings.enter` →
 * `BoardControls.refresh`), the Board page on each of its reads — which is
 * `board-bridge.ts`'s `boardMode`. Until one has arrived the block is as the
 * markup has it: English, and both toggles "Loading…" and off.
 *
 * The two toggles are `bind`'s two listeners, rule for rule: one command at a
 * time; the same command, same request id, is sent again after a failure that
 * may have followed a successful write; a failure that cannot have is dropped
 * and, for the mode toggle, the board read again. Both need `viewer.canManage`,
 * so a device that may only read sees them disabled, as there.
 */
function words(en: string, zh: string): string {
  return /^zh/i.test(document.documentElement.lang || navigator.language || "") ? zh : en
}

/** The refusals after which the write may still have happened. */
const RETRYABLE = /offline|busy|timeout|unavailable|network|connection|persistence_failed/

type Failure = { code?: string; retryable?: boolean }
type Command = Record<string, unknown> & { operation: string }

export function BoardBlock({ shown, goToPage }: { shown: boolean; goToPage: (name: string) => void }) {
  const board = useSyncExternalStore(subscribeBoardMode, boardMode)
  const [status, setStatus] = useState("")
  const [saving, setSaving] = useState(false)
  const savingRef = useRef(false)
  const pending = useRef<Command | null>(null)
  const operationError = useRef(false)

  const setBusy = (on: boolean) => {
    savingRef.current = on
    setSaving(on)
  }

  // `BoardControls.refresh`, on every arrival.
  const refresh = () =>
    refreshBoardMode()
      .then(() => {
        if (!operationError.current && !savingRef.current) setStatus("")
      })
      .catch((error: unknown) => {
        setStatus(L.failureSentence(error, words("Board unavailable", "無法讀取看板設定")))
      })

  useEffect(() => {
    if (shown) void refresh()
  }, [shown])

  const canManage = board?.viewer?.canManage === true
  const provider = board?.viewer?.narrativeProvider
  const providerName = provider === "codex" ? "OpenAI / Codex" : provider === "claude" ? "Anthropic / Claude" : ""
  const aiAllowed = !!provider && board?.narrativeConsent === provider

  const pressMode = () => {
    const latest = boardMode()
    if (!latest || latest.viewer?.canManage !== true || savingRef.current) return
    const command: Command = pending.current || {
      operation: "set_enabled",
      enabled: !latest.enabled,
      expectedRevision: latest.revision,
      requestId: crypto.randomUUID(),
    }
    if (command.operation !== "set_enabled") return
    pending.current = command
    setBusy(true)
    operationError.current = false
    setStatus(words("Saving…", "儲存中…"))
    boardCommand(command)
      .then((result) => {
        pending.current = null
        applyBoardMode(result.board)
        setStatus(words("Saved", "已儲存"))
      })
      .catch((error: Failure) => {
        // A network loss may follow a successful write: retry the same request.
        if (error.code && error.retryable !== true && !RETRYABLE.test(error.code)) {
          pending.current = null
          void refresh()
        }
        operationError.current = true
        setStatus(
          L.failureSentence(error, words("Save failed", "儲存失敗")) + words(" · Press again to retry.", " · 再按一次重試。"),
        )
      })
      .finally(() => setBusy(false))
  }

  const pressAI = () => {
    const latest = boardMode()
    if (!latest || latest.viewer?.canManage !== true || savingRef.current) return
    const who = latest.viewer?.narrativeProvider
    if (who !== "codex" && who !== "claude") return
    const command: Command = pending.current || {
      operation: "set_ai_consent",
      provider: who,
      enabled: latest.narrativeConsent !== who,
      policy: "board-reading-v1",
      expectedRevision: latest.revision,
      requestId: crypto.randomUUID(),
    }
    if (command.operation !== "set_ai_consent") return
    pending.current = command
    setBusy(true)
    boardCommand(command)
      .then((result) => {
        pending.current = null
        operationError.current = false
        applyBoardMode(result.board)
        setStatus(words("Saved", "已儲存"))
      })
      .catch((error: Failure) => {
        operationError.current = true
        if (error.code && error.retryable !== true && !RETRYABLE.test(error.code)) pending.current = null
        setStatus(L.failureSentence(error, words("Save failed", "儲存失敗")))
      })
      .finally(() => setBusy(false))
  }

  return (
    <div className="block" id="settings-board">
      <b id="settings-board-title">{board ? words("Enable Project Board", "啟用看板系統") : "Enable Project Board"}</b>
      <p className="say" id="settings-board-say">
        {board
          ? words(
              "Currently free. Organize work, sessions, usage and delivery under each Project. Disable to use the standard workflow; history is retained.",
              "目前免費。以 Project 項目整合派工、進度、用量與成果。關閉後使用一般流程，歷史紀錄仍保留。",
            )
          : "Currently free. Turn off to use the standard workflow; history is retained."}
      </p>
      <button
        className={board?.enabled ? "chip on" : "chip"}
        id="settings-board-toggle"
        type="button"
        aria-pressed={board ? (String(board.enabled) as "true" | "false") : "true"}
        disabled={!board || saving || !canManage}
        onClick={pressMode}
      >
        {board ? (board.enabled ? words("Enabled", "已啟用") : words("Disabled", "已關閉")) : "Loading…"}
      </button>{" "}
      <button
        className="chip"
        id="settings-board-history"
        type="button"
        data-page-to="projects"
        onClick={() => goToPage("projects")}
      >
        {board ? words("Open projects", "開啟專案") : "Open projects"}
      </button>
      <p className="say" id="settings-board-status" role="status">
        {status}
      </p>
      <b id="settings-board-ai-title">{board ? words("AI reading summaries", "AI 閱讀摘要") : "AI reading summaries"}</b>
      <p className="say" id="settings-board-ai-say">
        {board
          ? words(
              "Separate opt-in. Send stored Board titles, descriptions and documented outcomes to ",
              "獨立同意設定。將已儲存的看板標題、描述與成果文字送至 ",
            ) +
            providerName +
            words(
              " to summarize in your Clawdline language. Uses the configured naming model and its quota. No transcript, credential-file or attachment reading. Original text stays available; Board OFF stops generation.",
              "，以 Clawdline 設定語言整理；使用已設定的命名模型與其額度。不讀取完整對話、憑證檔或附件；原文保留，關閉看板即停止生成。",
            )
          : "Off until you consent to sending stored Board titles, descriptions and documented outcomes to your selected AI provider. No transcript or attachment reading."}
      </p>
      <button
        className="chip"
        id="settings-board-ai-toggle"
        type="button"
        aria-pressed={board ? (String(aiAllowed) as "true" | "false") : "false"}
        disabled={!board || saving || !canManage || !providerName}
        onClick={pressAI}
      >
        {board
          ? aiAllowed
            ? words("Stop AI sharing", "停止 AI 外送整理")
            : words("Allow sharing with ", "同意送至 ") + providerName
          : "Loading…"}
      </button>
    </div>
  )
}

