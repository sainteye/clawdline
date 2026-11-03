import assert from "node:assert/strict"
import { test } from "node:test"
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { isModuleLoadError, observeModulePreloadErrors } from "./module-load-error.ts"

test("a stale JavaScript chunk and a removed CSS dependency are recoverable module failures", () => {
  for (const error of [
    new TypeError("Failed to fetch dynamically imported module: https://console.example/assets/UsageOverview-old.js"),
    new TypeError("Importing a module script failed."),
    new TypeError("error loading dynamically imported module: https://console.example/assets/old.js"),
    new Error("Unable to preload CSS for /assets/old.css"),
  ]) assert.equal(isModuleLoadError(error), true, error.message)
})

test("a preload event identifies the original error without turning a prevented import into undefined", () => {
  const target = new EventTarget()
  const dispose = observeModulePreloadErrors(target)
  const failure = new Error("Browser-specific module failure")
  const event = Object.assign(new Event("vite:preloadError", { cancelable: true }), { payload: failure })
  assert.equal(target.dispatchEvent(event), true)
  assert.equal(event.defaultPrevented, false)
  assert.equal(isModuleLoadError(failure), true)
  assert.equal(isModuleLoadError(new Error(failure.message)), false)
  dispose()
  const later = new Error("Another browser-specific module failure")
  target.dispatchEvent(Object.assign(new Event("vite:preloadError"), { payload: later }))
  assert.equal(isModuleLoadError(later), false)
})

test("ordinary fetch, API, rendering and cancellation failures keep their existing error paths", () => {
  for (const error of [new TypeError("Failed to fetch"), new Error("cloud_read_busy"),
    new Error("Cannot read properties of undefined"), new DOMException("Cancelled", "AbortError"),
    null, undefined, "offline", { message: "network failure" }]) {
    assert.equal(isModuleLoadError(error), false)
  }
})
