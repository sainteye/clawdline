import assert from "node:assert/strict"
import test from "node:test"
import type { Persona } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path is required by Node's native type stripping.
import { headPersona, personaById, personasOfTeam, personaTeams, rowPersonaLine, shownTeam, suggestedPersona, switchTeam, teamsOffered, withTeams } from "./personas.ts"

const icon = { accent: "#000000", cells: [["#000000"]] }
const persona = (id: string, kinds: string[], teams = ["engineering"]): Persona => ({
  id,
  teams,
  name: { en: id, "zh-Hant": id },
  summary: { en: "", "zh-Hant": "" },
  suggested_kinds: kinds,
  icon,
  source: "",
})
const catalog = [
  persona("architect", ["epic"]),
  persona("backend", ["feature"]),
  persona("frontend", ["feature"]),
  persona("minimal-change", ["issue"]),
  persona("code-reviewer", []),
]

test("a Board item's kind suggests the one persona that names it", () => {
  assert.equal(suggestedPersona(catalog, "epic")?.id, "architect")
  assert.equal(suggestedPersona(catalog, "issue")?.id, "minimal-change")
})

test("a kind two personas suggest, or none does, is left for the person to choose", () => {
  assert.equal(suggestedPersona(catalog, "feature"), null)
  assert.equal(suggestedPersona(catalog, "plan"), null)
  assert.equal(suggestedPersona(null, "epic"), null)
})

test("an id the catalog does not name is no persona", () => {
  assert.equal(personaById(catalog, "architect")?.id, "architect")
  assert.equal(personaById(catalog, "janitor"), null)
  assert.equal(personaById(catalog, undefined), null)
  assert.equal(personaById(null, "architect"), null)
})

test("a catalog that cannot be read is an empty one, read once", async () => {
  // A fresh module, so the one-read cache starts empty.
  // @ts-expect-error -- a `.ts` path is required by Node's native type stripping.
  const fresh = (await import("./personas.ts?refused")) as typeof import("./personas.js")
  let reads = 0
  const refused = (async () => {
    reads++
    return new Response(JSON.stringify({ error: "cloud_machine_unsupported" }), { status: 409 })
  }) as typeof fetch
  assert.deepEqual(await fresh.loadPersonas(refused), [])
  assert.deepEqual(await fresh.loadPersonas(refused), [])
  assert.equal(reads, 1)
  assert.deepEqual(fresh.personasNow(), [])
})

test("a catalog read that throws is an empty one", async () => {
  // @ts-expect-error -- a `.ts` path is required by Node's native type stripping.
  const fresh = (await import("./personas.ts?offline")) as typeof import("./personas.js")
  const offline = (async () => {
    throw new TypeError("Failed to fetch")
  }) as typeof fetch
  assert.deepEqual(await fresh.loadPersonas(offline), [])
})

test("a catalog read keeps only well-formed personas", async () => {
  // @ts-expect-error -- a `.ts` path is required by Node's native type stripping.
  const fresh = (await import("./personas.ts?answered")) as typeof import("./personas.js")
  const answered = (async () =>
    new Response(JSON.stringify({ personas: [catalog[0], { id: "" }, null], license: "MIT" }), { status: 200 })) as typeof fetch
  const list = await fresh.loadPersonas(answered)
  assert.deepEqual(
    list.map((p) => p.id),
    ["architect"],
  )
})

test("the detail header shows a persona only when the catalog names it", () => {
  const named: Persona[] = [{ ...persona("architect", []), summary: { en: "Draws the plan.", "zh-Hant": "畫出計畫。" } }]
  const known = headPersona(named, "architect")
  assert.equal(known?.persona.id, "architect")
  assert.ok(known?.name)
  assert.ok(known?.summary)
  assert.ok(known?.title.includes(known.summary))
  assert.equal(headPersona(named, "janitor"), null, "unknown to the catalog")
  assert.equal(headPersona(named, undefined), null, "no persona on the row")
  assert.equal(headPersona(named, ""), null, "an empty persona")
  assert.equal(headPersona(null, "architect"), null, "catalog not read")
  assert.equal(headPersona([], "architect"), null, "catalog unreadable")
})

test("a session row has a role line only for a persona the catalog names", () => {
  const known = { ...persona("frontend", ["feature"]), summary: { en: "Builds the console.", "zh-Hant": "" } }
  const line = rowPersonaLine([known, ...catalog], "frontend")
  assert.equal(line?.persona, known)
  assert.equal(line?.name, "frontend")
  assert.equal(line?.title, "frontend — Builds the console.")
  assert.equal(rowPersonaLine(catalog, "janitor"), null)
  assert.equal(rowPersonaLine(catalog, undefined), null)
  assert.equal(rowPersonaLine(catalog, ""), null)
  assert.equal(rowPersonaLine(null, "frontend"), null)
})

