// Previous and next in the picture preview: `node --test web/console/src/session/lightbox-steps.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `session/order.test.ts`.
import { attachLightboxSteps, shareImageFromClick, saveImageFromClick, stepFrom, swipeStep } from "./lightbox-steps.ts"

test("a step goes to the neighbour and stops at either end", () => {
  const list = ["a", "b", "c"]
  assert.equal(stepFrom(list, "b", 1), "c")
  assert.equal(stepFrom(list, "b", -1), "a")
  assert.equal(stepFrom(list, "a", -1), null)
  assert.equal(stepFrom(list, "c", 1), null)
})

// A tile that expired while the preview was open is no longer in the list of
// live tiles; stepping from it must not jump to the first picture.
test("a picture no longer in the list has no neighbours", () => {
  assert.equal(stepFrom(["a", "c"], "b", 1), null)
})

test("a finger moving left is the next picture, right the previous", () => {
  assert.equal(swipeStep(-80, 5), 1)
  assert.equal(swipeStep(80, -5), -1)
})

// Reading down a long screenshot is a mostly vertical drag; it must never
// change pictures, and neither must a short horizontal wobble.
test("a scroll or a wobble is not a swipe", () => {
  assert.equal(swipeStep(-60, 200), 0)
  assert.equal(swipeStep(-30, 0), 0)
})

test("a share press gives the native sheet the original image file immediately", () => {
  const file = new File([new Uint8Array([137, 80, 78, 71])], "clawdline-image.png", { type: "image/png" })
  const calls: unknown[] = []
  const platform = {
    share(data: ShareData) { calls.push(["share", data.files]); return Promise.resolve() },
  }
  shareImageFromClick(file, platform)
  assert.deepEqual(calls, [["share", [file]]])
  assert.equal(file.type, "image/png")
  assert.equal(file.size, 4)
})

test("a browser without file sharing keeps its download action", () => {
  const file = new File(["png"], "clawdline-image.png", { type: "image/png" })
  let downloads = 0
  const platform = {}
  assert.equal(saveImageFromClick(file, platform, () => { downloads++ }), "download")
  assert.equal(downloads, 1)
})

test("a desktop with file sharing saves directly instead of opening the share sheet", () => {
  const file = new File(["png"], "picture.png", { type: "image/png" })
  let downloads = 0
  const platform = {
    showSaveFilePicker: undefined,
    share: () => { assert.fail("download must not share"); return Promise.resolve() },
    canShare: () => true,
  }
  assert.equal(saveImageFromClick(file, platform, () => { downloads++ }), "download")
  assert.equal(downloads, 1)
})

test("a desktop save picker receives the filename and writes the original bytes", async () => {
  const file = new File(["original bytes"], "picture.png", { type: "image/png" })
  const calls: unknown[] = []
  const platform = {
    showSaveFilePicker: async (options: { suggestedName: string }) => {
      calls.push(options)
      return { createWritable: async () => ({
        write: async (bytes: Blob) => { calls.push(bytes) },
        close: async () => { calls.push("closed") },
      }) }
    },
  }
  assert.equal(saveImageFromClick(file, platform, () => assert.fail("unexpected download")), "picker")
  await new Promise(resolve => setImmediate(resolve))
  assert.deepEqual(calls, [{ suggestedName: "picture.png" }, file, "closed"])
})

test("cancelling the save picker does not download; an unavailable picker falls back", async () => {
  const file = new File(["png"], "picture.png")
  for (const name of ["AbortError", "SecurityError"]) {
    let downloads = 0
    const platform = {
      showSaveFilePicker: () => Promise.reject(new DOMException("unavailable", name)),
    }
    saveImageFromClick(file, platform, () => { downloads++ })
    await new Promise(resolve => setImmediate(resolve))
    assert.equal(downloads, name === "AbortError" ? 0 : 1)
  }
})

test("cancelling sharing is handled without starting a download", async () => {
  const file = new File(["image"], "picture.png")
  shareImageFromClick(file, { share: () => Promise.reject(new DOMException("cancelled", "AbortError")) })
  await new Promise(resolve => setImmediate(resolve))
})

// Exercise the preview's actual controls, including loading and capability changes.
class PreviewElement extends EventTarget {
  children: PreviewElement[] = []
  dataset: Record<string, string> = {}
  className = ""
  hidden = false
  disabled = false
  src = "data:image/png;base64,aW1hZ2U="
  setAttribute() {}
  appendChild(child: PreviewElement) { this.children.push(child); return child }
  remove() {}
  querySelector() { return null }
  closest() { return null }
  querySelectorAll() { return [] }
  click() { this.dispatchEvent(new Event("click")) }
}

