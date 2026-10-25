import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strip-types loads the source extension.
import { matchShortcut, shortcutLabel, shortcutAria, macKeyboard } from "./shortcuts.ts"

const key = (key: string, extra: Partial<KeyboardEvent> = {}) => ({
  key, code: "", metaKey: false, ctrlKey: false, shiftKey: false, altKey: false,
  isComposing: false, defaultPrevented: false, ...extra,
})

test("navigation and help use the current platform's modifier", () => {
  for (const mac of [true, false]) {
    const mod = mac ? { metaKey: true } : { ctrlKey: true }
    for (const [letter, action] of [["k", "sessions"], ["b", "board"], ["\\", "sidebar"], ["/", "help"]]) {
      assert.equal(matchShortcut(key(letter, mod), mac, false, false), action)
      assert.equal(matchShortcut(key(letter, mac ? { ctrlKey: true } : { metaKey: true }), mac, false, false), null)
    }
  }
  assert.equal(shortcutLabel("board", true), "⌘B")
  assert.equal(shortcutLabel("board", false), "Ctrl+B")
  assert.equal(shortcutAria("sessions", false), "Control+k")
  assert.equal(macKeyboard("MacIntel"), true)
  assert.equal(macKeyboard("Linux x86_64"), false)
})

test("common actions stand down for typing and focused controls", () => {
  for (const [letter, action] of [["n", "start"], ["t", "task"], ["v", "voice"], ["c", "compose"], ["a", "attention"]]) {
    assert.equal(matchShortcut(key(letter), true, false, false), action)
    assert.equal(matchShortcut(key(letter), true, true, false), null)
    assert.equal(matchShortcut(key(letter), true, false, true), null)
  }
  assert.equal(matchShortcut(key("b", { metaKey: true }), true, true, false), null, "bold stays in the editor")
  assert.equal(matchShortcut(key("k", { metaKey: true }), true, true, false), "sessions")
})

test("IME, claimed events and extra modifiers cannot trigger an action", () => {
  for (const extra of [{ isComposing: true }, { defaultPrevented: true }, { altKey: true }, { shiftKey: true }, { ctrlKey: true }]) {
    assert.equal(matchShortcut(key("b", { metaKey: true, ...extra }), true, false, false), null)
  }
  assert.equal(matchShortcut(key("?"), true, false, true), "help")
  assert.equal(matchShortcut(key("?"), true, true, false), null)
  assert.equal(matchShortcut(key("?", { shiftKey: true }), true, false, false), "help")
  assert.equal(matchShortcut(key("、", { metaKey: true, code: "Slash" }), true, false, false), "help")
})