const teams = [
  ...catalog.map((p) =>
    p.id === "frontend" ? { ...p, teams: ["engineering", "design"] } : p.id === "code-reviewer" ? { ...p, teams: ["engineering", "quality"] } : p,
  ),
  persona("seo", [], ["marketing"]),
  persona("technical-writer", [], ["engineering", "marketing"]),
  persona("qa", [], ["quality"]),
]
// A daemon older than the `teams` field sends one `team`, or older still, none.
const single = [
  ...catalog.map(({ teams: _teams, ...rest }) => ({ ...rest, team: "engineering" }) as unknown as Persona),
  { ...persona("seo", []), teams: undefined, team: "marketing" } as unknown as Persona,
]
const untagged = catalog.map(({ teams: _teams, ...rest }) => rest as unknown as Persona)

test("the switcher offers the teams that have personas, in team order, and the chips show one team", () => {
  assert.deepEqual(teamsOffered(teams), ["engineering", "marketing", "quality", "design"])
  assert.deepEqual(personasOfTeam(teams, "marketing").map((p) => p.id), ["seo", "technical-writer"])
  assert.deepEqual(personasOfTeam(teams, "quality").map((p) => p.id), ["code-reviewer", "qa"])
  assert.deepEqual(personasOfTeam(teams, "design").map((p) => p.id), ["frontend"])
  assert.deepEqual(personasOfTeam(teams, "engineering").map((p) => p.id), [...catalog.map((p) => p.id), "technical-writer"])
  assert.deepEqual(personasOfTeam(teams, "business"), [])
})

test("a persona's teams are read from teams, an older single team, or neither", () => {
  assert.deepEqual(personaTeams(teams[2]), ["engineering", "design"])
  // Listed out of order, the switcher's order wins; unknown names are dropped.
  assert.deepEqual(personaTeams(persona("x", [], ["quality", "sales", "engineering"])), ["engineering", "quality"])
  assert.deepEqual(personaTeams(persona("x", [], ["sales"])), ["engineering"])
  assert.deepEqual(personaTeams(persona("x", [], [])), ["engineering"])
  assert.deepEqual(single.map(personaTeams).at(-1), ["marketing"])
  assert.deepEqual(teamsOffered(single), ["engineering", "marketing"])
  assert.deepEqual(withTeams(single.at(-1)!).teams, ["marketing"])
  assert.deepEqual(withTeams(untagged[0]).teams, ["engineering"])
})

test("a catalog without teams is all engineering, one team, so no switcher", () => {
  assert.deepEqual(teamsOffered(untagged), ["engineering"])
  assert.equal(personasOfTeam(untagged, "engineering").length, catalog.length)
  assert.equal(personasOfTeam(untagged, "marketing").length, 0)
  assert.equal(shownTeam(untagged, "", "marketing"), "engineering")
  assert.deepEqual(teamsOffered(null), [])
})

test("the team shown holds the chosen persona: the remembered one if it does, else its first team", () => {
  assert.equal(shownTeam(teams, "seo", "engineering"), "marketing")
  assert.equal(shownTeam(teams, "architect", "marketing"), "engineering")
  // In several teams: the remembered one when it holds the persona.
  assert.equal(shownTeam(teams, "technical-writer", "marketing"), "marketing")
  assert.equal(shownTeam(teams, "code-reviewer", "quality"), "quality")
  // Otherwise the persona's first team in switcher order.
  assert.equal(shownTeam(teams, "code-reviewer", "marketing"), "engineering")
  assert.equal(shownTeam(teams, "qa", "engineering"), "quality")
  assert.equal(shownTeam(teams, "frontend", "sales"), "engineering")
  // No persona: the remembered team, then engineering.
  assert.equal(shownTeam(teams, "", "quality"), "quality")
  assert.equal(shownTeam(teams, "", "business"), "engineering")
  assert.equal(shownTeam(teams, "janitor", "sales"), "engineering")
  assert.equal(shownTeam(teams, null, ""), "engineering")
  // The Board's kind default (epic → architect) shows the engineering team.
  assert.equal(shownTeam(teams, suggestedPersona(teams, "epic")?.id, "marketing"), "engineering")
})

test("switching team keeps a choice when the new team holds it too", () => {
  assert.deepEqual(switchTeam(teams, "architect", "marketing"), { team: "marketing", chosen: "" })
  assert.deepEqual(switchTeam(teams, "seo", "marketing"), { team: "marketing", chosen: "seo" })
  assert.deepEqual(switchTeam(teams, "technical-writer", "marketing"), { team: "marketing", chosen: "technical-writer" })
  assert.deepEqual(switchTeam(teams, "code-reviewer", "quality"), { team: "quality", chosen: "code-reviewer" })
  assert.deepEqual(switchTeam(teams, "code-reviewer", "design"), { team: "design", chosen: "" })
  assert.deepEqual(switchTeam(teams, "", "engineering"), { team: "engineering", chosen: "" })
})
