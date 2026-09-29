import assert from "node:assert/strict"
import test from "node:test"
import type { TerminalFrame, TerminalModes } from "@clawdline/contract"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { cursorStyle, frameBytes, modeBytes } from "./frame-writer.ts"

const ESC = "\x1b"
const modes: TerminalModes = { app_cursor: false, app_keypad: false, mouse: "none", mouse_sgr: false, alt: false }
const frame = (over: Partial<TerminalFrame> = {}): TerminalFrame => ({
  rev: "1", at: 0, cols: 5, rows: 2,
  cursor: { x: 2, y: 1, visible: true },
  modes, lines: ["abcde", "\x1b[31mx\x1b[0m"],
  ...over,
})

test("every row is placed and cleared in place, never with a newline", () => {
  const out = frameBytes(frame())
  assert.ok(!out.includes("\n") && !out.includes("\r"))
  assert.ok(out.includes(ESC + "[1;1H" + ESC + "[0m" + ESC + "[K" + "abcde"))
  assert.ok(out.includes(ESC + "[2;1H" + ESC + "[0m" + ESC + "[K" + "\x1b[31mx\x1b[0m"))
  // The clear comes before a full row, so its last cell is not erased.
  assert.ok(!out.includes("abcde" + ESC + "[0m" + ESC + "[K"))
})

test("the modes come first and the cursor last", () => {
  const out = frameBytes(frame({ modes: { ...modes, app_cursor: true, mouse: "button", mouse_sgr: true } }))
  assert.ok(out.startsWith(ESC + "[?25l" + ESC + "[?1h" + ESC + ">"))
  assert.ok(out.indexOf(ESC + "[?1002h") < out.indexOf(ESC + "[1;1H"))
  assert.ok(out.includes(ESC + "[?1006h"))
  assert.ok(out.endsWith(ESC + "[2;3H" + ESC + "[?25h"))
  assert.ok(frameBytes(frame({ cursor: { x: 0, y: 0, visible: false } })).endsWith(ESC + "[1;1H"))
})

test("the keypad, mouse and cursor shape follow the program", () => {
  assert.ok(modeBytes({ ...modes, app_keypad: true }, {}).includes(ESC + "="))
  assert.ok(modeBytes({ ...modes, mouse: "any" }, {}).includes(ESC + "[?1003h"))
  assert.ok(modeBytes({ ...modes, mouse: "standard" }, {}).includes(ESC + "[?1000h"))
  assert.equal(cursorStyle({}), 1)
  assert.equal(cursorStyle({ shape: "bar", blinking: false }), 6)
  assert.equal(cursorStyle({ shape: "underline", blinking: true }), 3)
})

test("a cursor outside the screen is kept on it", () => {
  assert.ok(frameBytes(frame({ cursor: { x: 99, y: 99, visible: true } })).endsWith(ESC + "[2;5H" + ESC + "[?25h"))
})
