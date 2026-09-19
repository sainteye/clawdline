// A question's name, as the page computes it: `node --test web/console/src/session/*.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
import { createHash } from "node:crypto"
import type { SessionMenu } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { menuFingerprint, sha256Hex } from "./fingerprint.ts"

// The same menu `internal/domain/session/fingerprint_test.go` hashes, as the
// page is sent it (`wireMenu`), and the same hex. If either end changes its
// canonical form alone, every answer stops matching every question.
const VECTOR: SessionMenu = {
  question: "下午想喝什麼？ Bash command rm -rf build",
  options: [
    { n: 1, label: "Yes", can: true, selected: true, detail: "a note" },
    { n: 2, label: "Yes, and don't ask again", can: true, selected: false, checked: true },
    { n: 3, label: "No, and tell Claude what to do differently", can: true, selected: false },
  ],
  selected: 1,
  submit: { label: "Submit", selected: false },
  steps: [
    { label: "飲料", done: true, answer: "Tea" },
    { label: "點心", done: false },
  ],
}
const VECTOR_HEX = "8eca80fffc9359d5f0fca31f3e36b741bd50218b658fc086f05931747bd4c5ce"

test("the page names a question with the same hex the Mac computes", () => {
  assert.equal(menuFingerprint(VECTOR), VECTOR_HEX)
})

test("sha256Hex is SHA-256, including across a block boundary and in non-Latin text", () => {
  for (const text of ["", "abc", "a".repeat(55), "a".repeat(56), "a".repeat(64), "a".repeat(1000), "問題 ❯ 1. Yes\u0000"]) {
    const want = createHash("sha256").update(Buffer.from(text, "utf8")).digest("hex")
    assert.equal(sha256Hex(new TextEncoder().encode(text)), want, JSON.stringify(text.slice(0, 20)))
  }
})

test("the caret, a tick, a row's note and the button are not the question", () => {
  const moved: SessionMenu = {
    ...VECTOR,
    selected: 3,
    options: VECTOR.options.map((o) => ({ ...o, selected: o.n === 3, checked: o.n === 2 ? false : o.checked, detail: "other" })),
    submit: { label: "Submit", selected: true },
    steps: VECTOR.steps!.map((s) => ({ ...s, answer: "Cake" })),
  }
  assert.equal(menuFingerprint(moved), VECTOR_HEX)
})

test("the prose, a row's words or number, a row more or less, and a set's progress each rename it", () => {
  const variants: SessionMenu[] = [
    { ...VECTOR, question: "下午想喝什麼？ Bash command rm -rf dist" },
    { ...VECTOR, options: VECTOR.options.map((o) => (o.n === 2 ? { ...o, label: o.label + " for rm" } : o)) },
    { ...VECTOR, options: VECTOR.options.map((o) => (o.n === 3 ? { ...o, n: 4 } : o)) },
    { ...VECTOR, options: VECTOR.options.slice(0, 2) },
    { ...VECTOR, steps: [VECTOR.steps![0], { label: "點心", done: true }] },
    { ...VECTOR, steps: undefined },
  ]
  for (const v of variants) assert.notEqual(menuFingerprint(v), VECTOR_HEX)
  // An absent question is the empty question, as `omitempty` sends it.
  const { question: _q, ...bare } = VECTOR
  assert.equal(menuFingerprint(bare as SessionMenu), menuFingerprint({ ...VECTOR, question: "" }))
})
