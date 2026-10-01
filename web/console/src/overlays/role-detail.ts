import type { Persona } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { personaName, personaSummary, personaTeams } from "../personas.js"

const node = (id: string) => document.getElementById(id)
const esc = L.escapeHTML
let shown: { session: string; persona: string } | null = null

const teamWords = {
  engineering: "personaTeamEngineering", marketing: "personaTeamMarketing",
  product: "personaTeamProduct", quality: "personaTeamQuality",
  operations: "personaTeamOperations", design: "personaTeamDesign",
  business: "personaTeamBusiness",
} as const
const kindWords = {
  epic: "personaKindEpic", feature: "personaKindFeature", issue: "personaKindIssue",
} as const

function sourceLinks(source: string): string {
  return source.split(",").map((part) => part.trim()).filter((url) => /^https?:\/\//i.test(url))
    .map((url) => '<a href="' + esc(url) + '" target="_blank" rel="noopener noreferrer">' +
      esc(url.replace(/[?#].*$/, "").split("/").pop() || url) + "</a>").join(", ")
}

function paint(persona: Persona): void {
  const content = node("role-detail-content")
  if (!content) return
  const focusedSource = content.contains(document.activeElement) && document.activeElement instanceof HTMLAnchorElement
    ? document.activeElement.href : ""
  const teams = personaTeams(persona).map((team) => esc(nextWord(teamWords[team]))).join(" · ")
  const kinds = (persona.suggested_kinds || []).map((kind) => {
    const key = kindWords[kind as keyof typeof kindWords]
    return key ? esc(nextWord(key)) : ""
  }).filter(Boolean).join(" · ")
  const sources = sourceLinks(persona.source || "")
  content.innerHTML =
    '<div class="role-detail-head"><canvas id="role-detail-icon" width="0" height="0" aria-hidden="true"></canvas>' +
    '<div><p class="role-detail-eyebrow">' + esc(nextWord("personaPicker")) + '</p>' +
    '<h2 id="role-detail-title">' + esc(personaName(persona)) + '</h2></div></div>' +
    '<p class="role-detail-summary">' + esc(personaSummary(persona)) + '</p>' +
    '<dl><dt>' + esc(nextWord("personaTeamPicker")) + '</dt><dd>' + teams + '</dd>' +
    '<dt>' + esc(nextWord("personaSuggestedKinds")) + '</dt><dd>' + (kinds || esc(nextWord("personaSuggestedNone"))) + '</dd>' +
    (sources ? '<dt>' + esc(nextWord("personaSource")) + '</dt><dd class="role-detail-sources">' + sources + '</dd>' : "") +
    '</dl><p class="role-detail-note">' + esc(nextWord("personaSuggestionNote")) + '</p>'
  L.paintIcon(content.querySelector<HTMLCanvasElement>("#role-detail-icon"), persona.icon, 5)
  if (focusedSource) {
    const link = [...content.querySelectorAll<HTMLAnchorElement>("a[href]")].find((a) => a.href === focusedSource)
    ;(link || node("role-detail-close"))?.focus({ preventScroll: true })
  }
}

export const RoleDetail = {
  isOpen(): boolean { return !!shown },
  open(session: string, persona: Persona): void {
    if (!node("role-detail")) return
    shown = { session, persona: persona.id }
    paint(persona)
    node("role-detail")!.hidden = false
    ;(node("info") as HTMLElement | null)?.setAttribute("inert", "")
    ;(node("role-detail-close") as HTMLButtonElement | null)?.focus({ preventScroll: true })
  },
  close(restore = true): void {
    if (!shown) return
    shown = null
    const overlay = node("role-detail")
    if (overlay) overlay.hidden = true
    node("info")?.removeAttribute("inert")
    if (restore) {
      const button = node("info-body")?.querySelector<HTMLButtonElement>("button[data-role-detail]")
      ;(button || node("info-close"))?.focus({ preventScroll: true })
    }
  },
  follow(session: string | null, persona: Persona | null): void {
    if (!shown) return
    if (!session || !persona || shown.session !== session || shown.persona !== persona.id) {
      RoleDetail.close()
      return
    }
    paint(persona)
  },
}

/** The top sheet traps keyboard focus, while the Session Info sheet is inert. */
export function bindRoleDetail(): () => void {
  const overlay = node("role-detail")
  const sheet = overlay?.querySelector<HTMLElement>(".role-detail-sheet")
  const close = node("role-detail-close")
  if (!overlay || !sheet || !close) return () => {}
  const onOverlay = () => RoleDetail.close()
  const onSheet = (event: Event) => event.stopPropagation()
  const onClose = () => RoleDetail.close()
  const onKey = (event: KeyboardEvent) => {
    if (!shown || event.key !== "Tab") return
    const focusable = [...sheet.querySelectorAll<HTMLElement>('a[href], button:not([disabled])')]
    if (!focusable.length) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (event.shiftKey && (document.activeElement === first || !sheet.contains(document.activeElement))) {
      event.preventDefault(); last.focus()
    } else if (!event.shiftKey && (document.activeElement === last || !sheet.contains(document.activeElement))) {
      event.preventDefault(); first.focus()
    }
  }
  overlay.addEventListener("click", onOverlay)
  sheet.addEventListener("click", onSheet)
  close.addEventListener("click", onClose)
  document.addEventListener("keydown", onKey, true)
  return () => {
    overlay.removeEventListener("click", onOverlay)
    sheet.removeEventListener("click", onSheet)
    close.removeEventListener("click", onClose)
    document.removeEventListener("keydown", onKey, true)
  }
}
