// Links to this console, pressed in a Home Screen window:
// `node --test web/console/src/same-page-links.test.ts`.
//
// The press and the window are stand-ins with the fields the rule reads. The
// whole-page behaviour — a real `target="_blank"` link in a real standalone
// window, before and after — is driven in a browser (see the task report); this
// pins which links the rule may touch and which it must leave alone.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see cloud/forget.test.ts.
import { followInPlace, samePageTarget } from "./same-page-links.ts"

const HERE = { href: "https://app.clawdline.com/#page=sessions", origin: "https://app.clawdline.com", pathname: "/" }

function anchor(href: string, target = "_blank", download = false) {
  const attributes: Record<string, string> = { href, target }
  const node = {
    getAttribute: (name: string) => attributes[name] ?? null,
    hasAttribute: (name: string) => name === "download" ? download : name in attributes,
    closest: (selector: string) => (selector === "a[href]" ? node : null),
  }
  return node
}

function press(target: unknown, extra: Partial<{ button: number; metaKey: boolean; ctrlKey: boolean; shiftKey: boolean; altKey: boolean; defaultPrevented: boolean }> = {}) {
  const event = {
    defaultPrevented: false,
    button: 0,
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    target,
    prevented: false,
    preventDefault() {
      event.prevented = true
    },
    ...extra,
  }
  return event
}

function window(standalone: boolean) {
  const went: string[] = []
  return {
    location: { ...HERE, assign: (url: string) => void went.push(url) },
    navigator: { standalone: false },
    matchMedia: (query: string) => ({ matches: standalone && query === "(display-mode: standalone)" }),
    went,
  }
}

test("in a standalone window, a machine's pairing link on this page is followed here", () => {
  const scope = window(true)
  const event = press(anchor("https://app.clawdline.com/#pair=eyJ2IjoxfQ"))
  assert.equal(followInPlace(event, scope), true)
  assert.equal(event.prevented, true)
  assert.deepEqual(scope.went, ["https://app.clawdline.com/#pair=eyJ2IjoxfQ"])
})

test("iOS says standalone through navigator.standalone, and that counts too", () => {
  const scope = { ...window(false), navigator: { standalone: true } }
  assert.equal(followInPlace(press(anchor("/#page=devices")), scope), true)
  assert.deepEqual(scope.went, ["https://app.clawdline.com/#page=devices"])
})

test("in a browser tab the new tab keeps the same storage, so _blank is left as it is", () => {
  const scope = window(false)
  const event = press(anchor("https://app.clawdline.com/#pair=eyJ2IjoxfQ"))
  assert.equal(followInPlace(event, scope), false)
  assert.equal(event.prevented, false)
  assert.deepEqual(scope.went, [])
})

test("another site, another path on this origin, a blob and a download all keep their new window", () => {
  const scope = window(true)
  for (const href of [
    "https://example.com/elsewhere",
    "https://api.clawdline.com/#pair=x",
    "https://app.clawdline.com/app/build/icon-512.png",
    "https://app.clawdline.com/v1/sessions/%251/image",
    "blob:https://app.clawdline.com/8f1c",
    "javascript:alert(1)",
  ]) {
    const event = press(anchor(href))
    assert.equal(followInPlace(event, scope), false, href)
    assert.equal(event.prevented, false, href)
  }
  assert.equal(followInPlace(press(anchor("/#page=devices", "_blank", true)), scope), false)
  assert.deepEqual(scope.went, [])
})

test("a modifier, another button, or a renderer that already handled it keeps its own meaning", () => {
  const scope = window(true)
  const link = anchor("/#pair=x")
  assert.equal(followInPlace(press(link, { metaKey: true }), scope), false)
  assert.equal(followInPlace(press(link, { ctrlKey: true }), scope), false)
  assert.equal(followInPlace(press(link, { shiftKey: true }), scope), false)
  assert.equal(followInPlace(press(link, { button: 1 }), scope), false)
  assert.equal(followInPlace(press(link, { defaultPrevented: true }), scope), false)
  assert.equal(followInPlace(press(anchor("/#pair=x", "")), scope), false, "no target: already in place")
  assert.equal(followInPlace(press({}), scope), false, "not inside a link")
  assert.deepEqual(scope.went, [])
})

test("the same page is the same origin and the same path; the fragment and the query may differ", () => {
  assert.equal(samePageTarget("?x=1#page=devices", HERE)?.href, "https://app.clawdline.com/?x=1#page=devices")
  assert.equal(samePageTarget("http://app.clawdline.com/#pair=x", HERE), null, "another scheme is another origin")
  assert.equal(samePageTarget("/index.html#pair=x", HERE), null)
  assert.equal(samePageTarget("", HERE), null)
  assert.equal(samePageTarget(null, HERE), null)
})
