import { useState } from "react"
import { nextWord } from "../../next-strings.js"
import { hostedConsole } from "./api.js"
import { TerminalProjectList } from "./TerminalProjectList.js"
import { openTerminalPage } from "./navigate.js"
import "./terminal.css"

/**
 * 終端 under the work page's project scope: folded shut, and its list read
 * only when opened, so the board costs no terminal request until then. Absent
 * on the console Clawdline Cloud serves.
 */
export function TerminalEntry({ project, label }: { project: string; label: string }) {
  const [open, setOpen] = useState(false)
  if (!project) return null
  if (hostedConsole()) return <button className="board-button" type="button" onClick={() => openTerminalPage(project)}>
    {nextWord("terminalEntryFor", { project: label || project })}
  </button>
  return (
    <details className="terminal-entry" open={open} onToggle={(ev) => setOpen(ev.currentTarget.open)}>
      <summary aria-label={nextWord("terminalEntryFor", { project: label || project })}>{nextWord("terminalEntry")}</summary>
      {/* The summary already names it; the list's own heading is kept for a screen reader only. */}
      {open && <TerminalProjectList project={project} label={label || project} shown={open} headingLevel={3} />}
    </details>
  )
}
