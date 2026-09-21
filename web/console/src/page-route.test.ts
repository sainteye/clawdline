import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { pageFromHash } from "./page-route.ts"

const knows = (name: string): name is "sessions" | "devices" => name === "sessions" || name === "devices"

test("a removed Dashboard address lands on the session list", () => {
  assert.equal(pageFromHash("#page=dashboard", knows), "sessions")
})

test("a known page address still opens that page", () => {
  assert.equal(pageFromHash("#page=devices", knows), "devices")
})
