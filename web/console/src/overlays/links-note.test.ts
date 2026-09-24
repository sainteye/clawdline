// The sentence under the Links section:
// `node --test --experimental-strip-types web/console/src/overlays/links-note.test.ts`
//
// The language is pinned rather than inherited. `next-strings.ts` reads
// `document.documentElement.lang` first and `navigator.language` second, and
// node has had a `navigator` since v21 — so a run on this machine answered in
// Traditional Chinese while the assertions had been written in English. A
// stand-in document decides it here, and both catalogs are read, because the
// person these sentences are for reads the second one.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `session/order.test.ts`.
import { deployQuietNote, deployWhyWord, repositoryNote } from "./links-note.ts"
// @ts-expect-error -- a `.ts` path, for node; see `legacy/devices-bridge.test.ts`.
import { nextWord } from "../next-strings.ts"

const lang = { documentElement: { lang: "en" } }
;(globalThis as { document?: unknown }).document = lang

/** The real catalog, and a clock that says exactly what it was handed. */
const say = { word: nextWord, clock: (unix: number) => "at " + unix }

/** Run a body against each language the catalog has. */
function inBoth(body: (tongue: string) => void): void {
  for (const tongue of ["en", "zh-Hant"]) {
    lang.documentElement.lang = tongue
    body(tongue)
  }
  lang.documentElement.lang = "en"
}

// The bug, in one assertion. A repository on GitHub, a git that answered, and
// a workflow poller with nothing to show used to fall off the end of the
// switch and return "" — so the file's own `{"why":"stale-fail"}` reached the
// screen as a blank cell. Asked about twice on 2026-09-21.
test("a repository on GitHub with no deploy row is not silence any more", () => {
  const quiet = { kind: "state_not_drawn" as const, state: "none", why: "stale-fail", updatedAt: 1789955841 }
  inBoth((tongue) => {
    assert.equal(repositoryNote("github", undefined, undefined, say), "",
      tongue + ": a drawn deploy row explains itself; there is nothing to add")
    const said = repositoryNote("github", undefined, quiet, say)
    assert.notEqual(said, "", tongue + ": the file said why, and the screen has to say it too")
    assert.match(said, /none/, tongue + ": the producer's own state word is in it")
    assert.match(said, /at 1789955841/,
      tongue + ": with when that tool decided it, which is how a dead poller shows")
    assert.doesNotMatch(said, /\{\w+\}/, tongue + ": no hole is left unfilled")
  })
  lang.documentElement.lang = "en"
  assert.match(repositoryNote("github", undefined, quiet, say), /stopped counting as the current state/)
  lang.documentElement.lang = "zh-Hant"
  assert.match(repositoryNote("github", undefined, quiet, say), /久到不再算是現在的狀態/)
  lang.documentElement.lang = "en"
})

// The five kinds are five different things to do, which is the whole reason
// they are five words and not one absent row.
test("each kind of nothing has its own sentence, and no two are the same", () => {
  inBoth((tongue) => {
    const said = [
      deployQuietNote({ kind: "no_file" }, say),
      deployQuietNote({ kind: "unreadable" }, say),
      deployQuietNote({ kind: "state_not_drawn", state: "none", why: "no-runs" }, say),
      deployQuietNote({ kind: "no_address", state: "ok" }, say),
      deployQuietNote({ kind: "running_stale", state: "running", updatedAt: 1790214000 }, say),
    ]
    for (const line of said) {
      assert.notEqual(line, "", tongue + ": every kind says something")
      assert.doesNotMatch(line, /\{\w+\}/, tongue + ": and leaves no hole unfilled")
    }
    assert.equal(new Set(said).size, said.length, tongue + ": and no two of them say the same thing")
    assert.equal(deployQuietNote(undefined, say), "", tongue + ": a row that was drawn needs no note")
  })
  lang.documentElement.lang = "en"
  assert.match(deployQuietNote({ kind: "no_file" }, say), /nobody is looking/,
    "an absent file is nobody looking, not no run")
  assert.match(deployQuietNote({ kind: "no_address", state: "ok" }, say), /It did not say why\./,
    "a file that gave no reason is told as that")
})

