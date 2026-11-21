import { useEffect, useLayoutEffect, useState, useSyncExternalStore } from "react"
import type { SessionRow, SessionShell } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { focusModalContainer } from "../modal-focus.js"
import { backgroundCounts, backgroundLine, backgroundLive, showsBackground, type BackgroundWords } from "./background.js"
import { WorkTree } from "./WorkTree.js"
import "./background.css"
import { callbackWord } from "./callback-copy.js"
import { deliverUntilSeen, nextMessages, pendingSends, queueSessionInputInBrowser } from "./send.js"
import { toast } from "../overlays/toast.js"

/**
 * The one line above the composer that says a session has work going on in
 * the background, and opens it — where Claude Code puts the same thing, at the
 * very bottom of its screen.
 *
 * Drawn only when there is something to open (`showsBackground`). Tapping it
 * opens a sheet with the commands the session left running, each of which
 * opens the Shell panel, and the provider's subagents, each of which opens its
 * transcript in the pane as the work tree always has.
 */
export function BackgroundStrip({ row, selected, onAgent, onShell, canAsk, machineQueues, onAsked, onSent }: {
  row: SessionRow
  selected: string | null
  onAgent: (id: string | null) => void
  onShell: (shell: SessionShell) => void
  canAsk: boolean
  machineQueues: boolean
  onAsked: () => void
  onSent?: () => void
}) {
  const [open, setOpen] = useState(false)
  useSyncExternalStore(pendingSends.subscribe, pendingSends.getVersion)
  const counts = backgroundCounts(row)
  const shows = showsBackground(counts) || !!row.background_omitted
  useEffect(() => {
    if (!shows) setOpen(false)
  }, [shows])
  useLayoutEffect(() => {
    if (open) focusModalContainer(document.getElementById("bg-sheet"))
  }, [open])
  if (!shows) return null

  const T = L.strings
  const close = (restore: boolean) => {
    setOpen(false)
    if (restore) document.getElementById("bg-strip-line")?.focus({ preventScroll: true })
  }
  const shells = row.shells ?? []
  const question = nextWord("bgQuestion")
  const conversation = row.sessionId || undefined
  const questionPending = () => pendingSends.of(row.id).some(card =>
    card.conversation === conversation && (card.requestText ?? card.text) === question)
  const ask = () => {
    if (!canAsk || questionPending()) return
    const queued = queueSessionInputInBrowser(row, machineQueues)
    const now = Date.now()
    const card = queued ? nextMessages.add(row.id, question, [], now, conversation)
      : pendingSends.add(row.id, question, [], now, false, conversation)
    if (!card) {
      toast(nextWord("sendQueueFull"), true)
      return
    }
    if (!queued) void deliverUntilSeen(card).then(code => { if (!code) onSent?.() })
    close(true)
    onAsked()
  }
  return (
    <div className="bg-strip" data-open={open ? "on" : "off"}>
      <button
        className="bg-strip-line"
        id="bg-strip-line"
        type="button"
        aria-expanded={open}
        aria-haspopup="dialog"
        aria-controls="bg-sheet"
        title={nextWord("bgOpen")}
        onClick={() => setOpen((was) => !was)}
      >
        <span className="dot" data-live={backgroundLive(counts) ? "on" : "off"} />
        <span className="said">{[backgroundLine(counts, stripWords()), row.background_reading === "unavailable" ? nextWord("bgUnavailable") : ""].filter(Boolean).join(" · ")}</span>
        <span className="chev" aria-hidden="true">›</span>
      </button>
      {open ? (
        <>
          <button className="bg-sheet-backdrop" type="button" tabIndex={-1} aria-label={nextWord("bgClose")}
            onClick={() => close(false)} />
          <div
            className="bg-sheet"
            id="bg-sheet"
            role="dialog"
            aria-labelledby="bg-sheet-title"
            onKeyDown={(ev) => {
              if (ev.key !== "Escape") return
              ev.preventDefault()
              ev.stopPropagation()
              close(true)
            }}
          >
            <div className="bg-sheet-head">
              <h2 id="bg-sheet-title">{nextWord("bgTitle")}</h2>
              {shells.length > 0 && <button className="chip bg-sheet-ask" type="button"
                disabled={!canAsk || questionPending()} onClick={ask}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7"
                  strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
                  <path d="M22 2 15 22l-4-9-9-4L22 2ZM22 2 11 13" />
                </svg>
                {nextWord("bgAskSession")}
              </button>}
              <button className="chip" id="bg-sheet-close" type="button" onClick={() => close(true)}>
                {nextWord("bgClose")}
              </button>
            </div>
            <div className="bg-sheet-body">
 {row.background_reading === "unavailable" && <p role="status">{nextWord("bgUnavailable")}</p>}
 {!!row.background_omitted && <p role="status">{nextWord("bgMore")}</p>}
              {shells.length ? (
                <section className="agents bg-shells" aria-label={T.webShells}>
                  <div className="head"><span>{T.webShells}</span><span className="n">{shells.length}</span></div>
                  {shells.map((shell) => (
                    <button
                      className="bg-shell" type="button" key={shell.id} data-shell={shell.id}
                      title={T.webShellOpen}
                      onClick={() => {
                        setOpen(false)
                        onShell(shell)
                      }}
                    >
                      <span className="mark" />
                      <span className="cmd">{shell.command || shell.id}</span>
                      <span className="at">{L.clock(shell.at)}</span>
                      <span className="sub">
                        {[shell.what, shell.kind === "callback" ? callbackWord(shell.state) : shell.doing].filter(Boolean).join(" · ") || T.webShellRunning}
                      </span>
                    </button>
                  ))}
                </section>
              ) : null}
              <WorkTree
                row={row}
                selected={selected}
                onProvider={(id) => {
                  setOpen(false)
                  onAgent(id)
                }}
              />
            </div>
          </div>
        </>
      ) : null}
    </div>
  )
}

function stripWords(): BackgroundWords {
  return {
    shellOne: nextWord("bgShellOne"),
    shellMany: nextWord("bgShellMany"),
    agentRunningOne: nextWord("bgAgentRunningOne"),
    agentRunningMany: nextWord("bgAgentRunningMany"),
    agentListedOne: nextWord("bgAgentListedOne"),
    agentListedMany: nextWord("bgAgentListedMany"),
    agentUnknown: nextWord("bgAgentUnknown"),
  }
}
