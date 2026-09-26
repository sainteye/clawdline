import { useEffect, useLayoutEffect, useState } from "react"
import type { SessionRow, SessionShell } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { backgroundCounts, backgroundLine, backgroundLive, showsBackground, type BackgroundWords } from "./background.js"
import { WorkTree } from "./WorkTree.js"
import "./background.css"

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
export function BackgroundStrip({ row, selected, onAgent, onShell }: {
  row: SessionRow
  selected: string | null
  onAgent: (id: string | null) => void
  onShell: (shell: SessionShell) => void
}) {
  const [open, setOpen] = useState(false)
  useEffect(() => setOpen(false), [row.id])
  const counts = backgroundCounts(row)
  const shows = showsBackground(counts)
  useEffect(() => {
    if (!shows) setOpen(false)
  }, [shows])
  useLayoutEffect(() => {
    if (open) document.getElementById("bg-sheet-close")?.focus({ preventScroll: true })
  }, [open])
  if (!shows) return null

  const T = L.strings
  const close = (restore: boolean) => {
    setOpen(false)
    if (restore) document.getElementById("bg-strip-line")?.focus({ preventScroll: true })
  }
  const shells = row.shells ?? []
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
        <span className="said">{backgroundLine(counts, stripWords())}</span>
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
              <button className="chip" id="bg-sheet-close" type="button" onClick={() => close(true)}>
                {nextWord("bgClose")}
              </button>
            </div>
            <div className="bg-sheet-body">
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
                        {[shell.what, shell.doing].filter(Boolean).join(" · ") || T.webShellRunning}
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
