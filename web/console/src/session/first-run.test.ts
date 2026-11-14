import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import * as First from "./first-run.ts"

function memory(entries: Record<string, string> = {}) {
  const values = new Map(Object.entries(entries))
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    values,
  }
}

test("the voice start button is hidden until the person turns it on", () => {
  const store = memory()
  assert.equal(First.voiceStartShown(store), false)
  First.setVoiceStartShown(true, store)
  assert.equal(First.voiceStartShown(store), true)
  First.setVoiceStartShown(false, store)
  assert.equal(First.voiceStartShown(store), false)
})

test("a new machine shows roles only after five successful starts", () => {
  const store = memory()
  First.seedSessionsStarted(false, store)
  for (let i = 0; i < First.ROLES_AFTER_STARTS - 1; i++) {
    First.countSessionStarted(store)
    assert.equal(First.sessionRolesShown(store), false, `after ${i + 1} starts`)
  }
  First.countSessionStarted(store)
  assert.equal(First.sessionRolesShown(store), true)
})

test("a person upgrading with Sessions already running keeps the role row", () => {
  const store = memory()
  First.seedSessionsStarted(true, store)
  assert.equal(First.sessionRolesShown(store), true)
  // The first decision stands: a later empty list does not take roles away.
  First.seedSessionsStarted(false, store)
  assert.equal(First.sessionRolesShown(store), true)
})

test("the Settings choice overrides the automatic rule both ways", () => {
  const fresh = memory()
  First.seedSessionsStarted(false, fresh)
  First.setRolesMode("show", fresh)
  assert.equal(First.sessionRolesShown(fresh), true)
  const seasoned = memory()
  First.seedSessionsStarted(true, seasoned)
  First.setRolesMode("hide", seasoned)
  assert.equal(First.sessionRolesShown(seasoned), false)
  assert.equal(First.rolesMode(memory({ [First.SESSION_ROLES_KEY]: "nonsense" })), "auto")
})

test("a refused or unreadable store falls back to the defaults", () => {
  const refusing = { getItem: () => { throw new Error("blocked") }, setItem: () => { throw new Error("blocked") } }
  assert.equal(First.voiceStartShown(refusing), false)
  assert.equal(First.sessionRolesShown(refusing), false)
  assert.equal(First.setVoiceStartShown(true, refusing), false)
  assert.equal(First.sessionsStarted(memory({ [First.SESSIONS_STARTED_KEY]: "-3" })), null)
})

test("the tour is done once finished, and Project presence follows the last count", () => {
  const store = memory()
  assert.equal(First.firstTourDone(store), false)
  First.finishFirstTour(store)
  assert.equal(First.firstTourDone(store), true)
  First.notePlaces(0)
  assert.equal(First.placePresence(), "none")
  First.notePlaces(2)
  assert.equal(First.placePresence(), "some")
  First.notePlaces(null)
  assert.equal(First.placePresence(), "unknown")
})
