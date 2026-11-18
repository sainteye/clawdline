import { useLayoutEffect, useRef } from "react"
import type { Icon, Persona } from "@clawdline/contract"
import { BRAND_MARK } from "../brand-mark.js"
import { paintIcon } from "../legacy/bridge.js"
import { personaById } from "../personas.js"

const colors = ["#e2c072", "#86c9c9", "#9db0ee", "#e88a7c", "#9ccf8e", "#cfa2e6"]
const shapes = [
  ["..####..", ".######.", "########", "##.##.##", "###++###", ".######.", "..#..#.."],
  ["...++...", "..++++..", ".######.", "##.##.##", "###++###", ".######.", "..#..#.."],
  [".+....+.", ".##..##.", "########", "##.##.##", "########", ".##++##.", "..####.."],
  ["..#..#..", "..####..", ".######.", "#####++#", "#####++#", ".######.", "..##.##."],
  ["...++...", "...##...", ".######.", "#+...+##", "#..+..##", ".######.", "..####.."],
]
const robots: Icon[] = colors.flatMap(accent => shapes.map(rows => ({ accent,
  cells: rows.map(row => Array.from(row, cell => cell === "#" ? accent : cell === "+" ? "#efece7" : null)),
})))

export function sessionBoardAvatar(id: string, persona: string | undefined, lead: boolean, personas: readonly Persona[]): Icon {
  if (lead) return BRAND_MARK
  const role = personaById(personas, persona)
  if (role) return role.icon
  // A rename, reorder or change of Lead does not change the fallback.
  let hash = 2166136261
  for (let i = 0; i < id.length; i++) hash = Math.imul(hash ^ id.charCodeAt(i), 16777619)
  return robots[(hash >>> 0) % robots.length]
}

export function SessionBoardMark({ icon, lead = false, project = false }: { icon: Icon; lead?: boolean; project?: boolean }) {
  const ref = useRef<HTMLCanvasElement>(null)
  useLayoutEffect(() => { paintIcon(ref.current, icon, 2) }, [icon])
  return <span className={project ? "session-board-project-mark" : "session-board-avatar"} aria-hidden="true">
    <canvas ref={ref} width={0} height={0} />
    {lead && <span className="clawdfather-crown" />}
  </span>
}
