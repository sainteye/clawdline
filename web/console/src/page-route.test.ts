import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { pageFromHash, workPageHash, workRouteFromHash } from "./page-route.ts"

const knows = (name: string): name is "sessions" | "devices" => name === "sessions" || name === "devices"

test("a removed Dashboard address lands on the session list", () => {
  assert.equal(pageFromHash("#page=dashboard", knows), "sessions")
})

test("a removed Board address and its old selectors land on the session list", () => {
  assert.equal(pageFromHash("#page=board&machine=this-mac&project=p1&item=i1", knows), "sessions")
})

test("removed usage and verification-ledger addresses land on the session list", () => {
  assert.equal(pageFromHash("#page=usage", knows), "sessions")
  assert.equal(pageFromHash("#page=ledger&graph=g1", knows), "sessions")
})

test("a known page address still opens that page", () => {
  assert.equal(pageFromHash("#page=devices", knows), "devices")
})

test("a work address carries an exact project scope and where it came from", () => {
  const project = "/workspace/a project & its work"
  const hash = workPageHash(project, "projects")
  assert.equal(hash, "#page=work&project=%2Fworkspace%2Fa%20project%20%26%20its%20work&from=projects")
  assert.deepEqual(workRouteFromHash(hash), { project, fromProjects: true })
})

test("the all-project work address says all and has no invented return page", () => {
  assert.equal(workPageHash(), "#page=work")
  assert.deepEqual(workRouteFromHash("#page=work"), { project: "", fromProjects: false })
  assert.deepEqual(workRouteFromHash("#page=projects&project=not-work"), {
    project: "",
    fromProjects: false,
  })
})

test("one malformed work field does not stop the page from being routed", () => {
  assert.deepEqual(workRouteFromHash("#page=work&project=%E0%A4%A&from=projects"), {
    project: "%E0%A4%A",
    fromProjects: true,
  })
})
