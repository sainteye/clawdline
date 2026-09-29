import { useState } from "react"
import { nextWord } from "../../next-strings.js"
import { hostedConsole } from "./api.js"
import { TerminalProjectList } from "./TerminalProjectList.js"
import "./terminal.css"

/**
 * 終端 under the work page's project scope: folded shut, and its list read
 * only when opened, so the board costs no terminal request until then. Absent
 * on the console Clawdline Cloud serves.
 */
export function TerminalEntry({ project, label }: { project: string; label: string }) {
  const [open, setOpen] = useState(false)
  if (!project || hostedConsole()) return null
  return (
    <details className="terminal-entry" open={open} onToggle={(ev) => setOpen(ev.currentTarget.open)}>
      <summary aria-label={nextWord("terminalEntryFor", { project: label || project })}>{nextWord("terminalEntry")}</summary>
      {open && <TerminalProjectList project={project} label={label || project} shown={open} headingLevel={3} />}
    </details>
  )
}
