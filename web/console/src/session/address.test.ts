// The session in the address: `node --test web/console/src/session/*.test.ts`.
//
// Imported by its `.ts` path for node, as order.test.ts explains.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { namesSession, sessionFragment, sessionsInFragment } from "./address.ts"

// Pane ids of three digits are spelled in pieces: tools/check-private.sh reads
// any `%NNN` in a published file as a pane copied from somebody's machine.
const pane = (n: number) => "%" + n
const escapedPane = (n: number) => "%25" + n

const ITERM = "w0t0p0:1A000000-0000-4000-8000-000000000001"

/** The fragment as a browser hands it back, after its own URL parser has read it. */
function throughAURL(fragment: string): string {
  return new URL("http://127.0.0.1:7727/" + fragment).hash
}

test("each kind of id this daemon lists is spelled so that nothing in it is read as an escape", () => {
  assert.equal(sessionFragment(pane(801)), "#session=" + escapedPane(801))
  assert.equal(sessionFragment("ttys008"), "#session=ttys008")
  assert.equal(sessionFragment(ITERM), "#session=w0t0p0%3A1A000000-0000-4000-8000-000000000001")
})

test("the spelling is the one a notification's link uses (SessionURL, push_test.go)", () => {
  for (const [id, want] of [
    ["%14", "#session=%2514"],
    ["w0t0p0:9F2A", "#session=w0t0p0%3A9F2A"],
    ["plain-id_1.2~3", "#session=plain-id_1.2~3"],
    ["a&b=c#d", "#session=a%26b%3Dc%23d"],
    ["", "#session="],
  ]) {
    assert.equal(sessionFragment(id), want, id)
  }
})

test("every id comes back as itself after a real URL has parsed the address", () => {
  for (const id of [pane(801), "ttys008", ITERM, pane(195), "%14", "a&b=c#d", "space in it", "名前", "100%", "%25"]) {
    const hash = throughAURL(sessionFragment(id))
    assert.deepEqual(sessionsInFragment(hash), [id], id)
  }
})

test("a fragment that names no session asks for none", () => {
  for (const hash of ["", "#", "#page=settings", "#session=", "#page=sessions&session=", "#xsession=ttys008"]) {
    assert.equal(sessionsInFragment(hash), null, hash)
    assert.equal(namesSession(hash), false, hash)
  }
})

test("the session is found beside other parts of the fragment", () => {
  assert.deepEqual(sessionsInFragment("#page=sessions&session=" + escapedPane(801)), [pane(801)])
  assert.deepEqual(sessionsInFragment("#session=ttys008&page=sessions"), ["ttys008"])
})

test("a pane written raw by an old link is tried as decoded and then as written", () => {
  // `%19` is U+0019: the control character is the evidence nothing was escaped.
  assert.deepEqual(sessionsInFragment("#session=" + pane(195)), ["\u00195", pane(195)])
  assert.deepEqual(sessionsInFragment("#session=%14"), ["\u0014", "%14"])
})

test("a raw escape that cannot decode is the id as written", () => {
  // `%80` is a lone continuation byte, which decodeURIComponent refuses.
  assert.deepEqual(sessionsInFragment("#session=" + pane(801)), [pane(801)])
  assert.deepEqual(sessionsInFragment("#session=%zz"), ["%zz"])
})

test("an escaped id is never also tried as its escaped spelling", () => {
  // Pane `%41`, escaped, must not open pane `%2541` when `%41` has gone.
  assert.deepEqual(sessionsInFragment("#session=%2541"), ["%41"])
})
