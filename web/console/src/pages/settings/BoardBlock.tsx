import { catalogWord } from "../../catalog.js"
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
 * markup has it: English, and its toggle "Loading…" and off.
 *
 * The toggle follows `bind`'s listener rule for rule: one command at a time;
 * the same command, same request id, is sent again after a failure that
 * may have followed a successful write; a failure that cannot have is dropped
 * and the board read again. It needs `viewer.canManage`, so a device that may
 * only read sees it disabled, as there.
 */
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
        setStatus(L.failureSentence(error, catalogWord("literal", "b253d8b3c409")))
      })

  useEffect(() => {
    if (shown) void refresh()
  }, [shown])

  const canManage = board?.viewer?.canManage === true

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
    setStatus(catalogWord("literal", "b27f67abf518"))
    boardCommand(command)
      .then((result) => {
        pending.current = null
        applyBoardMode(result.board)
        setStatus(catalogWord("literal", "d968c5e734ce"))
      })
      .catch((error: Failure) => {
        // A network loss may follow a successful write: retry the same request.
        if (error.code && error.retryable !== true && !RETRYABLE.test(error.code)) {
          pending.current = null
          void refresh()
        }
        operationError.current = true
        setStatus(
          L.failureSentence(error, catalogWord("literal", "a8f4898b1364")) + catalogWord("literal", "4c9f465602cb"),
        )
      })
      .finally(() => setBusy(false))
  }

  return (
    <div className="block" id="settings-board">
      <b id="settings-board-title">{catalogWord("literal", "9c2b7fce9411")}</b>
      <p className="say" id="settings-board-say">
        {board
          ? catalogWord("literal", "03e4342b12d6")
          : ""}
      </p>
      <button
        className={board?.enabled ? "chip on" : "chip"}
        id="settings-board-toggle"
        type="button"
        aria-pressed={board ? (String(board.enabled) as "true" | "false") : "true"}
        disabled={!board || saving || !canManage}
        onClick={pressMode}
      >
        {board ? (board.enabled ? catalogWord("literal", "2989c6b2870d") : catalogWord("literal", "1a40ea6c139f")) : ""}
      </button>{" "}
      <button
        className="chip"
        id="settings-board-history"
        type="button"
        data-page-to="projects"
        onClick={() => goToPage("projects")}
      >
        {catalogWord("literal", "e43bcf17f8bc")}
      </button>
      <p className="say" id="settings-board-status" role="status">
        {status}
      </p>
    </div>
  )
}
