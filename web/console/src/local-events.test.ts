import { test } from "node:test"
import assert from "node:assert/strict"
import type { StreamHandlers } from "@clawdline/core"
// @ts-expect-error -- Node executes source extensions.
import { shareLocalEvents } from "./local-events.ts"

test("fleet and screen listeners share one connection and catch up after a reopen", () => {
  const opened: StreamHandlers[] = []
  let closed = 0
  const stream = shareLocalEvents({
    open(url, handlers) {
      assert.equal(url, "/v1/events")
      opened.push(handlers)
      return { close: () => { closed++ } }
    },
  })
  const seen: string[] = []
  const fleet = stream.open("/v1/events", { onFrame: (event, data) => seen.push(event + ":" + data) })
  const screenEvents: string[] = []
  const screen = stream.open("/v1/events", {
    onFrame: (event, data) => { if (event === "screen") screenEvents.push(data) },
    onOpen: () => screenEvents.push("open"),
  })
  assert.equal(opened.length, 1)
  opened[0].onOpen?.()
  opened[0].onFrame("sessions", "older")
  opened[0].onFrame("sessions", "latest")
  opened[0].onFrame("orchestrator", "tasks")
  opened[0].onFrame("screen", "revision")
  assert.deepEqual(screenEvents, ["open", "revision"])

  fleet.close()
  assert.equal(closed, 0)
  const late: string[] = []
  const follow = stream.open("/v1/events", {
    onOpen: () => late.push("open"),
    onFrame: (event, data) => late.push(event + ":" + data),
  })
  assert.deepEqual(late, ["open", "sessions:latest", "orchestrator:tasks"])
  opened[0].onError?.(new Error("network went away"))
  const reconnect: string[] = []
  const afterError = stream.open("/v1/events", {
    onOpen: () => reconnect.push("open"),
    onFrame: (event, data) => reconnect.push(event + ":" + data),
  })
  assert.deepEqual(reconnect, [])
  opened[0].onOpen?.()
  opened[0].onFrame("sessions", "resynced")
  assert.deepEqual(reconnect, ["open", "sessions:resynced"])
  assert.deepEqual(seen, ["sessions:older", "sessions:latest", "orchestrator:tasks", "screen:revision"])
  assert.equal(opened.length, 1)
  screen.close()
  follow.close()
  afterError.close()
  afterError.close()
  assert.equal(closed, 1)
})

test("a connection the browser closed for good is replaced for the next listener", () => {
  const opened: StreamHandlers[] = []
  let closed = 0
  const stream = shareLocalEvents({
    open(_url, handlers) {
      opened.push(handlers)
      return { close: () => { closed++ } }
    },
  })
  const fleet: string[] = []
  const first = stream.open("/v1/events", { onOpen: () => fleet.push("open"), onFrame() {}, onError: () => fleet.push("error") })
  opened[0].onOpen?.()
  opened[0].onFrame("sessions", "before")
  // A non-200 answer leaves a native EventSource CLOSED: it never retries.
  opened[0].onError?.({ target: { readyState: 2 } })
  assert.deepEqual(fleet, ["open", "error"])

  const screen: string[] = []
  const late = stream.open("/v1/events", { onOpen: () => screen.push("open"), onFrame: (event, data) => screen.push(event + ":" + data) })
  assert.equal(opened.length, 2)
  assert.deepEqual(screen, [])
  opened[1].onOpen?.()
  opened[1].onFrame("sessions", "after")
  assert.deepEqual(screen, ["open", "sessions:after"])

  // The listener left on the dead connection must not detach the new one.
  first.close()
  assert.equal(closed, 1)
  const third: string[] = []
  stream.open("/v1/events", { onOpen: () => third.push("open"), onFrame() {} }).close()
  assert.deepEqual(third, ["open"])
  assert.equal(opened.length, 2)
  late.close()
  assert.equal(closed, 2)
})
