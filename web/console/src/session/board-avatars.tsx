import { useLayoutEffect, useRef } from "react"
import type { Icon, Persona } from "@clawdline/contract"
import { BRAND_MARK } from "../brand-mark.js"
import { paintIcon } from "../legacy/bridge.js"
import { personaById } from "../personas.js"
import { TerminalGlyph } from "./TerminalGlyph.js"

const squadRolePrefix = "clawdline.persona."

export function sessionBoardAvatar(id: string, persona: string | undefined, lead: boolean, personas: readonly Persona[]): Icon | null {
  if (lead) return BRAND_MARK
  const short = persona?.startsWith(squadRolePrefix) ? persona.slice(squadRolePrefix.length) : persona
  const assigned = personaById(personas, persona) ?? personaById(personas, short)
  if (assigned) return assigned.icon

  // The display catalog uses short IDs for built-ins and namespaced IDs for custom roles.
  // This is a visual choice only. It never changes the Squad assignment.
  const builtin = personas.filter(role => !role.id.includes(".") || role.id.startsWith(squadRolePrefix))
    .sort((a, b) => a.id.localeCompare(b.id, "en"))
  if (!builtin.length) return null
  let hash = 2166136261
  for (let i = 0; i < id.length; i++) hash = Math.imul(hash ^ id.charCodeAt(i), 16777619)
  return builtin[(hash >>> 0) % builtin.length].icon
}

export function SessionBoardMark({ icon, lead = false, project = false }: { icon: Icon | null; lead?: boolean; project?: boolean }) {
  const ref = useRef<HTMLCanvasElement>(null)
  useLayoutEffect(() => { paintIcon(ref.current, icon ?? undefined, lead ? 4 : 2) }, [icon, lead])
  return <span className={project ? "session-board-project-mark" : "session-board-avatar"} data-lead={lead || undefined} aria-hidden="true">
    {icon ? <canvas ref={ref} width={0} height={0} /> : <TerminalGlyph />}
    {lead && <span className="clawdfather-crown" />}
  </span>
}
