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
