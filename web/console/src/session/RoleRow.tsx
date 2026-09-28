import { useEffect, useRef } from "react"
import type { Persona } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import {
  type PersonaTeam,
  personaById,
  personaName,
  personasOfTeam,
  personaTitle,
  shownTeam,
  teamsOffered,
} from "../personas.js"
import "./persona.css"
import "./role-row.css"

/** Each team's name in next-strings.ts. */
const TEAM_WORDS = {
  engineering: "personaTeamEngineering",
  marketing: "personaTeamMarketing",
  product: "personaTeamProduct",
  quality: "personaTeamQuality",
  operations: "personaTeamOperations",
  design: "personaTeamDesign",
  business: "personaTeamBusiness",
} as const satisfies Record<PersonaTeam, string>

/** What one role row draws and what it answers to. */
export interface RoleRowChoice {
  personas: readonly Persona[]
  /** The chosen persona's id; "" for no role. */
  chosen: string
  /** The team last picked here, shown when no persona is chosen. */
  team: string
  disabled: boolean
  /** How a chip says it is chosen: a radio in a radiogroup, or a pressed button. */
  press: "radio" | "pressed"
  onPick: (id: string) => void
  onTeam: (team: PersonaTeam) => void
}

/**
 * The role row (docs/personas.md), drawn into `row` by the start sheet, its
 * resume step and the Board's new-Session assignment alike: the label, then
 * "No role" and one chip per persona of the team shown.
 *
 * With personas in more than one team the label is a native select naming the
 * team (a phone gets its own picker); with one team, or a catalog from a daemon
 * older than teams, it is the plain label it always was.
 */
export function drawRoleRow(row: HTMLElement, c: RoleRowChoice): void {
  row.innerHTML = ""
  const offered = teamsOffered(c.personas)
  const chosen = personaById(c.personas, c.chosen) ? c.chosen : ""
  const team = shownTeam(c.personas, chosen, c.team)
  if (offered.length > 1) {
    const wrap = document.createElement("span")
    wrap.className = "with-label role-team"
    const select = document.createElement("select")
    select.setAttribute("aria-label", nextWord("personaTeamPicker"))
    select.disabled = c.disabled
    offered.forEach((t) => {
      const option = document.createElement("option")
      option.value = t
      option.textContent = nextWord(TEAM_WORDS[t])
      option.selected = t === team
      select.appendChild(option)
    })
    select.onchange = () => c.onTeam(select.value as PersonaTeam)
    wrap.appendChild(select)
    row.appendChild(wrap)
  } else {
    const label = document.createElement("span")
    label.className = "with-label"
    label.textContent = nextWord("personaPicker")
    row.appendChild(label)
  }
  const chip = (id: string, words: string, title: string) => {
    const button = document.createElement("button")
    button.type = "button"
    button.dataset.personaId = id
    button.className = "chip" + (id ? " persona-chip" : "") + (id === chosen ? " on" : "")
    button.disabled = c.disabled
    if (c.press === "radio") {
      button.setAttribute("role", "radio")
      button.setAttribute("aria-checked", id === chosen ? "true" : "false")
    } else {
      button.setAttribute("aria-pressed", id === chosen ? "true" : "false")
    }
    if (title) button.title = title
    button.onclick = () => c.onPick(id)
    const name = document.createElement("span")
    name.textContent = words
    button.appendChild(name)
    row.appendChild(button)
    return button
  }
  chip("", nextWord("personaNone"), "")
  personasOfTeam(c.personas, team).forEach((p) => {
    const button = chip(p.id, personaName(p), personaTitle(p))
    const bot = document.createElement("canvas")
    bot.className = "persona-tag-bot"
    bot.setAttribute("aria-hidden", "true")
    if (L.drawIcon(bot, p.icon, 2)) button.insertBefore(bot, button.firstChild)
  })
}

/** The role row as a React element, for the Board. */
export function RoleRow(props: RoleRowChoice & { className: string; describedBy?: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const callbacks = useRef({ onPick: props.onPick, onTeam: props.onTeam })
  callbacks.current = { onPick: props.onPick, onTeam: props.onTeam }
  useEffect(() => {
    if (!ref.current) return
    const row = ref.current
    const active = document.activeElement instanceof HTMLButtonElement && row.contains(document.activeElement)
      ? document.activeElement.dataset.personaId ?? null
      : null
    const scrolled = row.scrollLeft
    drawRoleRow(row, {
      personas: props.personas,
      chosen: props.chosen,
      team: props.team,
      disabled: props.disabled,
      press: props.press,
      onPick: (id) => callbacks.current.onPick(id),
      onTeam: (team) => callbacks.current.onTeam(team),
    })
    if (active !== null) {
      const replacement = Array.from(row.querySelectorAll<HTMLButtonElement>("button"))
        .find((button) => button.dataset.personaId === active)
      replacement?.focus({ preventScroll: true })
    }
    row.scrollLeft = scrolled
  }, [props.personas, props.chosen, props.team, props.disabled, props.press])
  return <div className={props.className} ref={ref} role="radiogroup" aria-label={nextWord("personaPicker")}
    aria-describedby={props.describedBy} />
}
