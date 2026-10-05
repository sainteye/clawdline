import type { Persona } from "@clawdline/contract"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { personaName, personaSummary, personaTeams } from "../personas.js"
import type { WireCatalog, WireDefinition, WireNames } from "../pages/squad/wire.js"

const node = (id: string) => document.getElementById(id)
const esc = L.escapeHTML
interface RoleSkill { id: string; name: WireNames; purpose: WireNames; content: string; enabled: boolean; files: string[] }
interface RoleReading { body: string; version: string; skills: RoleSkill[]; origin: "snapshot" | "catalog"; handbook: string }
interface RoleSnapshot {
  snapshot: { definition_id: string; definition: { body: string; version: string }; handbook: { text: string };
    skills: { id: string; version: string; enabled: boolean; name: WireNames; purpose: WireNames; content: string;
      files?: { path: string }[] }[] }
}
let shown: { session: string; conversation: string; persona: string } | null = null
let reading: RoleReading | null = null
/** The refusal the role read came back with, kept so the sentence can name its code. */
let readError: { error: unknown } | null = null
let request = 0

const word = (names: WireNames): string => names[document.documentElement.lang.toLowerCase().startsWith("zh") ? "zh-Hant" : "en"] || names.en

async function readRole(conversation: string, persona: string): Promise<RoleReading> {
  if (conversation) {
    const response = await fetch(client.url("/v1/squad/session-snapshots/" + encodeURIComponent(conversation)), { credentials: "same-origin" })
    if (response.ok) {
      const result = await response.json() as RoleSnapshot
      const snapshot = result.snapshot
      if (snapshot?.definition_id !== "clawdline.persona." + persona || typeof snapshot.definition?.body !== "string") throw new Error("role_snapshot_mismatch")
      return { body: snapshot.definition.body, version: snapshot.definition.version, origin: "snapshot",
        handbook: snapshot.handbook?.text || "", skills: (snapshot.skills || []).map((skill) => ({
          id: skill.id, name: skill.name, purpose: skill.purpose, content: skill.content, enabled: skill.enabled,
          files: (skill.files || []).map((file) => file.path),
        })) }
    }
    // Older Sessions have no launch snapshot. An older daemon or Cloud peer
    // may also lack this read; the catalog below is explicitly labelled current.
    if (response.status !== 404 && response.status !== 501) throw new Error("role_snapshot_unavailable")
  }
  const response = await fetch(client.url("/v1/squad/definitions/" + encodeURIComponent("clawdline.persona." + persona)), { credentials: "same-origin" })
  if (!response.ok) throw new Error("role_catalog_unavailable")
  const definition = await response.json() as WireDefinition
  if (definition.definition_id !== "clawdline.persona." + persona || typeof definition.body !== "string") throw new Error("role_definition_missing")
  let catalog: WireCatalog | null = null
  if (definition.skills?.length) {
    const skillResponse = await fetch(client.url("/v1/squad/catalog"), { credentials: "same-origin" })
    if (!skillResponse.ok) throw new Error("role_skills_unavailable")
    catalog = await skillResponse.json() as WireCatalog
  }
  const skills = (definition.skills || []).map((ref) => {
    const skill = catalog?.skills?.find((item) => item.skill_id === ref.id && item.version === ref.version)
    return { id: ref.id, name: skill?.name || { en: ref.id, "zh-Hant": ref.id },
      purpose: skill?.purpose || { en: "", "zh-Hant": "" }, content: skill?.content || "", enabled: ref.enabled,
      files: (skill?.files || []).map((file) => file.path) }
  })
  return { body: definition.body, version: definition.version, skills, origin: "catalog", handbook: "" }
}

function load(): void {
  if (!shown) return
  const current = ++request
  const { conversation, persona } = shown
  reading = null
  readError = null
  void readRole(conversation, persona).then((value) => {
    if (current !== request || !shown) return
    reading = value
    paintCurrent()
  }, (error: unknown) => {
    if (current !== request || !shown) return
    readError = { error }
    paintCurrent()
  })
}

