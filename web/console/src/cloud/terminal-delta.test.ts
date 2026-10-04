import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- the focused runner bundles TypeScript source directly.
import { reconstructTerminalDelta, terminalScreenHash, type TerminalDelta } from "./terminal-delta.ts"
import type { TerminalFrame } from "@clawdline/contract"

const base: TerminalFrame = { rev: "a", at: Date.now() / 1000, cols: 80, rows: 2, dead: false,
  cursor: { x: 0, y: 0, visible: true },
  modes: { app_cursor: false, app_keypad: false, mouse: "none", mouse_sgr: false, alt: false },
  lines: ["alpha", "beta"] }
async function delta(): Promise<TerminalDelta> {
  const at = Date.now() / 1000
  return { v: 1, type: "terminal_frame_delta", terminal_id: "terminal", connection: "connection",
    frame_seq: 2, base_seq: 1, base_rev: "a", captured_at: at, rev: "b", at,
    cols: 80, rows: 2, dead: false, cursor: base.cursor, modes: base.modes,
    changed_rows: [{ row: 1, line: "gamma" }], screen_hash: await terminalScreenHash(["alpha", "gamma"]) }
}
test("a single ordered row reconstructs and hashes the exact screen", async () => {
  const value = await delta()
  assert.deepEqual((await reconstructTerminalDelta(base, 1, value, "connection", "terminal")).lines, ["alpha", "gamma"])
  assert.equal(await terminalScreenHash(["a", "bc"]), await terminalScreenHash(["a", "bc"]))
  assert.notEqual(await terminalScreenHash(["a", "bc"]), await terminalScreenHash(["ab", "c"]))
})
test("missing sequence, wrong base, rows, hash, alt and time reject before publication", async () => {
  const good = await delta()
  for (const change of [
    { frame_seq: 3 }, { base_seq: 0 }, { base_rev: "other" },
    { changed_rows: [{ row: 1, line: "x" }, { row: 0, line: "y" }] },
    { changed_rows: [{ row: 2, line: "x" }] }, { screen_hash: "0".repeat(64) },
    { modes: { ...base.modes, alt: true } }, { captured_at: good.captured_at - 10 },
  ]) await assert.rejects(reconstructTerminalDelta(base, 1, { ...good, ...change }, "connection", "terminal"), /terminal_delta_mismatch/)
})
