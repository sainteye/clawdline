import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { changedCharterSections, charterLineDiff, charterSections, charterTemplate, replaceCharterSection, sectionFromParts, sectionParts, shownCharterSection } from "./charter-text.ts"

const doc = "Intro line\n\n## Who does what\n\n- Orbit: reviews\n\n## Asking you\n\nAsk before deploying.\n"

test("sections split at level-two headings and join back to the same text", () => {
  const sections = charterSections(doc)
  assert.deepEqual(sections.map(s => s.heading), ["", "Who does what", "Asking you"])
  assert.equal(sections.map(s => s.text).join("\n"), doc)
  assert.equal(charterSections("## Only\n\ntext").length, 1)
  assert.equal(charterSections("\n## A\nx").filter(shownCharterSection).length, 1)
  // A "###" heading stays inside its section.
  assert.equal(charterSections("## A\n### sub\nx").length, 1)
})

test("replacing one section leaves the others and the spacing untouched", () => {
  const next = replaceCharterSection(doc, 1, "## Who does what\n\n- Orbit: reviews\n- Beacon: tests")
  assert.equal(next, "Intro line\n\n## Who does what\n\n- Orbit: reviews\n- Beacon: tests\n\n## Asking you\n\nAsk before deploying.\n")
  assert.equal(replaceCharterSection(doc, 2, "## Asking you\n\nAsk first.\n\n\n"), "Intro line\n\n## Who does what\n\n- Orbit: reviews\n\n## Asking you\n\nAsk first.\n")
})

test("the line diff marks removed and added lines in order", () => {
  assert.deepEqual(charterLineDiff("a\nb\nc", "a\nx\nc"), [
    { kind: "same", text: "a" }, { kind: "removed", text: "b" }, { kind: "added", text: "x" }, { kind: "same", text: "c" },
  ])
})

test("only the sections a change touches are reported, paired by heading", () => {
  const after = doc.replace("Ask before deploying.", "Ask before deploying or deleting.")
  const changed = changedCharterSections(doc, after)
  assert.deepEqual(changed.map(c => c.heading), ["Asking you"])
  assert.deepEqual(changed[0].lines.filter(l => l.kind !== "same").map(l => l.kind), ["removed", "added"])
  const added = changedCharterSections(doc, doc + "\n## Cards\n\nUse cards.\n")
  assert.deepEqual(added.map(c => c.heading), ["Cards"], JSON.stringify(added))
})

test("the template lists the Project's Bots under the first heading", () => {
  const text = charterTemplate(["Who", "How"], ["", "Steps."], ["Orbit", "Beacon"], ": ")
  assert.equal(text, "## Who\n\n- Orbit: \n- Beacon: \n\n## How\n\nSteps.\n")
})

test("the editor edits a section's body apart from its title, and clearing the body keeps the heading", () => {
  const [intro, who] = charterSections(doc)
  assert.deepEqual(sectionParts(who), { title: "Who does what", body: "- Orbit: reviews" })
  assert.deepEqual(sectionParts(intro), { title: "", body: "Intro line" })
  // Clearing the body to rewrite it keeps "## Who does what".
  assert.equal(replaceCharterSection(doc, 1, sectionFromParts(who, "Who does what", "")), "Intro line\n\n## Who does what\n\n## Asking you\n\nAsk before deploying.\n")
  // An empty title keeps the old heading; a new one renames it on one line.
  assert.equal(sectionFromParts(who, "  ", "New body"), "## Who does what\n\nNew body")
  assert.equal(sectionFromParts(who, "Roles\nand owners", "x"), "## Roles and owners\n\nx")
  assert.equal(sectionFromParts(intro, "ignored", "\nNew intro\n"), "New intro")
  // An unchanged title and body give back the same document.
  const parts = sectionParts(who)
  assert.equal(replaceCharterSection(doc, 1, sectionFromParts(who, parts.title, parts.body)), doc)
})