let currentPersona: Persona | null = null
function paintCurrent(): void { if (currentPersona && shown) paint(currentPersona) }

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
  const focusedSkill = document.activeElement instanceof HTMLElement && content.contains(document.activeElement) &&
    document.activeElement.tagName === "SUMMARY" ? document.activeElement.closest("details")?.getAttribute("data-skill") : null
  const scrollTop = content.scrollTop
  const expanded = new Set([...content.querySelectorAll<HTMLDetailsElement>("details[data-skill]")].filter((item) => item.open).map((item) => item.dataset.skill))
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
    '</dl><p class="role-detail-note">' + esc(nextWord("personaSuggestionNote")) + '</p>' +
    '<section class="role-detail-full"><h3>' + esc(nextWord("personaFullDefinition")) + '</h3>' +
    (reading ? '<p class="role-detail-origin">' + esc(nextWord(reading.origin === "snapshot" ? "personaSnapshotOrigin" : "personaCatalogOrigin")) + '</p>' +
      '<pre class="role-detail-text">' + esc(reading.body) + '</pre>' +
      (reading.handbook ? '<h3>' + esc(nextWord("personaHandbook")) + '</h3><pre class="role-detail-text">' + esc(reading.handbook) + '</pre>' : '') +
      '<h3>' + esc(nextWord("personaSkills")) + '</h3>' +
      (reading.skills.length ? reading.skills.map((skill, i) => '<details data-skill="' + i + '"><summary>' + esc(word(skill.name)) +
        (skill.enabled ? '' : ' · ' + esc(nextWord("personaSkillDisabled"))) + '</summary>' +
        (word(skill.purpose) ? '<p>' + esc(word(skill.purpose)) + '</p>' : '') +
        '<pre class="role-detail-text">' + esc(skill.content || nextWord("personaSkillUnavailable")) + '</pre>' +
        (skill.files.length ? '<p>' + esc(nextWord("personaSkillFiles")) + '</p><ul>' + skill.files.map((path) => '<li>' + esc(path) + '</li>').join('') + '</ul>' : '') +
        '</details>').join('')
        : '<p>' + esc(nextWord("personaSkillsNone")) + '</p>')
      : '<p role="status">' + esc(readError ? L.failureSentence(readError.error, nextWord("personaFullFailed")) : nextWord("personaFullLoading")) + '</p>') + '</section>'
  L.paintIcon(content.querySelector<HTMLCanvasElement>("#role-detail-icon"), persona.icon, 5)
  for (const detail of content.querySelectorAll<HTMLDetailsElement>("details[data-skill]")) detail.open = expanded.has(detail.dataset.skill)
  content.scrollTop = scrollTop
  if (focusedSkill !== null && focusedSkill !== undefined) content.querySelector<HTMLElement>('details[data-skill="' + focusedSkill + '"] summary')?.focus({ preventScroll: true })
  if (focusedSource) {
    const link = [...content.querySelectorAll<HTMLAnchorElement>("a[href]")].find((a) => a.href === focusedSource)
    ;(link || node("role-detail-close"))?.focus({ preventScroll: true })
  }
}

export const RoleDetail = {
  isOpen(): boolean { return !!shown },
  open(session: string, conversation: string, persona: Persona): void {
    if (!node("role-detail")) return
    shown = { session, conversation, persona: persona.id }
    currentPersona = persona
    reading = null
    readError = null
    paint(persona)
    node("role-detail")!.hidden = false
    ;(node("info") as HTMLElement | null)?.setAttribute("inert", "")
    ;(node("role-detail-close") as HTMLButtonElement | null)?.focus({ preventScroll: true })
    load()
  },
  close(restore = true): void {
    if (!shown) return
    shown = null
    currentPersona = null
    ++request
    const overlay = node("role-detail")
    if (overlay) overlay.hidden = true
    node("info")?.removeAttribute("inert")
    if (restore) {
      const button = node("info-body")?.querySelector<HTMLButtonElement>("button[data-role-detail]")
      ;(button || node("info-close"))?.focus({ preventScroll: true })
    }
  },
  follow(session: string | null, conversation: string, persona: Persona | null): void {
    if (!shown) return
    if (!session || !persona || shown.session !== session || shown.persona !== persona.id) {
      RoleDetail.close()
      return
    }
    currentPersona = persona
    if (shown.conversation !== conversation) { shown.conversation = conversation; load() }
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
