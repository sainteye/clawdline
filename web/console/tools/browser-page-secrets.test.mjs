import { test } from "node:test"
import assert from "node:assert/strict"
import { pathToFileURL } from "node:url"
const { readPage, locate, editableRects, focusedEditableKind } = await import(process.env.BROWSER_PAGE_MODULE ? pathToFileURL(process.env.BROWSER_PAGE_MODULE).href : "../../../extension/chrome/page.js")

test("page reads and element targeting use labels without retaining field values", () => {
  const field = (type, value, placeholder) => ({
    tagName: "INPUT", type, value, placeholder, innerText: "", name: "",
    getAttribute: () => null, getBoundingClientRect: () => ({ width: 100, height: 20, left: 0, top: 0 }), scrollIntoView() {},
  })
  const elements = [field("password", "synthetic-password", "Password"), field("text", "synthetic-otp", "Code"), field("submit", "Continue", "")]
  globalThis.document = { querySelectorAll: () => elements, body: { innerText: "Sign in" }, title: "Sign in" }
  globalThis.location = { href: "https://example.com/" }
  globalThis.getComputedStyle = () => ({ visibility: "visible", display: "block" })
  try {
    const page = readPage(1000, 10)
    assert.deepEqual(page.elements.map(e => e.label), ["Password", "Code", "Continue"])
    assert.equal(locate("synthetic-password").found, false)
    assert.equal(locate("Code").name, "Code")
  } finally {
    delete globalThis.document; delete globalThis.location; delete globalThis.getComputedStyle
  }
})

test("keyboard geometry contains only visible editable rectangles and kinds, never values", () => {
  const field = (type, disabled = false) => ({ tagName: "INPUT", type, disabled, readOnly: false,
    get value() { throw new Error("field value was read") },
    getBoundingClientRect: () => ({ left: 20, top: 40, width: 120, height: 44 }),
  })
  const password = field("password"), inputs = [password, field("text"), field("submit"), field("text", true)]
  globalThis.document = { querySelectorAll: () => inputs, activeElement: password }
  globalThis.getComputedStyle = () => ({ visibility: "visible", display: "block" })
  try {
    assert.deepEqual(editableRects(1), [{ x: 20, y: 40, width: 120, height: 44, kind: "password" }])
    assert.equal(editableRects(10).length, 2)
    assert.equal(focusedEditableKind(), "password")
  } finally { delete globalThis.document; delete globalThis.getComputedStyle }
})
