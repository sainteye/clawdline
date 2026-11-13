// The tag over the composer after a cleaned-up dictation:
// `node --test web/console/src/session/voice-polish.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { dictatedFrom, polishWord, swapDictation } from "./voice-polish.ts"

const raw = "好那就先這樣把我等一下再看。"
const polished = "好，那就先這樣，我等一下再看。"

test("a dictation with cleanup off, or left unchanged, shows no tag", () => {
  assert.equal(dictatedFrom(raw), null)
  assert.equal(dictatedFrom(raw, { status: "unchanged" }), null)
  assert.equal(dictatedFrom(raw, { status: "applied" }), null, "applied without the original has nothing to offer back")
})

test("a fallback says why the original was kept and offers no swap", () => {
  const d = dictatedFrom(raw, { status: "timeout" })
  assert.deepEqual(d, { status: "timeout", showing: "polished" })
  assert.equal(swapDictation("前面的字 " + raw, d), null)
  const said = polishWord(d)
  assert.ok(said && !said.startsWith("voicePolish."), said)
  assert.notEqual(polishWord({ status: "something-new", showing: "polished" }), "", "an unknown status still says something")
})

test("the swap replaces the dictated words and comes back", () => {
  const d = dictatedFrom(polished, { status: "applied", raw })
  assert.ok(d)
  const typed = "我先打的字。" + polished
  const once = swapDictation(typed, d)
  assert.deepEqual(once?.text, "我先打的字。" + raw)
  assert.equal(once?.dictated.showing, "raw")
  assert.notEqual(polishWord(once!.dictated), polishWord(d))
  const back = swapDictation(once!.text, once!.dictated)
  assert.equal(back?.text, typed)
  assert.equal(back?.dictated.showing, "polished")
})

test("a box edited past the dictated words is left alone", () => {
  const d = dictatedFrom(polished, { status: "applied", raw })!
  assert.equal(swapDictation("好，那就先這樣，我晚點再看。", d), null)
})

test("the last occurrence is swapped, because dictation appends", () => {
  const d = dictatedFrom(polished, { status: "applied", raw })!
  const box = polished + "\n" + polished
  assert.equal(swapDictation(box, d)?.text, polished + "\n" + raw)
})

test("a dictation sent with the paper plane carries the pictures already in the box", async () => {
  // @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
  const { dictationPictures } = await import("./voice-polish.ts")
  const urls = ["data:image/png;base64,AAAA", "data:image/jpeg;base64,BBBB"]
  assert.deepEqual(dictationPictures({ here: true, allowed: true, busy: false, urls }), { pictures: urls, hold: false })
  assert.deepEqual(dictationPictures({ here: true, allowed: true, busy: true, urls }), { pictures: [], hold: true },
    "a picture still shrinking keeps the message in the box instead of sending it without the picture")
  assert.deepEqual(dictationPictures({ here: true, allowed: false, busy: false, urls }), { pictures: [], hold: false })
  assert.deepEqual(dictationPictures({ here: false, allowed: true, busy: false, urls }), { pictures: [], hold: false })
})
