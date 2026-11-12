import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner needs source extensions.
import { DEMO_FILMS, demoFilmFor } from "./copilot-demo-films.ts"

test("the demo film follows the console's language and the screen's width", () => {
  assert.equal(demoFilmFor("zh-Hant", false), DEMO_FILMS.desktop["zh-Hant"])
  assert.equal(demoFilmFor("en", false), DEMO_FILMS.desktop.en)
  assert.equal(demoFilmFor("zh-Hant", true), DEMO_FILMS.phone["zh-Hant"])
  assert.equal(demoFilmFor("ja", true), DEMO_FILMS.phone.en)
  assert.match(DEMO_FILMS.phone.en!.video, /-phone-/)
  for (const film of [...Object.values(DEMO_FILMS.desktop), ...Object.values(DEMO_FILMS.phone)]) {
    assert.match(film!.video, /^https:\/\/media\.clawdline\.com\/voice-copilot\/demo-[a-zA-Z-]+-[0-9a-f]{12}\.mp4$/)
    assert.match(film!.poster!, /^https:\/\/media\.clawdline\.com\/voice-copilot\/poster-/)
  }
})
