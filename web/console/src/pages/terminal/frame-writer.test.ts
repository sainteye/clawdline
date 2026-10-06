import assert from "node:assert/strict"
import test from "node:test"
import type { TerminalFrame, TerminalModes } from "@clawdline/contract"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { cursorStyle, frameBytes, frameDeltaBytes, modeBytes } from "./frame-writer.ts"

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
  assert.ok(out.startsWith(ESC + "[?25l" + ESC + "[?7l" + ESC + "[?1h" + ESC + ">"))
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

test("a row wider than the screen cannot wrap and scroll the screen under a later delta", () => {
  const wide = frame({ lines: ["abcde", "vwxyz12345"] })
  for (const out of [frameBytes(wide), frameDeltaBytes(frame(), wide)]) {
    assert.ok(out.indexOf(ESC + "[?7l") >= 0 && out.indexOf(ESC + "[?7l") < out.indexOf("vwxyz"))
    assert.ok(!out.includes(ESC + "[?7h"))
  }
})

test("a changed row is redrawn without rewriting an unchanged row", () => {
  const next = frame({ rev: "2", lines: ["abcde", "xy"] })
  const out = frameDeltaBytes(frame(), next)
  assert.ok(!out.includes("abcde"))
  assert.ok(!out.includes(ESC + "[1;1H"))
  assert.ok(out.includes(ESC + "[2;1H" + ESC + "[0m" + ESC + "[Kxy"))
  assert.ok(out.endsWith(ESC + "[2;3H" + ESC + "[?25h"))
})

test("a resize or alternate screen change redraws the complete frame", () => {
  const prior = frame()
  const resized = frame({ cols: 6, rev: "2" })
  assert.equal(frameDeltaBytes(prior, resized), frameBytes(resized))
  const alt = frame({ modes: { ...modes, alt: true }, rev: "2" })
  assert.equal(frameDeltaBytes(prior, alt), frameBytes(alt))
})
