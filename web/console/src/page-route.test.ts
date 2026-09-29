import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { pageFromHash, terminalPageHash, terminalRouteFromHash, workPageHash, workProjectID, workRouteFromHash } from "./page-route.ts"

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

test("a removed Now address lands on the session list", () => {
  assert.equal(pageFromHash("#page=now", knows), "sessions")
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

test("a Project-page path selects that Project's durable Board scope", () => {
  const projects = [
    { id: "project-a", path: "/workspace/a" },
    { id: "project-b", path: "/workspace/b" },
  ]

  assert.equal(workProjectID("/workspace/b", projects), "project-b")
  assert.equal(workProjectID("project-a", projects), "project-a")
  assert.equal(workProjectID("/workspace/missing", projects), "")
})

test("a terminal address carries its project and terminal and reads back the same", () => {
  const hash = terminalPageHash("place 1", "trm_abc")
  assert.equal(hash, "#page=terminal&project=place%201&terminal=trm_abc")
  assert.deepEqual(terminalRouteFromHash(hash), { project: "place 1", terminal: "trm_abc", from: "" })
  assert.deepEqual(terminalRouteFromHash("#page=work&project=x"), { project: "", terminal: "", from: "" })
  assert.equal(terminalPageHash("p"), "#page=terminal&project=p")
})

test("a terminal address remembers the page Back returns to, and an old address goes to the board", () => {
  const hash = terminalPageHash("/w/app", "", "projects")
  assert.equal(hash, "#page=terminal&project=%2Fw%2Fapp&from=projects")
  assert.equal(terminalRouteFromHash(hash).from, "projects")
  assert.equal(terminalRouteFromHash(terminalPageHash("p", "t", "work")).from, "work")
  assert.equal(terminalRouteFromHash("#page=terminal&project=p&from=elsewhere").from, "")
  assert.equal(terminalRouteFromHash("#page=terminal&project=p").from, "")
})
