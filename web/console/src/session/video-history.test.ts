import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's source test runner needs the TypeScript extension.
import { enterVideoHistory, closeVideoHistory } from "./video-history.ts"

class BrowserHistory {
  entries: Record<string, unknown>[] = [{ view: "detail", draft: "keep" }]
  index = 0
  get state() { return this.entries[this.index] }
  pushState(state: Record<string, unknown>) { this.entries.splice(++this.index); this.entries.push(state) }
  replaceState(state: Record<string, unknown>) { this.entries[this.index] = state }
}

test("immediate close/reopen needs no asynchronous back event and does not grow history", () => {
  const history = new BrowserHistory()
  for (let i = 0; i < 20; i++) {
    enterVideoHistory(history, "modal-a")
    assert.equal(history.state.videoPreview, "modal-a")
    closeVideoHistory(history, "modal-a")
    assert.equal(history.state.videoPreview, undefined)
    assert.equal(history.state.view, "detail")
  }
  assert.equal(history.entries.length, 2)
  enterVideoHistory(history, "modal-a")
  assert.equal(history.entries.length, 2)
  assert.equal(history.state.videoPreview, "modal-a")
})

test("Back and Forward retain their modal marker and another owner is not closed", () => {
  const history = new BrowserHistory()
  enterVideoHistory(history, "modal-a")
  history.index--
  assert.equal(history.state.videoPreview, undefined)
  history.index++
  assert.equal(history.state.videoPreview, "modal-a")
  closeVideoHistory(history, "modal-b")
  assert.equal(history.state.videoPreview, "modal-a")
  assert.equal(history.state.draft, "keep")
})
