import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { pageAdmitted, pageFromHash, sessionsPageHash, sessionsTerminalMode, sessionsTerminalRoute, workPageHash, workProjectID, workRouteFromHash } from "./page-route.ts"

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

test("a terminal opens beside the Session list and its address reads back the same", () => {
  const hash = sessionsPageHash(true, { project: "place 1", terminal: "trm_abc" })
  assert.equal(hash, "#page=sessions&mode=terminal&project=place%201&terminal=trm_abc")
  assert.equal(sessionsTerminalMode(hash), true)
  assert.deepEqual(sessionsTerminalRoute(hash), { project: "place 1", terminal: "trm_abc" })
  assert.deepEqual(sessionsTerminalRoute("#page=sessions&mode=terminal"), { project: "", terminal: "" })
  assert.deepEqual(sessionsTerminalRoute("#page=sessions&project=p&terminal=t"), { project: "", terminal: "" }, "the session mode opens no terminal")
  assert.equal(sessionsPageHash(true, { project: "p", terminal: "" }), "#page=sessions&mode=terminal", "a project alone is the list")
})

test("the retired terminal page's addresses open the Session terminal list", () => {
  assert.equal(sessionsTerminalMode("#page=terminal&project=p&from=projects"), true)
  assert.deepEqual(sessionsTerminalRoute("#page=terminal&project=p&terminal=t&from=sessions"), { project: "p", terminal: "t" })
  assert.deepEqual(sessionsTerminalRoute("#page=terminal&project=p"), { project: "", terminal: "" }, "a project's list is now the whole list")
})

test("the Session address keeps the terminal mode through a reload", () => {
  assert.equal(sessionsPageHash(true), "#page=sessions&mode=terminal")
  assert.equal(sessionsTerminalMode(sessionsPageHash(true)), true)
  assert.equal(sessionsTerminalMode(sessionsPageHash()), false)
  assert.equal(sessionsTerminalMode("#page=work&mode=terminal"), false)
})


test("usage bookmarks open the overview when its module is available", () => {
  const withUsage = (name: string): name is "sessions" | "usage" => name === "sessions" || name === "usage"
  assert.equal(pageFromHash("#page=usage", withUsage), "usage")
  assert.equal(pageFromHash("#page=dashboard", withUsage), "sessions")
})

test("a typed reach-page address opens it only for an administrator", () => {
  const built = (name: string) => ["sessions", "settings", "transparency"].includes(name)
  const route = (operationsAccess: boolean, hash: string) => pageFromHash(hash,
    (name: string): name is "sessions" | "settings" | "transparency" => pageAdmitted(name, { knows: built, peerAccess: false, operationsAccess }))
  assert.equal(route(false, "#page=transparency"), "sessions")
  assert.equal(route(true, "#page=transparency"), "transparency")
  // Settings stays open to everybody; only the reach page moved behind the administrator section.
  assert.equal(route(false, "#page=settings"), "settings")
  // An administrator cannot open a page this console does not have.
  assert.equal(pageAdmitted("transparency", { knows: () => false, peerAccess: false, operationsAccess: true }), false)
})
