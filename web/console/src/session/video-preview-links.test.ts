import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's source test runner needs the TypeScript extension.
import { videoLocatorFromHash, videoLocatorFromLink } from "./video-preview-links.ts"
import { documentHash } from "../legacy/js/net/document-links.js"

const locator = { machine: "machine-a", session: "session-a", scope: "project", path: "clip.mp4" }
const base = "https://app.clawdline.com/#session=session-a"
const address = "https://app.clawdline.com/" + documentHash(locator)

test("existing MP4 publication links open the same exact video identity", () => {
  assert.deepEqual(videoLocatorFromLink(address, base), locator)
  assert.deepEqual(videoLocatorFromLink(documentHash(locator), base), locator)
  const task = { ...locator, scope: "task", task: "10000000-0000-4000-8000-000000000001", path: "out/clip.MP4" }
  assert.deepEqual(videoLocatorFromHash(documentHash(task)), task)
})

test("external URLs, documents and incomplete or ambiguous locators cannot launch media reads", () => {
  for (const url of [address.replace("app.clawdline.com", "other.example"),
    address.replace("/#", "/other#"), address.replace("/#", "/?token=secret#"),
    address.replace("clip.mp4", "report.html"), address.replace("clip.mp4", "../clip.mp4"),
    address + "&machine=other", address + "&extra=1", address.replace("scope=project&", ""),
    "javascript:alert(1)", "data:video/mp4,AAAA", "blob:https://app.clawdline.com/clip.mp4"]) {
    assert.equal(videoLocatorFromLink(url, base), null, url)
  }
})
