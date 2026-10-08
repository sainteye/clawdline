import { test } from "node:test"
import assert from "node:assert/strict"
import type { UpdateStatus } from "@clawdline/contract"
import {
  createUpdateReadStore, updateBannerVersion, UPDATE_READ_EVERY_MS,
  type UpdateRead, type UpdateReadEnvironment,
// @ts-expect-error -- a `.ts` path for node's type-stripping test runner.
} from "./update-model.ts"

function status(state: UpdateStatus["state"]): UpdateRead {
  return { kind: "status", status: {
    state, install_kind: "release", running: state === "current"
      ? { stamp: "new", version: "v2" } : { stamp: "old", version: "v1" },
    latest: { stamp: "new", version: "v2" }, checked_at: "2026-10-08T00:00:00Z",
    source_url: "https://example.invalid/manifest.json", apply: { state: "idle" },
  } }
}

function environment() {
  let now = 0
  let visible = true
  const callbacks = new Map<string, Set<() => void>>()
  const timers = new Set<() => void>()
  const on = (event: string, listener: () => void) => {
    if (!callbacks.has(event)) callbacks.set(event, new Set())
    callbacks.get(event)!.add(listener)
    return () => { callbacks.get(event)!.delete(listener) }
  }
  const env: UpdateReadEnvironment = {
    now: () => now,
    visible: () => visible,
    onVisibilityChange: (listener) => on("visibilitychange", listener),
    onFocus: (listener) => on("focus", listener),
    onOnline: (listener) => on("online", listener),
    setInterval: (listener, ms) => {
      assert.equal(ms, UPDATE_READ_EVERY_MS)
      timers.add(listener)
      return listener as unknown as ReturnType<typeof setInterval>
    },
    clearInterval: (timer) => { timers.delete(timer as unknown as () => void) },
  }
  return {
    env,
    advance: (ms: number) => { now += ms },
    show: (next: boolean) => { visible = next; callbacks.get("visibilitychange")?.forEach((fn) => fn()) },
    fire: (event: string) => { callbacks.get(event)?.forEach((fn) => fn()) },
    tick: () => { for (const fn of timers) fn() },
    timers: () => timers.size,
    listeners: () => [...callbacks.values()].reduce((count, set) => count + set.size, 0),
  }
}

const settle = () => new Promise<void>((resolve) => setImmediate(resolve))
const bannerVersion = (read: UpdateRead | null) => updateBannerVersion(read?.kind === "status" ? read.status : null, null)

test("a hidden page makes no periodic reads and checks immediately after a stale return", async () => {
  const time = environment()
  let reads = 0
  const store = createUpdateReadStore(async () => { reads++; return status("update_available") }, time.env)
  const stop = store.subscribe(() => {})
  await settle()
  assert.equal(reads, 1)
  time.show(false)
  assert.equal(time.timers(), 0)
  time.advance(UPDATE_READ_EVERY_MS + 1)
  time.tick()
  time.fire("focus")
  time.fire("online")
  assert.deepEqual(await store.refresh(), store.current(), "the Settings timer cannot poll in background")
  assert.equal(reads, 1)
  time.show(true)
  await settle()
  assert.equal(reads, 2)
  assert.equal(time.timers(), 1)
  stop()
  assert.equal(time.listeners(), 0)
  assert.equal(time.timers(), 0)
})

test("short switches and simultaneous resume events share the in-flight read", async () => {
  const time = environment()
  const pending: Array<(read: UpdateRead) => void> = []
  const store = createUpdateReadStore(() => new Promise((resolve) => { pending.push(resolve) }), time.env)
  const stopA = store.subscribe(() => {})
  const stopB = store.subscribe(() => {})
  assert.equal(pending.length, 1)
  pending.shift()!(status("update_available"))
  await settle()
  time.advance(UPDATE_READ_EVERY_MS - 1)
  time.show(false)
  time.show(true)
  time.fire("focus")
  assert.equal(pending.length, 0)
  time.advance(1)
  time.fire("focus")
  time.fire("online")
  time.tick()
  assert.equal(pending.length, 1)
  pending.shift()!(status("current"))
  await settle()
  assert.equal(bannerVersion(store.current()), null)
  stopA()
  stopB()
})

test("a failed read retries on network return and clears an obsolete banner", async () => {
  const time = environment()
  const answers: UpdateRead[] = [status("update_available"), { kind: "unreachable" }, status("unknown")]
  let reads = 0
  const store = createUpdateReadStore(async () => { reads++; return answers.shift()! }, time.env)
  const stop = store.subscribe(() => {})
  await settle()
  assert.equal(bannerVersion(store.current()), "v2")
  time.advance(UPDATE_READ_EVERY_MS)
  time.tick()
  await settle()
  assert.equal(store.current()?.kind, "unreachable")
  assert.equal(bannerVersion(store.current()), null)
  time.fire("online")
  await settle()
  assert.equal(reads, 3, "network recovery need not wait another ten minutes")
  assert.equal(store.current()?.kind, "status")
  assert.equal(bannerVersion(store.current()), null)
  stop()
})

test("a remounted view rechecks an aged shared reading", async () => {
  const time = environment()
  let reads = 0
  const store = createUpdateReadStore(async () => { reads++; return status("current") }, time.env)
  store.subscribe(() => {})()
  await settle()
  time.advance(UPDATE_READ_EVERY_MS)
  const stop = store.subscribe(() => {})
  await settle()
  assert.equal(reads, 2)
  stop()
})