test("desktop previews download even with sharing support; touch previews keep native sharing", async (t) => {
  const shared: ShareData[] = []
  let supportsSharing = true
  let touchInterface = false
  const downloads: PreviewElement[] = []
  const saved: Blob[] = []
  const revoked: string[] = []
  t.mock.method(URL, "createObjectURL", (blob: Blob) => {
    saved.push(blob)
    return `blob:download-${saved.length}`
  })
  t.mock.method(URL, "revokeObjectURL", (url: string) => { revoked.push(url) })
  t.mock.method(globalThis, "fetch", async () => ({
    ok: true, blob: async () => new Blob(["original image"], { type: "image/png" }),
  }) as Response)
  t.mock.getter(globalThis, "navigator", () => ({
    share: (data: ShareData) => { shared.push(data); return Promise.resolve() },
    canShare: () => supportsSharing,
  }) as Navigator)
  const dialog = new PreviewElement()
  const doc = Object.assign(new PreviewElement(), {
    body: new PreviewElement(),
    createElement: (tag: string) => {
      const element = new PreviewElement()
      if (tag === "a") element.addEventListener("click", () => downloads.push(element))
      return element
    },
    defaultView: { matchMedia: () => ({ matches: touchInterface }) },
  })
  const preview = attachLightboxSteps(dialog as unknown as HTMLElement, new PreviewElement() as unknown as HTMLElement, {
    previous: "Previous", next: "Next", download: "Download", share: "Share", position: () => "",
  }, doc as unknown as Document)
  const share = dialog.children.find(child => child.className === "image-lightbox-share")!
  const download = dialog.children.find(child => child.className === "image-lightbox-download")!
  const trigger = new PreviewElement() as unknown as Element
  for (const [touch, supported] of [[false, true], [true, true], [true, false], [false, false], [true, true]]) {
    touchInterface = touch
    supportsSharing = supported
    const canShare = touch && supported
    preview.opened(trigger, "picture.png")
    assert.equal(share.hidden, true)
    assert.equal(download.hidden, true)
    await new Promise(resolve => setImmediate(resolve))
    assert.equal(share.hidden, !canShare)
    assert.equal(download.hidden, canShare)
    if (canShare) share.click()
    else download.click()
  }
  assert.equal(downloads.length, 3)
  for (const link of downloads) {
    assert.equal((link as unknown as HTMLAnchorElement).download, "picture.png")
    assert.match((link as unknown as HTMLAnchorElement).href, /^blob:download-/)
  }
  assert.equal(saved.length, 3)
  for (const blob of saved) assert.equal(await blob.text(), "original image")
  await new Promise(resolve => setTimeout(resolve, 10))
  assert.equal(revoked.length, 3)
  assert.equal(shared.length, 2)
  for (const data of shared) {
    assert.equal(data.files?.[0].name, "picture.png")
    assert.equal(await data.files?.[0].text(), "original image")
  }
})

test("reading a pannable screenshot below 100% does not swipe to its neighbour", async (t) => {
  t.mock.method(globalThis, "fetch", async () => ({
    ok: true, blob: async () => new Blob(["image"], { type: "image/png" }),
  }) as Response)
  const dialog = new PreviewElement()
  const image = new PreviewElement()
  image.dataset.zoomed = "false"
  const current = new PreviewElement()
  let steps = 0
  const next = new PreviewElement()
  next.click = () => { steps++ }
  const doc = Object.assign(new PreviewElement(), {
    createElement: () => new PreviewElement(),
    querySelectorAll: () => [current, next],
    defaultView: { matchMedia: () => ({ matches: false }) },
  })
  const preview = attachLightboxSteps(dialog as unknown as HTMLElement, image as unknown as HTMLElement, {
    previous: "Previous", next: "Next", download: "Download", share: "Share", position: (i, n) => `${i}/${n}`,
  }, doc as unknown as Document)
  preview.opened(current as unknown as Element, "picture.png")
  const swipe = () => {
    const start = new Event("touchstart")
    Object.assign(start, { touches: [{ clientX: 200, clientY: 300 }] })
    dialog.dispatchEvent(start)
    const end = new Event("touchend")
    Object.assign(end, { touches: [], changedTouches: [{ clientX: 100, clientY: 300 }] })
    dialog.dispatchEvent(end)
  }
  image.dataset.pannable = "true"
  swipe()
  assert.equal(steps, 0)
  image.dataset.pannable = "false"
  swipe()
  assert.equal(steps, 1)
  await new Promise(resolve => setImmediate(resolve))
})