// `gh-run-status.py` writes six of these today. The seventh is the one that
// matters: a list that kept only what it recognised would be a filter wearing
// a translation's clothes, and the blank cell would come back the first time
// that tool learned a word.
test("every reason that tool writes has words, and a new one is said as it was written", () => {
  const known = ["no-gh", "no-branch", "gh-failed", "no-runs", "workflow-disabled", "stale-fail"]
  const strange = "a word nobody has written yet"
  inBoth((tongue) => {
    const said = known.map((why) => deployWhyWord(why, say))
    for (const [i, line] of said.entries()) {
      assert.notEqual(line, "", tongue + ": " + known[i])
      assert.doesNotMatch(line, /\{\w+\}/, tongue + ": " + known[i] + " leaves no hole unfilled")
      assert.doesNotMatch(line, new RegExp(known[i]!), tongue + ": " + known[i] + " is said in words, not echoed")
    }
    assert.equal(new Set(said).size, known.length, tongue + ": six reasons, six sentences")

    const unknown = deployWhyWord(strange, say)
    assert.match(unknown, new RegExp(strange), tongue + ": an unknown reason is quoted, never swallowed")
    assert.notEqual(deployWhyWord("", say), "", tongue + ": no reason at all is still an answer")
    assert.equal(deployWhyWord(undefined, say), deployWhyWord("", say), tongue + ": absent and empty are one case")
  })
  lang.documentElement.lang = "en"
  assert.match(deployWhyWord(strange, say), /does not know/, "and named as one this app does not know")
  assert.match(deployWhyWord("", say), /did not say why/)
  lang.documentElement.lang = "zh-Hant"
  assert.match(deployWhyWord(strange, say), /不認得的理由/)
  lang.documentElement.lang = "en"
})

// The other four repository answers are untouched by all of this; the fifth
// case was added beside them, not over them.
test("the four kinds that were already answered still answer the same way", () => {
  const quiet = { kind: "state_not_drawn" as const, state: "none", why: "stale-fail" }
  inBoth((tongue) => {
    for (const repo of ["no_remote", "not_a_repository", "remote_not_github"] as const) {
      const line = repositoryNote(repo, undefined, quiet, say)
      assert.notEqual(line, "", tongue + ": " + repo)
      assert.doesNotMatch(line, /stale-fail/, tongue + ": " + repo + " does not borrow the GitHub sentence")
    }
    assert.notEqual(repositoryNote("unreadable", "git_timeout", quiet, say), "",
      tongue + ": which way git failed is still said")
    assert.equal(repositoryNote(undefined, undefined, undefined, say), "",
      tongue + ": a session the daemon never placed says nothing, as before")
  })
  lang.documentElement.lang = "en"
  assert.match(repositoryNote("unreadable", "git_timeout", quiet, say), /did not answer in time/)
  assert.match(repositoryNote("no_remote", undefined, quiet, say), /no origin remote/)
})

// A deploy whose producer stopped between `running` and its verdict used to
// be drawn as a bar at 100% for as long as the file stayed. It is not drawn
// now, and the sentence says why and since when — not "no run".
test("a running deploy nobody rewrote says it stopped, with when", () => {
  inBoth((tongue) => {
    const said = deployQuietNote({ kind: "running_stale", state: "running", updatedAt: 1790214000 }, say)
    assert.match(said, /at 1790214000/, tongue + ": when the producer last wrote it")
    assert.doesNotMatch(said, /\{\w+\}/, tongue + ": no hole is left unfilled")
    assert.notEqual(said, deployQuietNote({ kind: "state_not_drawn", state: "running" }, say),
      tongue + ": a stopped producer is not a producer with nothing to show")
  })
})
